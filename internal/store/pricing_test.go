package store_test

import (
	"path/filepath"
	"testing"

	"github.com/agent-room-alkl/subport/internal/model"
	"github.com/agent-room-alkl/subport/internal/store"
)

func openPricingStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.OpenStore(filepath.Join(t.TempDir(), "pricing.json"), "http://127.0.0.1:9")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The shape of a real price list, taken from a live competitor's page rather
// than invented: four token classes priced separately, times a group ratio.
// 0.16x is the ratio that page actually advertises.
func TestCostAtAKnownPriceList(t *testing.T) {
	s := openPricingStore(t)
	u, _ := s.CreateUser("payer", "pw", model.RoleUser)

	if err := s.SetModelPrice(model.ModelPrice{
		Model: "claude-sonnet-5",
		Input: 2_000, Output: 10_000, CacheRead: 200, CacheWrite: 2_500,
	}); err != nil {
		t.Fatalf("SetModelPrice: %v", err)
	}
	if err := s.SetGroupRatio("cheap", 1_600); err != nil { // 0.16x
		t.Fatalf("SetGroupRatio: %v", err)
	}
	if err := s.SetUserBilling(u.ID, "cheap", 0); err != nil {
		t.Fatalf("SetUserBilling: %v", err)
	}

	// 1M input, 1M output, at 0.16x:
	//   (1e6*2000 + 1e6*10000) / 1e6 = 12000, * 0.16 = 1920
	counts := model.TokenCounts{Input: 1_000_000, Output: 1_000_000, Total: 2_000_000}
	cost, rate := s.PriceCall(u.ID, "claude-sonnet-5", counts)
	if cost != 1_920 {
		t.Errorf("cost = %d, want 1920", cost)
	}
	if rate.Ratio != 1_600 {
		t.Errorf("snapshot ratio = %d, want 1600", rate.Ratio)
	}
	if rate.Input != 2_000 || rate.Output != 10_000 {
		t.Errorf("snapshot rates wrong: %+v", rate)
	}
}

// THE ONE THAT MATTERS. A bill is a statement about the past. If a usage row
// only referenced a price list, editing prices today would silently rewrite
// every invoice already issued - and nobody would notice until a customer
// compared two exports of the same month and found different totals.
func TestEditingThePriceListDoesNotRewriteAnOldBill(t *testing.T) {
	s := openPricingStore(t)
	u, _ := s.CreateUser("historian", "pw", model.RoleUser)
	_ = s.SetModelPrice(model.FlatPrice("gpt-4o", 1_000))

	counts := model.TokenCounts{Input: 1_000_000, Total: 1_000_000}
	cost, rate := s.PriceCall(u.ID, "gpt-4o", counts)
	if cost != 1_000 {
		t.Fatalf("cost = %d, want 1000", cost)
	}
	if err := s.SettleUsage(model.UsageLog{
		UserID: u.ID, KeyID: "k", Model: "gpt-4o",
		Tokens: counts.Total, TokenParts: counts, BilledAt: rate, Cost: cost,
		Status: "success", AccountID: "a", Attempts: 1,
	}, 0); err != nil {
		t.Fatalf("SettleUsage: %v", err)
	}

	// Prices go up tenfold after the fact.
	if err := s.SetModelPrice(model.FlatPrice("gpt-4o", 10_000)); err != nil {
		t.Fatalf("SetModelPrice: %v", err)
	}

	logs := s.UsageOf(u.ID, 10)
	if len(logs) != 1 {
		t.Fatalf("want 1 usage row, got %d", len(logs))
	}
	if logs[0].Cost != 1_000 {
		t.Errorf("an already-issued bill changed when prices changed: cost = %d, want 1000", logs[0].Cost)
	}
	if logs[0].BilledAt.Input != 1_000 {
		t.Errorf("the rate snapshot was not preserved: %+v", logs[0].BilledAt)
	}
	// And the user's quota, which was charged at the old price, is untouched.
	after, _ := s.UserByID(u.ID)
	if after.QuotaUsed != 1_000 {
		t.Errorf("quota_used = %d, want 1000", after.QuotaUsed)
	}
}

func TestPersonalRatioOverridesTheGroup(t *testing.T) {
	s := openPricingStore(t)
	u, _ := s.CreateUser("vip", "pw", model.RoleUser)
	_ = s.SetModelPrice(model.FlatPrice("gpt-4o", 1_000))
	_ = s.SetGroupRatio("standard", 10_000) // 1.0x
	_ = s.SetUserBilling(u.ID, "standard", 0)

	counts := model.TokenCounts{Input: 1_000_000, Total: 1_000_000}
	if cost, _ := s.PriceCall(u.ID, "gpt-4o", counts); cost != 1_000 {
		t.Fatalf("group price wrong: %d", cost)
	}

	// Same user, now with a personal deal at 0.5x.
	_ = s.SetUserBilling(u.ID, "standard", 5_000)
	cost, rate := s.PriceCall(u.ID, "gpt-4o", counts)
	if cost != 500 {
		t.Errorf("cost = %d, want 500 - the personal override did not win", cost)
	}
	if rate.Ratio != 5_000 {
		t.Errorf("snapshot ratio = %d, want 5000", rate.Ratio)
	}
}

// A missing group, a missing price, a missing user: none of them may mean
// free. Unpriced must fall back to list price, because the failure mode of
// "free" is unbounded and silent.
func TestMissingConfigurationIsListPriceNotFree(t *testing.T) {
	s := openPricingStore(t)
	u, _ := s.CreateUser("nobody", "pw", model.RoleUser)
	counts := model.TokenCounts{Input: 1_000_000, Total: 1_000_000}

	if got := s.EffectiveRatio(u.ID); got != store.DefaultRatio {
		t.Errorf("ratio with no group = %d, want %d", got, store.DefaultRatio)
	}
	if got := s.EffectiveRatio("usr_does_not_exist"); got != store.DefaultRatio {
		t.Errorf("ratio for an unknown user = %d, want %d", got, store.DefaultRatio)
	}
	// A model with no rate card at all.
	if cost, _ := s.PriceCall(u.ID, "some-model-nobody-priced", counts); cost != 1_000_000 {
		t.Errorf("unpriced model cost = %d, want 1000000 (1:1), not free", cost)
	}
	// A group name that was never defined.
	_ = s.SetUserBilling(u.ID, "typo-group", 0)
	if got := s.EffectiveRatio(u.ID); got != store.DefaultRatio {
		t.Errorf("ratio for an undefined group = %d, want list price", got)
	}
}

// Some providers report only a total. Splitting it by a guess would look
// precise while being made up, so the total is billed whole - but it MUST
// still be billed.
func TestTotalOnlyReportStillBills(t *testing.T) {
	s := openPricingStore(t)
	u, _ := s.CreateUser("totaller", "pw", model.RoleUser)
	_ = s.SetModelPrice(model.FlatPrice("gpt-4o", 3_000))

	counts := model.TokenCounts{Total: 500_000} // no breakdown at all
	cost, _ := s.PriceCall(u.ID, "gpt-4o", counts)
	if cost != 1_500 {
		t.Errorf("cost = %d, want 1500 - a total-only report must not bill zero", cost)
	}
}

// Rounding is stated so it can be argued with: half away from zero, applied
// once at the end. Rounding each class separately would let four half-units
// become two whole ones on a call that consumed almost nothing.
func TestRoundingIsHalfAwayFromZeroAndAppliedOnce(t *testing.T) {
	rate := model.RateFor(model.FlatPrice("m", 1), model.RatioScale) // 1 per million, 1.0x

	cases := []struct {
		tokens int64
		want   int64
	}{
		{499_999, 0},   // 0.499999 -> 0
		{500_000, 1},   // exactly half -> away from zero
		{500_001, 1},   // 0.500001 -> 1
		{1_499_999, 1}, // 1.499999 -> 1
		{1_500_000, 2}, // 1.5 -> 2
	}
	for _, tc := range cases {
		got := model.Cost(model.TokenCounts{Total: tc.tokens}, rate)
		if got != tc.want {
			t.Errorf("Cost(%d tokens) = %d, want %d", tc.tokens, got, tc.want)
		}
	}

	// Four classes each landing on a half must round ONCE, together: 4 x 0.5
	// is 2, not 4.
	quarters := model.TokenCounts{Input: 500_000, Output: 500_000, CacheRead: 500_000, CacheWrite: 500_000}
	if got := model.Cost(quarters, rate); got != 2 {
		t.Errorf("four half-units cost %d, want 2 - rounding was applied per class", got)
	}
}

// A negative rate or ratio is a configuration mistake. Charging a negative
// amount would credit quota out of nothing, which is a much worse outcome than
// charging nothing.
func TestNegativeConfigurationCannotCreditQuota(t *testing.T) {
	counts := model.TokenCounts{Total: 1_000_000}
	if got := model.Cost(counts, model.RateFor(model.FlatPrice("m", 1_000), -5_000)); got < 0 {
		t.Errorf("negative ratio produced cost %d", got)
	}
	if got := model.Cost(counts, model.RateFor(model.FlatPrice("m", -1_000), model.RatioScale)); got < 0 {
		t.Errorf("negative rate produced cost %d", got)
	}
}

// Rows written before any of this existed have no parts and no snapshot. They
// must still read back, still show their cost, and still be compensable.
func TestLegacyRowsWithoutPartsStillWork(t *testing.T) {
	s := openPricingStore(t)
	u, _ := s.CreateUser("oldtimer", "pw", model.RoleUser)

	if err := s.AddUsage(model.UsageLog{
		UserID: u.ID, KeyID: "k", Model: "gpt-4o",
		Tokens: 42, Cost: 42, Status: "success", AccountID: "a", Attempts: 1,
	}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}
	logs := s.UsageOf(u.ID, 10)
	if len(logs) != 1 || logs[0].Cost != 42 || logs[0].Tokens != 42 {
		t.Fatalf("legacy row did not round-trip: %+v", logs)
	}
	if logs[0].TokenParts.HasBreakdown() {
		t.Error("a legacy row reported a breakdown it never had")
	}
	if logs[0].TokenParts.Total != 42 {
		t.Errorf("Total should mirror Tokens for legacy rows, got %d", logs[0].TokenParts.Total)
	}
}
