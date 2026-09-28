package payment

// AlipayReturnQuery 描述支付宝同步跳转 (return_url) 携带的查询参数。
type AlipayReturnQuery struct {
	// 商户订单号
	OutTradeNo string `form:"out_trade_no" json:"out_trade_no" example:"ORDER_20260927_001" validate:"required"`
	// 支付宝交易凭证号
	TradeNo string `form:"trade_no" json:"trade_no" example:"2026092722001400000000000000"`
	// 交易金额 (元)
	TotalAmount string `form:"total_amount" json:"total_amount" example:"88.00"`
}
