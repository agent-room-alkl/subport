package model

// PaymentOrder is one recharge attempt against an external (or mock) provider.
type PaymentOrder struct {
	ID               string `json:"id"`
	UserID           string `json:"user_id"`
	Provider         string `json:"provider"`
	PackageID        string `json:"package_id"`
	AmountFiatCents  int64  `json:"amount_fiat_cents"`
	Currency         string `json:"currency"`
	QuotaCredit      int64  `json:"quota_credit"`
	Status           string `json:"status"` // pending | paid | expired | cancelled
	ProviderTradeNo  string `json:"provider_trade_no"`
	PayURL           string `json:"pay_url"`
	IdempotencyKey   string `json:"idempotency_key"`
	CreatedAt        string `json:"created_at"`
	PaidAt           string `json:"paid_at"`
	ExpiresAt        string `json:"expires_at"`
}

const (
	OrderPending   = "pending"
	OrderPaid      = "paid"
	OrderExpired   = "expired"
	OrderCancelled = "cancelled"

	ProviderAlipay      = "alipay"
	ProviderAdminManual = "admin_manual"

	TopupSourceAlipay = "alipay"
	TopupSourceAdmin  = "admin"
	TopupSourceMock   = "mock"
)

// QuotaTopup is an audit row for every credit applied to a user.
type QuotaTopup struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	OrderID    string `json:"order_id"`
	Credit     int64  `json:"credit"`
	Source     string `json:"source"` // admin | alipay | mock
	OperatorID string `json:"operator_id"`
	Note       string `json:"note"`
	CreatedAt  string `json:"created_at"`
}

// RechargePackage is a priced credit bundle shown in the console.
type RechargePackage struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	AmountFiatCents int64  `json:"amount_fiat_cents"`
	Currency        string `json:"currency"`
	QuotaCredit     int64  `json:"quota_credit"`
}
