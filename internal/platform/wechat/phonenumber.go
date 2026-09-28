package wechat

import (
	"context"
	"errors"
	"strings"

	"bgin/internal/platform/errs"
)

// phoneNumberPath 是手机号快速验证端点，用小程序端 getPhoneNumber 回调的 code 换取手机号。
const phoneNumberPath = "/wxa/business/getuserphonenumber"

// Watermark 是微信为敏感数据附加的归属水印。
// 它的意义在于证明「这份数据来自本小程序」，而不是被其他来源的报文塞进来的。
type Watermark struct {
	AppID     string `json:"appid"`
	Timestamp int64  `json:"timestamp"`
}

// PhoneInfo 是手机号快速验证返回的用户手机号。
type PhoneInfo struct {
	// PhoneNumber 用户绑定的手机号，国外号码会带区号。
	PhoneNumber string `json:"phoneNumber"`
	// PurePhoneNumber 不带区号的手机号。
	PurePhoneNumber string `json:"purePhoneNumber"`
	// CountryCode 国家区号，例如 86。
	CountryCode string `json:"countryCode"`
	// Watermark 数据归属水印，由本包校验后再返回。
	Watermark Watermark `json:"watermark"`
}

// E164 把微信返回的手机号规格化为标准 E.164 格式（例如 +8613800138000）。
//
// 微信的 PhoneNumber 只在国外号码上才带区号，直接落库会让同一号码出现两种形态，
// 而用户表的唯一索引按字面值比对——形态不统一就等于没有唯一约束。
func (p *PhoneInfo) E164() (string, error) {
	if p == nil {
		return "", errs.Internal(errors.New("wechat phone info is nil"))
	}

	pure := strings.TrimSpace(p.PurePhoneNumber)
	country := strings.TrimSpace(p.CountryCode)

	// 缺区号或裸号就构不出 E.164：把形态不明的号码写进身份表，比直接失败危险得多。
	if pure == "" || country == "" {
		return "", errs.Internal(errors.New("wechat returned incomplete phone number"))
	}

	return "+" + strings.TrimPrefix(country, "+") + pure, nil
}

// phoneNumberResponse 是 getuserphonenumber 的响应报文。
type phoneNumberResponse struct {
	upstreamError
	PhoneInfo PhoneInfo `json:"phone_info"`
}

// GetPhoneNumber 用小程序端 getPhoneNumber 回调返回的 code 换取用户手机号。
//
// 该接口按成功次数计费（每次约 0.03 元），且 code 只能使用一次，因此本方法不做任何业务重试：
// 超时等网络类失败一律直接透传，由调用方决定是否需要人工核对——超时重试可能在
// 「首次实际已成功」的情况下重复扣费。唯一的例外是凭据失效（40001/40014/42001），
// 那类失败确定没有产生手机号，由 withAccessToken 刷新凭据后重试一次。
func (c *Client) GetPhoneNumber(ctx context.Context, code string) (*PhoneInfo, error) {
	const operation = "wechat.GetPhoneNumber"

	if c == nil || c.http == nil {
		return nil, errs.ServiceUnavailable("wechat miniprogram client is not configured")
	}

	trimmedCode := strings.TrimSpace(code)
	if trimmedCode == "" {
		return nil, errs.BadRequest("code is required")
	}

	var payload phoneNumberResponse

	err := c.withAccessToken(ctx, func(callCtx context.Context, token string) (int, error) {
		// 每次调用都从零开始解码：失败报文中缺失的字段会残留上一次的取值。
		payload = phoneNumberResponse{}

		response, requestErr := c.http.R().
			SetContext(callCtx).
			SetQueryParam("access_token", token).
			SetBody(map[string]string{"code": trimmedCode}).
			Post(phoneNumberPath)
		if requestErr != nil {
			return 0, errs.ServiceUnavailable("wechat getuserphonenumber request failed: " + c.redactError(requestErr))
		}

		return decodeBusinessResponse(operation, response, &payload)
	})
	if err != nil {
		return nil, err
	}

	// errcode 为 0 却拿不到号码说明上游行为与契约不符：
	// 静默返回空号码会让上层把一个空手机号写进用户档案。
	if payload.PhoneInfo.PhoneNumber == "" && payload.PhoneInfo.PurePhoneNumber == "" {
		return nil, errs.Internal(errors.New("wechat returned empty phone number"))
	}

	// 归属校验：水印里的 appid 必须等于本小程序的 appid。
	// 不校验的话，一个来自其他小程序的报文（或被中间人塞入的报文）同样能被当成合法手机号写进账号体系。
	if payload.PhoneInfo.Watermark.AppID != c.appID {
		return nil, errs.Forbidden("wechat: phone number watermark does not belong to this miniprogram")
	}

	return &payload.PhoneInfo, nil
}
