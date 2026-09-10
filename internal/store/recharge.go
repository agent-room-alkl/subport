package store

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

// ListUsers returns every user row (caller must strip secrets via PublicUser).
func (s *Store) ListUsers() []model.User {
	s.mu.Lock()
	defer s.mu.Unlock()
	users, err := s.sqliteListUsers()
	if err != nil || users == nil {
		return []model.User{}
	}
	return users
}

// AddQuotaCredit raises quota_total by credit in one UPDATE and returns new totals.
// Prefer this for paid top-ups so historical quota_used stays meaningful
// (unlike compensation which reduces quota_used via sqliteCreditQuota).
func (s *Store) AddQuotaCredit(userID string, credit int64) (total, used int64, err error) {
	if credit <= 0 {
		return 0, 0, errors.New("credit must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteAddQuotaCredit(userID, credit)
}

// CreatePaymentOrder inserts a pending order. If IdempotencyKey is set and a
// matching row already exists for the user, that row is returned instead.
func (s *Store) CreatePaymentOrder(o model.PaymentOrder) (model.PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if o.IdempotencyKey != "" {
		existing, err := s.sqliteGetPaymentOrderByIdem(o.UserID, o.IdempotencyKey)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return model.PaymentOrder{}, err
		}
	}
	if o.ID == "" {
		o.ID = NewID("pay")
	}
	if o.Status == "" {
		o.Status = model.OrderPending
	}
	if o.CreatedAt == "" {
		o.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := s.sqliteCreatePaymentOrder(o); err != nil {
		return model.PaymentOrder{}, err
	}
	return o, nil
}

func (s *Store) GetPaymentOrder(id string) (model.PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteGetPaymentOrder(id)
}

func (s *Store) SetPaymentOrderPayURL(id, payURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteUpdateOrderPayURL(id, payURL)
}

// MarkOrderPaidAndCredit is idempotent: only the first pending→paid transition
// inserts a topup and adds credit. Returns credited=true when credit was applied.
func (s *Store) MarkOrderPaidAndCredit(orderID, tradeNo, source, operatorID, note string) (model.PaymentOrder, model.QuotaTopup, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if source == "" {
		source = model.TopupSourceAlipay
	}
	return s.sqliteMarkOrderPaidAndCredit(orderID, tradeNo, source, operatorID, note)
}

// AdminManualTopup credits a user and writes an audit topup (source=admin).
// Optionally records a synthetic paid order with provider=admin_manual.
func (s *Store) AdminManualTopup(userID string, credit int64, operatorID, note string, withOrder bool) (model.QuotaTopup, model.PaymentOrder, error) {
	if credit <= 0 {
		return model.QuotaTopup{}, model.PaymentOrder{}, errors.New("credit must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	var order model.PaymentOrder
	orderID := ""
	if withOrder {
		order = model.PaymentOrder{
			ID:              NewID("pay"),
			UserID:          userID,
			Provider:        model.ProviderAdminManual,
			PackageID:       "admin",
			AmountFiatCents: 0,
			Currency:        "CNY",
			QuotaCredit:     credit,
			Status:          model.OrderPaid,
			CreatedAt:       now,
			PaidAt:          now,
		}
		if err := s.sqliteCreatePaymentOrder(order); err != nil {
			return model.QuotaTopup{}, model.PaymentOrder{}, err
		}
		orderID = order.ID
	}

	if _, _, err := s.sqliteAddQuotaCredit(userID, credit); err != nil {
		return model.QuotaTopup{}, model.PaymentOrder{}, err
	}
	top := model.QuotaTopup{
		ID:         NewID("top"),
		UserID:     userID,
		OrderID:    orderID,
		Credit:     credit,
		Source:     model.TopupSourceAdmin,
		OperatorID: operatorID,
		Note:       note,
		CreatedAt:  now,
	}
	if err := s.sqliteInsertTopup(top); err != nil {
		return model.QuotaTopup{}, model.PaymentOrder{}, err
	}
	return top, order, nil
}

func (s *Store) ListTopups(limit int) []model.QuotaTopup {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.sqliteListTopups(limit)
	if err != nil || out == nil {
		return []model.QuotaTopup{}
	}
	return out
}

func (s *Store) ListTopupsOf(userID string, limit int) []model.QuotaTopup {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.sqliteListTopupsOf(userID, limit)
	if err != nil || out == nil {
		return []model.QuotaTopup{}
	}
	return out
}

func (s *Store) ListPaymentOrders(limit int) []model.PaymentOrder {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.sqliteListPaymentOrders(limit)
	if err != nil || out == nil {
		return []model.PaymentOrder{}
	}
	return out
}

// DefaultRechargePackages are used when SUBPORT_RECHARGE_PACKAGES is unset.
func DefaultRechargePackages() []model.RechargePackage {
	return []model.RechargePackage{
		{ID: "p10", Label: "10 CNY", AmountFiatCents: 1000, Currency: "CNY", QuotaCredit: 200_000},
		{ID: "p50", Label: "50 CNY", AmountFiatCents: 5000, Currency: "CNY", QuotaCredit: 1_000_000},
		{ID: "p100", Label: "100 CNY", AmountFiatCents: 10000, Currency: "CNY", QuotaCredit: 2_200_000},
	}
}

// LoadRechargePackages reads SUBPORT_RECHARGE_PACKAGES JSON or returns defaults.
func LoadRechargePackages() []model.RechargePackage {
	raw := strings.TrimSpace(os.Getenv("SUBPORT_RECHARGE_PACKAGES"))
	if raw == "" {
		return DefaultRechargePackages()
	}
	var pkgs []model.RechargePackage
	if err := json.Unmarshal([]byte(raw), &pkgs); err != nil || len(pkgs) == 0 {
		return DefaultRechargePackages()
	}
	return pkgs
}

// PackageByID looks up a package from the configured list.
func PackageByID(id string) (model.RechargePackage, bool) {
	for _, p := range LoadRechargePackages() {
		if p.ID == id {
			return p, true
		}
	}
	return model.RechargePackage{}, false
}
