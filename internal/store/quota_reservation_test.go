package store_test

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/agent-room-alkl/subport/internal/model"
	"github.com/agent-room-alkl/subport/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.OpenStore(filepath.Join(t.TempDir(), "quota.json"), "http://127.0.0.1:9")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// burn spends quota down so the user sits exactly `remaining` short of total.
func burn(t *testing.T, s *store.Store, u model.User, remaining int64) {
	t.Helper()
	if err := s.AddUsage(model.UsageLog{
		UserID: u.ID, KeyID: "k", Model: "m",
		Tokens: u.QuotaTotal - remaining, Cost: u.QuotaTotal - remaining,
		Status: "success", AccountID: "a", Attempts: 1,
	}); err != nil {
		t.Fatalf("burn: %v", err)
	}
}

// THE BUG. One unit of quota left, many requests arriving at once. Under the
// old read-then-call-then-bill sequence every one of them read the same
// quota_used and every one of them passed. Admission has to be the thing that
// is atomic, not the billing that happens 120 seconds later.
func TestConcurrentAdmissionCannotOvershootQuota(t *testing.T) {
	s := openTestStore(t)
	u, err := s.CreateUser("racer", "pw", model.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	burn(t, s, u, 1) // exactly 1 quota remains

	const K = 32
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		admitted int
		refused  int
		other    []error
	)
	start := make(chan struct{})
	for i := 0; i < K; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release them all at once, or they queue up politely
			err := s.ReserveQuota(u.ID, 1)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				admitted++
			case errors.Is(err, store.ErrQuotaExhausted):
				refused++
			default:
				other = append(other, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if len(other) > 0 {
		t.Fatalf("unexpected errors: %v", other)
	}
	if admitted != 1 {
		t.Errorf("admitted %d of %d requests, want exactly 1 - the rest overshot the quota", admitted, K)
	}
	if refused != K-1 {
		t.Errorf("refused %d, want %d", refused, K-1)
	}

	after, err := s.UserByID(u.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if got := after.QuotaUsed + after.QuotaReserved; got > after.QuotaTotal {
		t.Errorf("used+reserved = %d exceeds total %d", got, after.QuotaTotal)
	}
}

// A reservation that is never settled is quota the user has silently lost, so
// the failure path has to give it back exactly.
func TestFailedCallLeavesQuotaExactlyWhereItStarted(t *testing.T) {
	s := openTestStore(t)
	u, _ := s.CreateUser("failer", "pw", model.RoleUser)

	before, _ := s.UserByID(u.ID)
	if err := s.ReserveQuota(u.ID, 5000); err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	held, _ := s.UserByID(u.ID)
	if held.QuotaReserved != 5000 {
		t.Fatalf("QuotaReserved = %d, want 5000 - nothing was actually held", held.QuotaReserved)
	}
	if held.QuotaUsed != before.QuotaUsed {
		t.Errorf("a reservation charged the user: quota_used moved %d -> %d", before.QuotaUsed, held.QuotaUsed)
	}

	if err := s.ReleaseQuota(u.ID, 5000); err != nil {
		t.Fatalf("ReleaseQuota: %v", err)
	}
	after, _ := s.UserByID(u.ID)
	if after.QuotaUsed != before.QuotaUsed || after.QuotaReserved != 0 {
		t.Errorf("after release: used=%d reserved=%d, want used=%d reserved=0",
			after.QuotaUsed, after.QuotaReserved, before.QuotaUsed)
	}
}

// Reserving is a guess; the provider's usage count is the truth. Settling must
// charge the truth and hand back the whole guess, or every over-estimate would
// quietly eat quota.
func TestOverEstimateIsReleasedAndOnlyRealCostIsCharged(t *testing.T) {
	s := openTestStore(t)
	u, _ := s.CreateUser("estimator", "pw", model.RoleUser)

	if err := s.ReserveQuota(u.ID, 10_000); err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	if err := s.SettleUsage(model.UsageLog{
		UserID: u.ID, KeyID: "k", Model: "m",
		Tokens: 42, Cost: 42,
		Status: "success", AccountID: "a", Attempts: 1,
	}, 10_000); err != nil {
		t.Fatalf("SettleUsage: %v", err)
	}

	after, _ := s.UserByID(u.ID)
	if after.QuotaUsed != 42 {
		t.Errorf("quota_used = %d, want 42 - the estimate was billed instead of the real cost", after.QuotaUsed)
	}
	if after.QuotaReserved != 0 {
		t.Errorf("quota_reserved = %d after settling, want 0 - the hold leaked", after.QuotaReserved)
	}
	if logs := s.UsageOf(u.ID, 10); len(logs) != 1 || logs[0].Tokens != 42 {
		t.Errorf("usage log not recorded correctly: %+v", logs)
	}
}

// Under-estimating must not be silently capped either: the user made the call,
// the call cost what it cost.
func TestUnderEstimateStillChargesTheRealCost(t *testing.T) {
	s := openTestStore(t)
	u, _ := s.CreateUser("under", "pw", model.RoleUser)

	if err := s.ReserveQuota(u.ID, 100); err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	if err := s.SettleUsage(model.UsageLog{
		UserID: u.ID, KeyID: "k", Model: "m", Tokens: 7_500, Cost: 7_500,
		Status: "success", AccountID: "a", Attempts: 1,
	}, 100); err != nil {
		t.Fatalf("SettleUsage: %v", err)
	}
	after, _ := s.UserByID(u.ID)
	if after.QuotaUsed != 7_500 || after.QuotaReserved != 0 {
		t.Errorf("used=%d reserved=%d, want used=7500 reserved=0", after.QuotaUsed, after.QuotaReserved)
	}
}

// Releasing twice must not manufacture quota. Floor the reservation at zero.
func TestDoubleReleaseCannotCreateQuota(t *testing.T) {
	s := openTestStore(t)
	u, _ := s.CreateUser("doubler", "pw", model.RoleUser)

	_ = s.ReserveQuota(u.ID, 300)
	_ = s.ReleaseQuota(u.ID, 300)
	_ = s.ReleaseQuota(u.ID, 300)

	after, _ := s.UserByID(u.ID)
	if after.QuotaReserved != 0 {
		t.Errorf("quota_reserved = %d, want 0", after.QuotaReserved)
	}
}

// Unlimited (quota_total <= 0) must stay unlimited; the conditional UPDATE has
// to special-case it or every such user would be refused at zero.
func TestUnlimitedUserIsAlwaysAdmitted(t *testing.T) {
	s := openTestStore(t)
	u, _ := s.CreateUser("unlimited", "pw", model.RoleUser)
	burn(t, s, u, 0) // used == total

	if err := s.ReserveQuota(u.ID, 1); !errors.Is(err, store.ErrQuotaExhausted) {
		t.Fatalf("a user at their limit should be refused, got %v", err)
	}
	// Same store, a user whose total is unlimited.
	if err := s.SetQuotaTotal(u.ID, 0); err != nil {
		t.Skipf("no SetQuotaTotal in this build: %v", err)
	}
	if err := s.ReserveQuota(u.ID, 1_000_000_000); err != nil {
		t.Errorf("unlimited user refused: %v", err)
	}
}

// Reserving for a user who does not exist is a different bug from reserving
// too much, and must not be reported as quota exhaustion.
func TestUnknownUserIsNotReportedAsQuotaExhausted(t *testing.T) {
	s := openTestStore(t)
	err := s.ReserveQuota("usr_does_not_exist", 1)
	if errors.Is(err, store.ErrQuotaExhausted) {
		t.Fatal("a missing user was reported as quota exhausted; that sends an operator hunting the wrong thing")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
