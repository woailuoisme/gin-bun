package payment

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/samber/oops"

	"bgin/internal/config"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/logger"
	platformPayment "bgin/internal/platform/payment"
	"bgin/internal/platform/payment/enums"
)

const (
	// notifyLockTTL 是防止渠道并发通知重放的分布式锁持有时间。
	notifyLockTTL = 30 * time.Second
	// notifyDoneTTL 是已消费通知的去重标记保留时长。
	// 渠道自带的阶梯重试可能跨越数小时甚至数天，标记必须比锁活得久，
	// 否则超出锁有效期后的重放会再次进入业务状态机。
	notifyDoneTTL = 7 * 24 * time.Hour
	// errUnsupportedChannel 是渠道不合法时的统一对外口径。
	// HTTP 边界（Handler）与服务层都做这项校验，共用同一文案以免两处措辞漂移。
	errUnsupportedChannel = "Unsupported payment channel"
)

// CallbackHandler 是支付与退款业务消费钩子接口。
// 业务切片或聚合根通过实现此接口接收成功状态并推进领域状态机。
type CallbackHandler interface {
	// OnPaymentSuccess 处理支付成功回调通知。返回 nil 代表业务成功消费，由网关向渠道返回成功 ACK。
	OnPaymentSuccess(ctx context.Context, result *platformPayment.PaymentResult) error
	// OnRefundSuccess 处理退款成功或状态变更通知。返回 nil 代表业务成功消费。
	OnRefundSuccess(ctx context.Context, result *platformPayment.RefundResult) error
}

// defaultCallbackHandler 是默认的空操作业务处理器，记录日志并返回成功。
type defaultCallbackHandler struct{}

func (d *defaultCallbackHandler) OnPaymentSuccess(ctx context.Context, result *platformPayment.PaymentResult) error {
	log := logger.FromContext(ctx)
	log.Info().
		Str("out_trade_no", result.OutTradeNo).
		Str("channel_trade_no", result.ChannelTradeNo).
		Int64("paid_amount", result.PaidAmount).
		Msg("payment success callback received (using default noop handler)")

	return nil
}

func (d *defaultCallbackHandler) OnRefundSuccess(ctx context.Context, result *platformPayment.RefundResult) error {
	log := logger.FromContext(ctx)
	log.Info().
		Str("out_trade_no", result.OutTradeNo).
		Str("out_refund_no", result.OutRefundNo).
		Str("channel_refund_no", result.ChannelRefundNo).
		Int64("refund_amount", result.RefundAmount).
		Str("status", string(result.Status)).
		Msg("refund callback received (using default noop handler)")

	return nil
}

// Service 负责协调支付回调的验签、分布式幂等防重与业务事件分发。
type Service struct {
	paymentMgr *platformPayment.Manager
	cache      *cache.Client
	cfg        *config.Config
	handler    CallbackHandler
}

// NewService 构造支付回调协调服务。
func NewService(paymentMgr *platformPayment.Manager, cacheClient *cache.Client, cfg *config.Config) *Service {
	return &Service{
		paymentMgr: paymentMgr,
		cache:      cacheClient,
		cfg:        cfg,
		handler:    &defaultCallbackHandler{},
	}
}

// SetCallbackHandler 注册业务消费钩子实现。
func (s *Service) SetCallbackHandler(handler CallbackHandler) {
	if handler != nil {
		s.handler = handler
	}
}

// HandleNotification 统一处理微信与支付宝的异步通知回调。
func (s *Service) HandleNotification(
	ctx context.Context,
	channelStr, appKey string,
	req *http.Request,
) (platformPayment.NotificationAck, error) {
	log := logger.FromContext(ctx)

	channel := enums.PaymentChannel(channelStr)
	if !channel.IsValid() {
		// 此处刻意不重复打日志：HTTP 边界已就同一事件留下带渠道字段的告警，
		// 服务层只负责对程序化调用方返回可判定的错误。
		return platformPayment.FailureAck(channel), errs.BadRequest(errUnsupportedChannel)
	}

	if s.paymentMgr == nil {
		return platformPayment.FailureAck(channel), errs.ServiceUnavailable("payment gateway is not initialized")
	}

	// 1. 严格验签并解析报文 (微信商户证书/微信平台证书/支付宝公钥证书)
	notification, err := s.paymentMgr.ParseNotification(ctx, channel, appKey, req)
	if err != nil {
		log.Warn().
			Err(err).
			Str("channel", channel.String()).
			Str("app_key", appKey).
			Msg("payment callback verification or parsing failed")

		return platformPayment.FailureAck(channel), oops.
			Code(string(errs.CodeBadRequest)).
			With("channel", channel.String()).
			With("app_key", appKey).
			Wrap(err)
	}

	// 2. 幂等与并发防重放控制
	tradeNo := s.extractTradeNo(notification)
	if s.cache != nil && tradeNo != "" {
		// 已消费过的通知直接回成功应答：通道会重放同一通知（超时重试、人工补发等），
		// 此时再走一遍业务状态机会造成重复入账，回失败应答又会触发渠道无限重试。
		doneKey := cache.Key("payment", "notify", "done", channel.String(), tradeNo)
		if _, getErr := s.cache.Get(ctx, doneKey); getErr == nil {
			log.Warn().
				Str("channel", channel.String()).
				Str("trade_no", tradeNo).
				Msg("callback notification already consumed, acknowledged without reprocessing")

			return platformPayment.SuccessAck(channel), nil
		}

		lockKey := cache.Key("payment", "notify", "lock", channel.String(), tradeNo)
		acquired, lockErr := s.cache.SetNX(ctx, lockKey, "1", notifyLockTTL)
		if lockErr == nil && !acquired {
			log.Warn().
				Str("channel", channel.String()).
				Str("trade_no", tradeNo).
				Msg("concurrent callback notification detected, skipping duplicate processing")

			return platformPayment.FailureAck(channel), errs.Conflict("duplicate notification in progress")
		}
	}

	// 3. 分发到具体业务处理器执行状态流转
	var handleErr error
	switch notification.Kind {
	case enums.PaymentNotificationKindPayment:
		if notification.Payment == nil {
			return platformPayment.FailureAck(channel), errs.BadRequest("payment notification payload is missing")
		}
		handleErr = s.handler.OnPaymentSuccess(ctx, notification.Payment)

	case enums.PaymentNotificationKindRefund:
		if notification.Refund == nil {
			return platformPayment.FailureAck(channel), errs.BadRequest("refund notification payload is missing")
		}
		handleErr = s.handler.OnRefundSuccess(ctx, notification.Refund)

	default:
		return platformPayment.FailureAck(channel), errs.BadRequest("unsupported payment notification kind: " + notification.Kind.String())
	}

	// 4. 业务处理失败时，向渠道返回 Failure 触发渠道自带的阶梯重试机制
	if handleErr != nil {
		log.Error().
			Err(handleErr).
			Str("channel", channel.String()).
			Str("kind", notification.Kind.String()).
			Str("trade_no", tradeNo).
			Msg("payment callback consumption failed, returning failure ack for retry")

		return platformPayment.FailureAck(channel), oops.
			Code(string(errs.CodeInternal)).
			With("channel", channel.String()).
			With("kind", notification.Kind.String()).
			With("trade_no", tradeNo).
			Wrap(handleErr)
	}

	log.Info().
		Str("channel", channel.String()).
		Str("kind", notification.Kind.String()).
		Str("trade_no", tradeNo).
		Msg("payment callback notification successfully processed")

	// 5. 落持久去重标记：并发锁只覆盖处理窗口，标记覆盖渠道跨越数天的重试与人工补发。
	if s.cache != nil && tradeNo != "" {
		doneKey := cache.Key("payment", "notify", "done", channel.String(), tradeNo)
		if err := s.cache.Set(ctx, doneKey, "1", notifyDoneTTL); err != nil {
			log.Warn().Err(err).Str("trade_no", tradeNo).Msg("failed to persist payment notification dedup marker")
		}
	}

	return platformPayment.SuccessAck(channel), nil
}

// FailureRedirectURL 返回支付失败后引导用户回到的订单结果页地址。
// 由 Service 统一拼装，Handler 只负责跳转，避免在 HTTP 层读取服务内部配置。
func (s *Service) FailureRedirectURL() string {
	if s == nil || s.cfg == nil {
		return "/orders?status=failed"
	}
	return strings.TrimRight(s.cfg.App.URL, "/") + "/orders?status=failed"
}

// HandleAlipayReturn 处理支付宝前端同步重定向 (return_url)。
// 该接口仅供浏览器跳转导流，不能作为最终支付成功的凭据，实际结果严格以异步通知为准。
func (s *Service) HandleAlipayReturn(ctx context.Context, req *http.Request) (string, error) {
	log := logger.FromContext(ctx)

	if s.paymentMgr == nil {
		return "", errs.ServiceUnavailable("payment gateway is not initialized")
	}

	// 对 URL Query 参数进行支付宝公钥验签
	notification, err := s.paymentMgr.ParseNotification(ctx, enums.PaymentChannelAlipay, "", req)
	if err != nil {
		log.Warn().Err(err).Msg("alipay return callback verification failed")
		return "", oops.Code(string(errs.CodeBadRequest)).Wrap(err)
	}

	outTradeNo := ""
	if notification.Payment != nil {
		outTradeNo = notification.Payment.OutTradeNo
	}
	if outTradeNo == "" {
		outTradeNo = req.URL.Query().Get("out_trade_no")
	}

	// 构造前端重定向目标地址
	baseURL := strings.TrimRight(s.cfg.App.URL, "/")
	redirectURL := fmt.Sprintf("%s/orders/%s?status=success", baseURL, outTradeNo)

	log.Info().
		Str("out_trade_no", outTradeNo).
		Str("redirect_url", redirectURL).
		Msg("alipay return callback verified, redirecting to frontend")

	return redirectURL, nil
}

// extractTradeNo 提取用于并发排重的交易标识号。
func (s *Service) extractTradeNo(n *platformPayment.Notification) string {
	if n == nil {
		return ""
	}
	if n.Payment != nil && n.Payment.OutTradeNo != "" {
		return n.Payment.OutTradeNo
	}
	if n.Refund != nil && n.Refund.OutRefundNo != "" {
		return n.Refund.OutRefundNo
	}

	return n.ID
}
