package httpapi

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/agent-room-alkl/subport/internal/gateway"
	"github.com/agent-room-alkl/subport/internal/model"
	"github.com/agent-room-alkl/subport/internal/store"
)

func msg(role, content string) struct {
	Role    string `json:"role"`
	Content string `json:"content"`
} {
	return struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: role, Content: content}
}

func TestReservationScalesWithPromptButNeverBelowTheFloor(t *testing.T) {
	small := gateway.ChatRequest{}
	if got := reservationFor(small); got != defaultReservation {
		t.Errorf("empty request reserved %d, want the floor %d", got, defaultReservation)
	}

	big := gateway.ChatRequest{}
	big.Messages = append(big.Messages, msg("user", string(make([]byte, 100_000))))
	if got := reservationFor(big); got <= defaultReservation {
		t.Errorf("a 100k-char prompt reserved %d, no more than the floor - the estimate is not scaling", got)
	}
}

// One enormous prompt must not be able to hold a user's whole allowance for the
// duration of a call. The cap is what keeps a single request from becoming a
// denial of service against its own owner.
func TestReservationIsCapped(t *testing.T) {
	t.Setenv("SUBPORT_MAX_RESERVATION", "10000")
	s := newQuotaTestStore(t)
	u, _ := s.CreateUser("capped", "pw", model.RoleUser)

	req := gateway.ChatRequest{}
	req.Messages = append(req.Messages, msg("user", string(make([]byte, 5_000_000))))

	held, err := admit(s, u, req)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if held != 10_000 {
		t.Errorf("held %d, want the cap 10000", held)
	}
}

func newQuotaTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.OpenStore(filepath.Join(t.TempDir(), "q.json"), "http://127.0.0.1:9")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The policy layer must pass the store's refusal through unchanged. If it ever
// swallowed ErrQuotaExhausted the gateway would answer 500 for a user who is
// simply out of quota, which is the difference between "you need to top up"
// and "we are broken".
func TestAdmitSurfacesQuotaExhaustion(t *testing.T) {
	s := newQuotaTestStore(t)
	u, _ := s.CreateUser("broke", "pw", model.RoleUser)
	if err := s.SetQuotaTotal(u.ID, 10); err != nil {
		t.Fatalf("SetQuotaTotal: %v", err)
	}
	u, _ = s.UserByID(u.ID)

	_, err := admit(s, u, gateway.ChatRequest{})
	if !isQuotaExhausted(err) {
		t.Fatalf("admit err = %v, want quota exhausted", err)
	}
	if !errors.Is(err, store.ErrQuotaExhausted) {
		t.Errorf("the error lost its identity on the way out: %v", err)
	}
}

// The end the whole task exists for, expressed at the layer handleChat will
// call: many requests at once, one small allowance, and admission must not let
// through more than the allowance covers.
func TestConcurrentAdmitCannotExceedTheAllowance(t *testing.T) {
	s := newQuotaTestStore(t)
	u, _ := s.CreateUser("swarm", "pw", model.RoleUser)
	// Room for exactly two default-sized reservations.
	if err := s.SetQuotaTotal(u.ID, defaultReservation*2); err != nil {
		t.Fatalf("SetQuotaTotal: %v", err)
	}
	u, _ = s.UserByID(u.ID)

	const K = 24
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ok      int
		refused int
		other   []error
	)
	start := make(chan struct{})
	for i := 0; i < K; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := admit(s, u, gateway.ChatRequest{})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case isQuotaExhausted(err):
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
	if ok != 2 {
		t.Errorf("admitted %d of %d, want exactly 2 - the allowance covers two", ok, K)
	}
	if refused != K-2 {
		t.Errorf("refused %d, want %d", refused, K-2)
	}

	after, _ := s.UserByID(u.ID)
	if after.QuotaUsed+after.QuotaReserved > after.QuotaTotal {
		t.Errorf("used+reserved = %d exceeds total %d", after.QuotaUsed+after.QuotaReserved, after.QuotaTotal)
	}
}
