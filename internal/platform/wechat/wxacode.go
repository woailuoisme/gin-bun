package wechat

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/go-resty/resty/v2"

	"bgin/internal/platform/errs"
)

const (
	// unlimitedCodePath 是数量不限的小程序码端点。
	unlimitedCodePath = "/wxa/getwxacodeunlimit"
	// maxSceneLength 是 scene 允许的最大可见字符数（按字符数而非字节数计）。
	maxSceneLength = 32
	// sceneSpecialRunes 是 scene 除数字与大小写字母外允许出现的特殊字符。
	sceneSpecialRunes = "!#$&'()*+,/:;=?@-._~"
)

// EnvVersion 指定小程序码指向的小程序版本。
type EnvVersion string

// 小程序码可用的小程序版本。
const (
	EnvVersionRelease EnvVersion = "release"
	EnvVersionTrial   EnvVersion = "trial"
	EnvVersionDevelop EnvVersion = "develop"
)

// UnlimitedCodeParams 是获取数量不限小程序码的入参。
type UnlimitedCodeParams struct {
	// Scene 是扫码进入页面时携带的参数，最多 32 个可见字符。
	Scene string
	// Page 是扫码进入的页面路径，形如 pages/index/index，不能带前导斜杠；留空表示主页。
	Page string
	// EnvVersion 指定打开哪个版本，留空等同于 release。
	EnvVersion EnvVersion
	// Width 是码的宽度（像素），留空由微信按默认值处理。
	Width int
	// IsHyaline 为 true 时生成透明底色的小程序码。
	IsHyaline bool
}

// GetUnlimitedCode 生成数量不限的小程序码，返回 JPEG 字节流。
//
// 只返回字节流、不落盘：小程序码是永久有效的，预生成、去重与分发属于业务语义
// （哪些 scene 需要预生成、复用键怎么定、放到哪个 CDN 路径），由调用方决定；
// 把存储塞进本包会让 infra 层反向依赖对象存储，也会把复用策略变成包内的隐式行为。
// 注意上游限 5000 次/分钟，且同一 scene 重复生成会得到内容相同但字节不同的多份图，
// 因此调用方应复用既有产物而不是每次现生成。
func (c *Client) GetUnlimitedCode(ctx context.Context, params UnlimitedCodeParams) ([]byte, error) {
	const operation = "wechat.GetUnlimitedCode"

	if c == nil || c.http == nil {
		return nil, errs.ServiceUnavailable("wechat miniprogram client is not configured")
	}

	params = params.Normalize()
	if err := params.Validate(); err != nil {
		return nil, err
	}

	body := map[string]any{"scene": params.Scene}
	if params.Page != "" {
		body["page"] = params.Page
	}
	if params.EnvVersion != "" {
		body["env_version"] = string(params.EnvVersion)
	}
	if params.Width > 0 {
		body["width"] = params.Width
	}
	if params.IsHyaline {
		body["is_hyaline"] = true
	}

	var code []byte

	err := c.withAccessToken(ctx, func(callCtx context.Context, token string) (int, error) {
		code = nil

		response, requestErr := c.http.R().
			SetContext(callCtx).
			SetQueryParam("access_token", token).
			SetBody(body).
			Post(unlimitedCodePath)
		if requestErr != nil {
			return 0, errs.ServiceUnavailable("wechat getwxacodeunlimit request failed: " + c.redactError(requestErr))
		}

		// 成功返回 JPEG 字节流、失败才返回 JSON 错误报文，因此先按报文形态分流。
		if !isJSONResponse(response) {
			code = response.Body()

			return 0, nil
		}

		var payload upstreamError
		if errCode, decodeErr := decodeBusinessResponse(operation, response, &payload); decodeErr != nil {
			return errCode, decodeErr
		}

		return 0, errs.Internal(errors.New("wechat getwxacodeunlimit returned an unexpected json body"))
	})
	if err != nil {
		return nil, err
	}

	// 上游以 200 + 空字节流应答时不能当成一张图交出去。
	if len(code) == 0 {
		return nil, errs.Internal(errors.New("wechat getwxacodeunlimit succeeded without image body"))
	}

	return code, nil
}

// Normalize 清洗参数，避免把首尾空白拼进 scene 或页面路径。
func (p UnlimitedCodeParams) Normalize() UnlimitedCodeParams {
	p.Scene = strings.TrimSpace(p.Scene)
	p.Page = strings.TrimSpace(p.Page)

	return p
}

// Validate 在本地拦截注定被上游拒绝的参数。
//
// 这些约束（字符数、字符集、前导斜杠、页面已发布）都在微信侧才报错，
// 而每一次失败调用都要先取一次凭据并占用接口配额，本地拦下能省掉整轮往返。
func (p UnlimitedCodeParams) Validate() error {
	if p.Scene == "" {
		return errs.BadRequest("scene is required")
	}

	if utf8.RuneCountInString(p.Scene) > maxSceneLength {
		return errs.BadRequest("scene must not exceed 32 visible characters")
	}

	for _, r := range p.Scene {
		if !isSceneRuneAllowed(r) {
			return errs.BadRequest("scene contains characters not supported by wechat")
		}
	}

	// 带前导斜杠的页面路径会被上游以 41030 拒绝，且该错误与「页面不存在」共用一个码，
	// 本地拦下可以让调用方一眼看出是格式问题。
	if strings.HasPrefix(p.Page, "/") {
		return errs.BadRequest("page must not start with a slash")
	}

	if p.EnvVersion != "" && !p.EnvVersion.IsValid() {
		return errs.BadRequest("env_version must be one of release, trial, develop")
	}

	return nil
}

// isSceneRuneAllowed 判断字符是否属于 scene 允许的字符集。
func isSceneRuneAllowed(r rune) bool {
	isDigit := r >= '0' && r <= '9'
	isLower := r >= 'a' && r <= 'z'
	isUpper := r >= 'A' && r <= 'Z'

	return isDigit || isLower || isUpper || strings.ContainsRune(sceneSpecialRunes, r)
}

// IsValid 判断小程序版本取值是否合法。
func (v EnvVersion) IsValid() bool {
	switch v {
	case EnvVersionRelease, EnvVersionTrial, EnvVersionDevelop:
		return true
	default:
		return false
	}
}

// isJSONResponse 判断响应是否为 JSON 报文。
// 判据取「声明的 Content-Type」与「报文体首字符」的并集：
// 上游在错误路径上的 Content-Type 并不总是规范，只看任一者都会误判。
func isJSONResponse(response *resty.Response) bool {
	if strings.Contains(response.Header().Get("Content-Type"), "json") {
		return true
	}

	return bytes.HasPrefix(bytes.TrimSpace(response.Body()), []byte("{"))
}
