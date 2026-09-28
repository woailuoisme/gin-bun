package payment

// CallbackAckResponse 描述支付网关向渠道返回的应答状态。
type CallbackAckResponse struct {
	// 应答状态码或纯文本
	Status string `json:"status" example:"success"`
	// 应答说明
	Message string `json:"message" example:"OK"`
}
