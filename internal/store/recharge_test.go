package store_test

import (
	"testing"

	"github.com/agent-room-alkl/subport/internal/model"
)

func TestMarkOrderPaidAndCreditIdempotent(t *testing.T) {
	s := openTestStore(t)
	u, err := s.CreateUser("payer", "password1", model.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	before, err := s.UserByID(u.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}

	order, err := s.CreatePaymentOrder(model.PaymentOrder{
		UserID:          u.ID,
		Provider:        model.ProviderAlipay,
		PackageID:       "p50",
		AmountFiatCents: 5000,
		Currency:        "CNY",
		QuotaCredit:     1_000_000,
		Status:          model.OrderPending,
	})
	if err != nil {
		t.Fatalf("CreatePaymentOrder: %v", err)
	}

	_, top1, credited1, err := s.MarkOrderPaidAndCredit(order.ID, "trade-1", model.TopupSourceAlipay, "", "first")
	if err != nil {
		t.Fatalf("first mark: %v", err)
	}
	if !credited1 {
		t.Fatal("expected first mark to credit")
	}
	if top1.Credit != 1_000_000 {
		t.Fatalf("topup credit = %d", top1.Credit)
	}

	after1, _ := s.UserByID(u.ID)
	if after1.QuotaTotal != before.QuotaTotal+1_000_000 {
		t.Fatalf("quota_total after first = %d want %d", after1.QuotaTotal, before.QuotaTotal+1_000_000)
	}
	if after1.QuotaUsed != before.QuotaUsed {
		t.Fatalf("quota_used should be unchanged, got %d want %d", after1.QuotaUsed, before.QuotaUsed)
	}

	_, _, credited2, err := s.MarkOrderPaidAndCredit(order.ID, "trade-1", model.TopupSourceAlipay, "", "second")
	if err != nil {
		t.Fatalf("second mark: %v", err)
	}
	if credited2 {
		t.Fatal("second mark must not credit again")
	}
	after2, _ := s.UserByID(u.ID)
	if after2.QuotaTotal != after1.QuotaTotal {
		t.Fatalf("double notify changed total: %d -> %d", after1.QuotaTotal, after2.QuotaTotal)
	}

	tops := s.ListTopupsOf(u.ID, 10)
	if len(tops) != 1 {
		t.Fatalf("expected 1 topup, got %d", len(tops))
	}
}

func TestAddQuotaCreditIncreasesTotal(t *testing.T) {
	s := openTestStore(t)
	u, err := s.CreateUser("topupee", "password1", model.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	total, used, err := s.AddQuotaCredit(u.ID, 5000)
	if err != nil {
		t.Fatalf("AddQuotaCredit: %v", err)
	}
	if total != u.QuotaTotal+5000 {
		t.Fatalf("total=%d", total)
	}
	if used != u.QuotaUsed {
		t.Fatalf("used changed unexpectedly: %d", used)
	}
}

func TestAdminManualTopup(t *testing.T) {
	s := openTestStore(t)
	u, err := s.CreateUser("manual", "password1", model.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	top, order, err := s.AdminManualTopup(u.ID, 12345, "admin1", "gift", true)
	if err != nil {
		t.Fatalf("AdminManualTopup: %v", err)
	}
	if top.Source != model.TopupSourceAdmin {
		t.Fatalf("source=%s", top.Source)
	}
	if order.Provider != model.ProviderAdminManual || order.Status != model.OrderPaid {
		t.Fatalf("order=%+v", order)
	}
	after, _ := s.UserByID(u.ID)
	if after.QuotaTotal != u.QuotaTotal+12345 {
		t.Fatalf("total=%d", after.QuotaTotal)
	}
}
