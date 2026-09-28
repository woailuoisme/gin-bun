package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// defaultPaymentAppKey 是单应用便捷配置展开后的应用标识。
const defaultPaymentAppKey = "default"

// PaymentConfig 定义支付渠道配置。
// 每个渠道维护一组 Channel Application（可注册多个应用），下单时按「渠道 + 场景」解析到唯一应用。
// 渠道遵循「配置即启用」原则：配置了任一应用即生效，不存在独立启用开关；
// 沙箱与生产通过运行环境切换，不在同一份配置内并存两套字段。
type PaymentConfig struct {
	// FakeEnabled 在非生产环境注册进程内 Fake 渠道，用于本地开发与端到端演练。
	// 显式开关而非按环境隐式回退，避免「未配置」与「已降级」在运行期难以区分。
	FakeEnabled bool `env:"PAYMENT_FAKE_ENABLED" envDefault:"false"`
	// CertsDir 支付凭据与证书文件的存放根目录。证书路径若为相对路径，将以该目录为基准解析。
	CertsDir string `env:"PAYMENT_CERTS_DIR" envDefault:"./storage/app/certs"`
	Wechat   WechatPaymentChannelConfig
	Alipay   AlipayPaymentChannelConfig
}

// WechatPaymentChannelConfig 定义微信支付渠道配置。
// Default 承载单应用场景下的扁平环境变量；ApplicationsJSON 用于声明多个微信支付应用。
type WechatPaymentChannelConfig struct {
	// ApplicationsJSON 以 JSON 对象描述多个微信支付应用，键为应用标识，值为 WechatPaymentAppConfig。
	ApplicationsJSON string `env:"WECHAT_PAY_APPS_JSON"`
	// Default 单应用便捷配置：未提供 ApplicationsJSON 时作为唯一应用生效。
	Default WechatPaymentAppConfig
}

// WechatPaymentAppConfig 定义单个微信支付应用的凭据、证书路径与可承载场景。
// 商户私钥与平台公钥只配置文件路径，PEM 内容由基础设施层在启动时读入内存。
type WechatPaymentAppConfig struct {
	// AppID 直连商户申请的公众号、小程序或开放平台移动应用 appid。
	AppID string `env:"WECHAT_PAY_APP_ID" json:"app_id"`
	// MchID 微信支付商户号。
	MchID string `env:"WECHAT_PAY_MCH_ID" json:"mch_id"`
	// MerchantSerialNo 商户 API 证书序列号。
	MerchantSerialNo string `env:"WECHAT_PAY_MCH_SERIAL_NO" json:"merchant_serial_no"`
	// APIv3Key APIv3 密钥，用于通知报文的 AES-256-GCM 解密与平台证书下载。
	APIv3Key string `env:"WECHAT_PAY_API_V3_KEY" json:"api_v3_key"`
	// MerchantPrivateKeyPath 商户 API 证书私钥 apiclient_key.pem 的路径。
	MerchantPrivateKeyPath string `env:"WECHAT_PAY_MERCHANT_PRIVATE_KEY_PATH" json:"merchant_private_key_path"`
	// PlatformPublicKeyPath 微信支付公钥文件路径（公钥模式）；留空则改用平台证书自动下载与轮换。
	PlatformPublicKeyPath string `env:"WECHAT_PAY_PLATFORM_PUBLIC_KEY_PATH" json:"platform_public_key_path"`
	// PlatformPublicKeyID 微信支付公钥 ID，公钥模式下必填。
	PlatformPublicKeyID string `env:"WECHAT_PAY_PLATFORM_PUBLIC_KEY_ID" json:"platform_public_key_id"`
	// NotifyURL 该应用的支付与退款结果通知地址。
	NotifyURL string `env:"WECHAT_PAY_NOTIFY_URL" json:"notify_url"`
	// Scenes 该应用可承载的支付场景，取值见 enums.PaymentScene。
	Scenes []string `env:"WECHAT_PAY_SCENES" json:"scenes"`
}

// AlipayPaymentChannelConfig 定义支付宝渠道配置。
type AlipayPaymentChannelConfig struct {
	// ApplicationsJSON 以 JSON 对象描述多个支付宝应用，键为应用标识，值为 AlipayPaymentAppConfig。
	ApplicationsJSON string `env:"ALIPAY_APPS_JSON"`
	// Default 单应用便捷配置：未提供 ApplicationsJSON 时作为唯一应用生效。
	Default AlipayPaymentAppConfig
}

// AlipayPaymentAppConfig 定义单个支付宝应用的凭据、证书路径与可承载场景。
// 支付宝 v3 仅支持公钥证书模式，三本证书缺一不可，否则响应验签会被静默跳过。
type AlipayPaymentAppConfig struct {
	// AppID 支付宝应用 ID。
	AppID string `env:"ALIPAY_APP_ID" json:"app_id"`
	// PrivateKeyPath 应用私钥 apiclient_key.pem 的路径。
	PrivateKeyPath string `env:"ALIPAY_PRIVATE_KEY_PATH" json:"private_key_path"`
	// AppCertPath 应用公钥证书 appCertPublicKey.crt 的路径。
	AppCertPath string `env:"ALIPAY_APP_CERT_PATH" json:"app_cert_path"`
	// PublicCertPath 支付宝公钥证书 alipayCertPublicKey_RSA2.crt 的路径。
	PublicCertPath string `env:"ALIPAY_PUBLIC_CERT_PATH" json:"public_cert_path"`
	// RootCertPath 支付宝根证书 alipayRootCert.crt 的路径。
	RootCertPath string `env:"ALIPAY_ROOT_CERT_PATH" json:"root_cert_path"`
	// NotifyURL 该应用的支付与退款结果通知地址。
	NotifyURL string `env:"ALIPAY_NOTIFY_URL" json:"notify_url"`
	// ReturnURL 同步回跳地址，仅电脑网站与手机网站支付需要。
	ReturnURL string `env:"ALIPAY_RETURN_URL" json:"return_url"`
	// Scenes 该应用可承载的支付场景，取值见 enums.PaymentScene。
	Scenes []string `env:"ALIPAY_SCENES" json:"scenes"`
}

// resolveApplications 解析渠道的最终应用集合，两个渠道共用同一套规则。
// 未提供 ApplicationsJSON 时使用扁平环境变量描述的单应用，键固定为 default。
// jsonVarName 只用于错误信息，让报错点名具体的环境变量。
func resolveApplications[T any](
	applicationsJSON string,
	defaultApp T,
	declared func(T) bool,
	jsonVarName string,
) (map[string]T, error) {
	if raw := strings.TrimSpace(applicationsJSON); raw != "" {
		apps := make(map[string]T)
		if err := json.Unmarshal([]byte(raw), &apps); err != nil {
			return nil, fmt.Errorf("config: invalid %s: %w", jsonVarName, err)
		}

		return apps, nil
	}

	if !declared(defaultApp) {
		return map[string]T{}, nil
	}

	return map[string]T{defaultPaymentAppKey: defaultApp}, nil
}

// ResolveWechatApplications 解析微信支付渠道的最终应用集合。
func ResolveWechatApplications(channel WechatPaymentChannelConfig) (map[string]WechatPaymentAppConfig, error) {
	return resolveApplications(channel.ApplicationsJSON, channel.Default, wechatDefaultDeclared, "WECHAT_PAY_APPS_JSON")
}

// ResolveAlipayApplications 解析支付宝渠道的最终应用集合。
func ResolveAlipayApplications(channel AlipayPaymentChannelConfig) (map[string]AlipayPaymentAppConfig, error) {
	return resolveApplications(channel.ApplicationsJSON, channel.Default, alipayDefaultDeclared, "ALIPAY_APPS_JSON")
}

// HasWechatApplications 判断微信渠道是否配置了至少一个应用。
func HasWechatApplications(channel WechatPaymentChannelConfig) bool {
	return hasApplications(ResolveWechatApplications(channel))
}

// HasAlipayApplications 判断支付宝渠道是否配置了至少一个应用。
func HasAlipayApplications(channel AlipayPaymentChannelConfig) bool {
	return hasApplications(ResolveAlipayApplications(channel))
}

// hasApplications 判断解析结果是否至少含一个应用；解析失败按"未配置"处理。
func hasApplications[T any](apps map[string]T, err error) bool {
	return err == nil && len(apps) > 0
}

// PaymentConfigured 判断是否配置了至少一个渠道应用。
func PaymentConfigured(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	return HasWechatApplications(cfg.Payment.Wechat) || HasAlipayApplications(cfg.Payment.Alipay)
}

// anyNonEmpty 判断这组字段里是否至少有一个被填写（去空白后非空）。
func anyNonEmpty(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}

	return false
}

// wechatDefaultDeclared 判断微信支付扁平单应用配置是否已被填写。
func wechatDefaultDeclared(app WechatPaymentAppConfig) bool {
	return anyNonEmpty(app.AppID, app.MchID, app.MerchantSerialNo, app.APIv3Key,
		app.MerchantPrivateKeyPath, app.PlatformPublicKeyPath, app.PlatformPublicKeyID, app.NotifyURL) ||
		len(app.Scenes) > 0
}

// alipayDefaultDeclared 判断支付宝扁平单应用配置是否已被填写。
func alipayDefaultDeclared(app AlipayPaymentAppConfig) bool {
	return anyNonEmpty(app.AppID, app.PrivateKeyPath, app.AppCertPath, app.PublicCertPath,
		app.RootCertPath, app.NotifyURL, app.ReturnURL) ||
		len(app.Scenes) > 0
}

// validatePayment 校验支付配置的合法性。
func (c *Config) validatePayment() error {
	if _, err := ResolveWechatApplications(c.Payment.Wechat); err != nil {
		return err
	}
	if _, err := ResolveAlipayApplications(c.Payment.Alipay); err != nil {
		return err
	}
	return nil
}
