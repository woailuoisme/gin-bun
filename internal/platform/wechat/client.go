// Package wechat 提供微信开放平台（公众平台 / 小程序）服务端 API 客户端实现。
package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"golang.org/x/sync/singleflight"

	"bgin/internal/config"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/errs"
)

const (
	// defaultBaseURL 是微信开放平台服务端 API 的默认域名。
	defaultBaseURL = "https://api.weixin.qq.com"
	// code2SessionPath 是小程序登录凭证校验端点，用 wx.login() 的 code 换取用户身份。
	code2SessionPath = "/sns/jscode2session"
	// grantTypeAuthorizationCode 是 code2Session 的唯一合法取值。
	grantTypeAuthorizationCode = "authorization_code"
	// defaultTimeout 是单次调用微信开放平台 API 的超时。
	defaultTimeout = 5 * time.Second
)

// 微信开放平台业务错误码常量。这些错误码随 HTTP 200 返回，不体现在状态码上。
const (
	errCodeSystemBusy        = -1
	errCodeInvalidCredential = 40001 // 凭据无效或已不是最新的
	errCodeInvalidAppID      = 40013
	errCodeInvalidToken      = 40014 // 无效的 access_token
	errCodeInvalidCode       = 40029
	errCodeInvalidSecret     = 40125
	errCodeHighRiskUser      = 40226
	errCodeInvalidPage       = 41030 // 页面路径不存在、未发布或带了前导斜杠
	errCodeTokenExpired      = 42001 // access_token 已超时
	errCodeAPIFreqLimit      = 45009 // 接口调用频率或日配额超限
	errCodeTooManyRequests   = 45011 // 接口分钟级配额超限
	errCodeAPIUnauthorized   = 48001 // 小程序未获得该接口权限
)

// credentialFailureCodes 是「刷新凭据后值得重试一次」的错误码集合。
// 这些错误由凭据本身失效引起，与请求参数无关，重放同一个请求不会重复产生副作用。
var credentialFailureCodes = []int{errCodeInvalidCredential, errCodeInvalidToken, errCodeTokenExpired}

// Session 是小程序登录成功后取得的用户身份。
//
// SessionKey 是会话密钥，只能留在服务端：它可以解密用户敏感数据、参与数据签名校验，
// 一旦下发到客户端即等同于把该用户的解密能力交出。本结构的调用方不得把它序列化给前端。
type Session struct {
	// OpenID 用户在该小程序内的唯一标识。换一个小程序，同一用户的该值即不同。
	OpenID string
	// UnionID 用户在微信开放平台账号下的唯一标识，仅当小程序已绑定开放平台账号时才有值。
	UnionID string
	// SessionKey 会话密钥，仅供服务端使用。
	SessionKey string
}

// Client 是微信开放平台 API 客户端，绑定单个小程序的凭据。
//
// 本结构持有 access_token 的进程内缓存与 resty 连接池，必须是进程级单例：
// 按请求新建客户端会让凭据缓存与连接池每次都重建。
type Client struct {
	appID     string
	appSecret string
	http      *resty.Client
	cache     *cache.Client
	memTokens sync.Map
	tokenKey  string
	// flight 保证同一进程内并发回源只发生一次。
	flight singleflight.Group
}

// NewClient 依据小程序凭据构造客户端。
// 凭据未声明时返回 nil，调用方据此判定小程序能力未启用。
//
// cacheClient 用于缓存 access_token，为 nil 时退化为实例私有内存缓存——
// 本包的凭据缓存刻意不做跨实例共享（stable_token 在有效期内返回同一凭据，无需共享），
// 因此该退化不改变语义，但注入共享缓存可以让同一进程内的多个客户端复用凭据。
func NewClient(cfg config.WeChatMiniProgramConfig, cacheClient *cache.Client) *Client {
	if !cfg.Declared() {
		return nil
	}

	appID := strings.TrimSpace(cfg.AppID)

	httpClient := resty.New().
		SetBaseURL(defaultBaseURL).
		SetTimeout(defaultTimeout).
		SetRetryCount(2).
		SetRetryWaitTime(100 * time.Millisecond).
		SetRetryMaxWaitTime(2 * time.Second)

	return &Client{
		appID:     appID,
		appSecret: strings.TrimSpace(cfg.AppSecret),
		http:      httpClient,
		cache:     cacheClient,
		tokenKey:  tokenCacheKey(appID),
	}
}

// WithBaseURL 覆盖默认域名，返回同一实例以便链式调用。
// 提供该入口是为了让单测能在进程内起桩，生产环境不应调用。
func (c *Client) WithBaseURL(baseURL string) *Client {
	if c != nil && c.http != nil && strings.TrimSpace(baseURL) != "" {
		c.http.SetBaseURL(strings.TrimSuffix(strings.TrimSpace(baseURL), "/"))
	}

	return c
}

// AppID 返回该客户端绑定的小程序 AppID。
// 调用方在落库身份时需要它：微信的 openid 是应用级标识，脱离 appid 无法解释。
func (c *Client) AppID() string {
	if c == nil {
		return ""
	}

	return c.appID
}

// Code2Session 用 wx.login() 返回的 code 校验并换取用户身份。
//
// 不做重试：code 只能使用一次且 5 分钟内有效，重试一个已被消费的 code 必然返回
// errCodeInvalidCode，重试只会把一次客户端错误放大成多次无效调用。
func (c *Client) Code2Session(ctx context.Context, code string) (*Session, error) {
	const operation = "wechat.Code2Session"

	if c == nil || c.http == nil {
		return nil, errs.ServiceUnavailable("wechat miniprogram client is not configured")
	}

	trimmedCode := strings.TrimSpace(code)
	if trimmedCode == "" {
		return nil, errs.BadRequest("code is required")
	}

	response, err := c.http.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"appid":      c.appID,
			"secret":     c.appSecret,
			"js_code":    trimmedCode,
			"grant_type": grantTypeAuthorizationCode,
		}).
		Get(code2SessionPath)
	if err != nil {
		return nil, errs.ServiceUnavailable("wechat code2session request failed: " + c.redactError(err))
	}

	var payload code2SessionResponse
	if _, decodeErr := decodeBusinessResponse(operation, response, &payload); decodeErr != nil {
		return nil, decodeErr
	}

	// errcode 为 0 却拿不到 openid 说明上游行为与契约不符，必须显式失败：
	// 静默返回空 openid 会在上层被当成一个合法身份写进账号体系。
	if payload.OpenID == "" {
		return nil, errs.Internal(errors.New("wechat code2session succeeded without openid"))
	}

	return &Session{
		OpenID:     payload.OpenID,
		UnionID:    payload.UnionID,
		SessionKey: payload.SessionKey,
	}, nil
}

// code2SessionResponse 是 code2session 的响应报文。
// 成功与失败共用同一结构：ErrCode 为 0 表示成功，否则只携带错误描述。
type code2SessionResponse struct {
	upstreamError
	OpenID     string `json:"openid"`
	SessionKey string `json:"session_key"`
	UnionID    string `json:"unionid"`
}

// upstreamError 是所有微信响应报文共有的业务错误字段。
type upstreamError struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// upstreamErrcode 暴露业务错误字段，供 errCarrier 统一分派。
// 方法名刻意不与嵌入字段名同名：同名字段会遮蔽被提升的方法，导致类型无法满足接口。
func (e *upstreamError) upstreamErrcode() (int, string) {
	return e.ErrCode, e.ErrMsg
}

// errCarrier 抽象携带微信统一业务错误字段的响应结构体。
type errCarrier interface {
	upstreamErrcode() (int, string)
}

// decodeBusinessResponse 解出上游响应体并判定业务成败，返回上游 errcode 与已映射的错误。
//
// 调用方需要 errcode 本身（而非只关心错误）：凭据失效类错误要据此触发一次刷新重试。
func decodeBusinessResponse(operation string, response *resty.Response, payload errCarrier) (int, error) {
	if err := json.Unmarshal(response.Body(), payload); err != nil {
		return 0, errs.Internal(fmt.Errorf("failed to decode wechat response (status %d): %w", response.StatusCode(), err))
	}

	code, message := payload.upstreamErrcode()
	if code != 0 {
		return code, mapUpstreamError(operation, code, message)
	}

	return 0, nil
}

// withAccessToken 执行一次带 access_token 的业务调用。
//
// call 返回上游 errcode（0 表示成功）与已映射的错误。所有带凭据的调用统一经由本方法，
// 以便在凭据失效时集中处理「丢弃本地缓存 → 重取 → 重试恰好一次」：
// 一个进程缓存里的凭据可能因另一个实例的刷新、或运营后台重置而提前作废，
// 一次刷新重试能把这类问题挡在调用方之外，而重试次数封顶为一次则避免把上游限流放大成风暴。
func (c *Client) withAccessToken(ctx context.Context, call func(ctx context.Context, token string) (int, error)) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}

	code, callErr := call(ctx, token)
	if code == 0 || !slices.Contains(credentialFailureCodes, code) {
		return callErr
	}

	// 重取恒走 force_refresh=false：用 true 会立即作废其他实例正在使用的凭据。
	c.invalidateAccessToken(ctx)

	retryToken, refreshErr := c.accessToken(ctx)
	if refreshErr != nil {
		return refreshErr
	}

	if _, retryErr := call(ctx, retryToken); retryErr != nil {
		return retryErr
	}

	return nil
}

// redactError 从 error 中抹掉 AppSecret。
//
// 本包存在带上密钥的 GET 调用（code2Session），凭据位于 query 中，
// 而 net/http 的拨号与超时错误会把完整请求 URL 拼进错误文本。若原样透传，
// AppSecret 就会随同一条连接失败写进日志，而日志的阅读面远大于密钥本身应有的范围。
// 此处做精确抹除而非依赖调用方自觉。
func (c *Client) redactError(err error) string {
	if err == nil {
		return ""
	}
	if c == nil || c.appSecret == "" {
		return err.Error()
	}

	return strings.ReplaceAll(err.Error(), c.appSecret, "[redacted]")
}

// mapUpstreamError 把微信的业务错误码映射为仓库统一的错误语义。
//
// 40013（AppID 非法）与 40125（AppSecret 非法）是服务端配置错误而非调用方错误，
// 归入 Internal 以便触发告警，而不是让客户端以为自己传错了参数。
func mapUpstreamError(_ string, code int, message string) error {
	switch code {
	case errCodeInvalidCode:
		return errs.BadRequest("wechat: code is invalid, expired or already used")
	case errCodeInvalidPage:
		return errs.BadRequest("wechat: page path does not exist or is not published")
	case errCodeHighRiskUser:
		return errs.Forbidden("wechat: user is flagged as high risk")
	case errCodeAPIFreqLimit, errCodeTooManyRequests:
		return errs.RateLimited("wechat: api rate limit exceeded")
	case errCodeSystemBusy:
		return errs.ServiceUnavailable("wechat: system busy, retry later")
	case errCodeInvalidCredential, errCodeInvalidToken, errCodeTokenExpired:
		// 走到这里说明刷新凭据后仍被拒绝：appid/secret 配错或账号被限制，属服务端问题。
		return errs.Internal(fmt.Errorf("wechat: credential rejected after refresh: %s (%d)", message, code))
	case errCodeInvalidAppID, errCodeInvalidSecret:
		return errs.Internal(fmt.Errorf("wechat: credential rejected by upstream: %s (%d)", message, code))
	case errCodeAPIUnauthorized:
		return errs.Internal(fmt.Errorf("wechat: miniprogram is not authorized for this api: %s (%d)", message, code))
	default:
		return errs.ServiceUnavailable("wechat: unexpected upstream error")
	}
}
