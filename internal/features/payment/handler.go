package payment

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"bgin/internal/platform/errs"
	"bgin/internal/platform/logger"
	"bgin/internal/platform/payment/enums"
)

// Handler 承接支付渠道的 Webhook 异步通知与客户端同步跳转。
type Handler struct {
	service *Service
}

// NewHandler 构造支付回调 HTTP 处理器。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// HandleNotification 接收微信与支付宝支付/退款异步回调通知。
//
// @Summary      支付与退款异步通知回调
// @Description  接收微信或支付宝服务端的支付与退款异步通知，由网关执行公钥/证书验签、幂等防重检查与业务状态机推进，返回渠道约定的确认应答。
// @Tags         支付中心
// @Accept       json,x-www-form-urlencoded
// @Produce      json,plain
// @Param        channel path string true "支付渠道 (wechat, alipay)" enums(wechat, alipay)
// @Param        app     path string false "应用标识 (可选，缺省时使用渠道默认应用)"
// @Success      200 {object} CallbackAckResponse "回调验签与业务消费成功"
// @x-order      1
// @Router       /payments/notify/{channel} [post]
func (h *Handler) HandleNotification(c *gin.Context) {
	channelStr := c.Param("channel")
	appKey := c.Param("app")

	channel := enums.PaymentChannel(channelStr)
	if !channel.IsValid() {
		// 报文里不拼接调用方原始输入，渠道值只进结构化日志。
		logger.FromContext(c.Request.Context()).Warn().Str("channel", channelStr).Msg("unsupported payment channel callback received")
		err := errs.BadRequest(errUnsupportedChannel)
		_ = c.Error(err)
		errs.Render(c, err)
		return
	}

	ack, err := h.service.HandleNotification(c.Request.Context(), channelStr, appKey, c.Request)
	if err != nil {
		_ = c.Error(err)
	}

	// 渠道统一按渠道协议返回 200 及对应报文内容。
	c.Data(http.StatusOK, ack.ContentType, []byte(ack.Body))
}

// HandleAlipayReturn 接收支付宝前端客户端同步重定向跳转 (return_url)。
//
// @Summary      支付宝客户端同步重定向跳转
// @Description  用户在支付宝完成付款后由浏览器重定向跳回，经公钥验签通过后 302 跳转至前端订单结果展示页。仅作展示导流，实际支付状态严格以异步通知为准。
// @Tags         支付中心
// @Accept       html
// @Produce      html
// @Success      302 {string} string "重定向至前端订单展示页"
// @x-order      2
// @Router       /payments/return/alipay [get]
func (h *Handler) HandleAlipayReturn(c *gin.Context) {
	redirectURL, err := h.service.HandleAlipayReturn(c.Request.Context(), c.Request)
	if err != nil {
		_ = c.Error(err)
		log := logger.FromContext(c.Request.Context())
		log.Warn().Err(err).Msg("alipay return verification failed, redirecting to order failure page")

		c.Redirect(http.StatusFound, h.service.FailureRedirectURL())
		return
	}

	c.Redirect(http.StatusFound, redirectURL)
}
