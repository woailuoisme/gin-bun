package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"bgin/internal/platform/cache"
	"bgin/internal/platform/errs"
)

const (
	// stableEndpoint 是稳定版接口调用凭据端点。
	stableEndpoint = "/cgi-bin/stable_token"
	// grantTypeClientCredential 是获取接口调用凭据的唯一合法取值。
	grantTypeClientCredential = "client_credential"
	// tokenRefreshMargin 是提前失效量：宁可提前几分钟重取，
	// 也不要在临界点上让业务请求撞上一次注定失败的调用。
	tokenRefreshMargin = 5 * time.Minute
	// minTokenTTL 是本地缓存时长的下限，避免上游返回极短有效期时写入一个立刻失效的条目。
	minTokenTTL = 30 * time.Second
)

// tokenEntry 是进程内缓存的凭据条目。
//
// 过期时刻必须随值一起存，不能只依赖缓存 TTL：stable_token 在有效期内会返回同一个凭据，
// 但 expires_in 是**剩余**秒数。若按固定 TTL 缓存，一个在新旧凭据交替期间启动的实例
// 会把「剩余 60 秒」的凭据缓存成一整段 TTL，之后每个业务请求都要先吃一次 40001 再重试。
type tokenEntry struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (e tokenEntry) isValid(now time.Time) bool {
	return e.Token != "" && now.Before(e.ExpiresAt)
}

// tokenResponse 是 stable_token 的响应报文。
type tokenResponse struct {
	upstreamError
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// tokenCacheKey 生成按 appid 隔离的凭据缓存键。
func tokenCacheKey(appID string) string {
	return cache.Key("wechat", "token", appID)
}

// accessToken 返回业务调用所需的接口调用凭据，优先命中进程内缓存。
//
// 缓存未命中时通过 singleflight 合并并发回源：微信对取凭据的接口本身有配额，
// 且并发回源在多实例下会把彼此的凭据相互作废。
func (c *Client) accessToken(ctx context.Context) (string, error) {
	if c == nil {
		return "", errs.ServiceUnavailable("wechat miniprogram client is not configured")
	}

	if token, ok := c.cachedToken(ctx); ok {
		return token, nil
	}

	value, err, _ := c.flight.Do(c.tokenKey, func() (any, error) {
		// 用 WithoutCancel：一次请求取消不应连累共享同一次回源的其他请求。
		flightCtx := context.WithoutCancel(ctx)

		// 二次校验：并发场景下可能已被其他 goroutine 回填。
		if token, ok := c.cachedToken(flightCtx); ok {
			return token, nil
		}

		return c.fetchAccessToken(flightCtx)
	})
	if err != nil {
		return "", err
	}

	token, _ := value.(string)
	if token == "" {
		return "", errs.Internal(errors.New("wechat returned empty access_token"))
	}

	return token, nil
}

// cachedToken 读取仍在有效期内的凭据。
func (c *Client) cachedToken(ctx context.Context) (string, bool) {
	now := time.Now().UTC()
	if c.cache != nil {
		raw, err := c.cache.Get(ctx, c.tokenKey)
		if err != nil || raw == "" {
			return "", false
		}
		var entry tokenEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			return "", false
		}
		if !entry.isValid(now) {
			return "", false
		}
		return entry.Token, true
	}

	val, ok := c.memTokens.Load(c.tokenKey)
	if !ok {
		return "", false
	}
	entry, ok := val.(tokenEntry)
	if !ok || !entry.isValid(now) {
		return "", false
	}
	return entry.Token, true
}

// fetchAccessToken 向微信换取一次凭据并写入缓存。
func (c *Client) fetchAccessToken(ctx context.Context) (string, error) {
	const operation = "wechat.accessToken"

	response, err := c.http.R().
		SetContext(ctx).
		SetBody(map[string]any{
			"grant_type": grantTypeClientCredential,
			"appid":      c.appID,
			"secret":     c.appSecret,
			// 恒为 false：force_refresh=true 会立即作废其他实例正在使用的凭据。
			"force_refresh": false,
		}).
		Post(stableEndpoint)
	if err != nil {
		return "", errs.ServiceUnavailable("wechat stable_token request failed: " + c.redactError(err))
	}

	var payload tokenResponse
	if _, decodeErr := decodeBusinessResponse(operation, response, &payload); decodeErr != nil {
		return "", decodeErr
	}

	if payload.AccessToken == "" {
		return "", errs.Internal(errors.New("wechat stable_token succeeded without access_token"))
	}

	ttl := tokenTTL(payload.ExpiresIn)
	entry := tokenEntry{Token: payload.AccessToken, ExpiresAt: time.Now().UTC().Add(ttl)}

	if c.cache != nil {
		if raw, err := json.Marshal(entry); err == nil {
			_ = c.cache.Set(ctx, c.tokenKey, string(raw), ttl)
		}
	} else {
		c.memTokens.Store(c.tokenKey, entry)
	}

	return payload.AccessToken, nil
}

// invalidateAccessToken 丢弃凭据缓存，使下一次 accessToken 重新向微信取一次。
func (c *Client) invalidateAccessToken(ctx context.Context) {
	if c == nil {
		return
	}

	if c.cache != nil {
		_, _ = c.cache.Delete(ctx, c.tokenKey)
	}
	c.memTokens.Delete(c.tokenKey)
}

// tokenTTL 把上游返回的有效期换算为本地缓存时长，预留提前失效量并设下限。
func tokenTTL(expiresIn int) time.Duration {
	return max(minTokenTTL, time.Duration(expiresIn)*time.Second-tokenRefreshMargin)
}
