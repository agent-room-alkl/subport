package gateway

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

// MaxAttempts bounds the failover walk so a fully unhealthy pool cannot spin.
const MaxAttempts = 3

type ChatRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// RelayError carries the one fact that decides whether a retry is allowed.
type RelayError struct {
	Err           error
	FirstByteSent bool
}

func (e RelayError) Error() string { return e.Err.Error() }
func (e RelayError) Unwrap() error { return e.Err }

type Scheduler struct {
	mu          sync.RWMutex
	accounts    []model.Account
	routes      []model.ModelRoute
	healthStore AccountHealthStore
}

func NewScheduler(accounts []model.Account) *Scheduler {
	return &Scheduler{accounts: accounts}
}

// SetModelRoutes replaces the scheduler's routing table used by Pick.
func (s *Scheduler) SetModelRoutes(routes []model.ModelRoute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.ModelRoute, len(routes))
	copy(out, routes)
	s.routes = out
}

func (s *Scheduler) ModelRoutes() []model.ModelRoute {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.ModelRoute, len(s.routes))
	copy(out, s.routes)
	return out
}

func (s *Scheduler) Accounts() []model.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Account, len(s.accounts))
	copy(out, s.accounts)
	return out
}

// ReplaceAccounts swaps the in-memory account list (e.g. after store reload).
func (s *Scheduler) ReplaceAccounts(accounts []model.Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Account, len(accounts))
	copy(out, accounts)
	s.accounts = out
}

// SetAccountHealth updates an account's Healthy flag in the scheduler's
// in-memory copy. The store holds the authoritative state; this mirrors it
// so the scheduler's Pick() sees the change without a restart.
func (s *Scheduler) SetAccountHealth(id string, healthy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			s.accounts[i].Healthy = healthy
			if healthy {
				s.accounts[i].CooldownUntil = ""
			}
			return
		}
	}
}

// Pick returns the account for the given attempt index. Attempts walk healthy
// accounts tier by tier, exhausting each tier horizontally before descending -
// attempt 0 and 1 are peers in tier 1 before attempt 2 reaches tier 2.
//
// When modelName matches enabled model_routes, only eligible providers (plus
// mock) are considered so the walk never burns attempts on the wrong upstream.
func (s *Scheduler) Pick(attempt int, modelName string) (model.Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now().UTC()
	tiers := map[int][]model.Account{}
	for _, a := range s.accounts {
		if !accountSchedulable(a, now) {
			continue
		}
		if !AccountEligibleForModel(s.routes, a.Provider, modelName) {
			continue
		}
		tiers[a.Priority] = append(tiers[a.Priority], a)
	}
	// If routing filtered everyone out (e.g. only mock paused and no matching
	// provider), fall back to unfiltered schedulable accounts so the pool is not
	// silently empty.
	if len(tiers) == 0 {
		for _, a := range s.accounts {
			if accountSchedulable(a, now) {
				tiers[a.Priority] = append(tiers[a.Priority], a)
			}
		}
	}
	priorities := make([]int, 0, len(tiers))
	for p := range tiers {
		priorities = append(priorities, p)
	}
	sort.Ints(priorities)

	seen := 0
	for _, p := range priorities {
		for _, a := range tiers[p] {
			if seen == attempt {
				return a, true
			}
			seen++
		}
	}
	return model.Account{}, false
}

// CallUpstream dispatches to the adapter named by the account's Provider.
// It fails closed on an unknown provider rather than falling back to the demo
// upstream, so a misconfigured account is loud instead of silently fake.
func CallUpstream(a model.Account, req ChatRequest) (Reply, error) {
	p, err := ProviderFor(a.Provider)
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}
	return p.Call(a, req)
}

// Result reports which account served a request and how many attempts it took.
type Result struct {
	Account model.Account
	Text    string
	// Tokens is the provider's own usage count, or 0 when it did not report
	// one. Callers must not read 0 as "free".
	Tokens   int64
	Attempts int
}

// Relay runs the failover walk. It returns on the first upstream success.
// A pre-first-byte failure advances to the next account; a post-first-byte
// failure aborts immediately and is never replayed.
func (s *Scheduler) Relay(req ChatRequest) (Result, error) {
	var lastErr error

	for attempt := 0; attempt < MaxAttempts; attempt++ {
		acct, ok := s.Pick(attempt, req.Model)
		if !ok {
			continue
		}

		reply, err := CallUpstream(acct, req)
		status := "200"
		if err != nil {
			status = "failed"
		}
		if err != nil {
			log.Printf("attempt=%d account=%s provider=%s -> %s err=%v", attempt, acct.ID, acct.Provider, status, err)
		} else {
			log.Printf("attempt=%d account=%s provider=%s -> %s", attempt, acct.ID, acct.Provider, status)
		}

		s.NoteUpstreamResult(acct.ID, err)

		if err == nil {
			return Result{
				Account:  acct,
				Text:     reply.Content,
				Tokens:   reply.Tokens,
				Attempts: attempt + 1,
			}, nil
		}
		lastErr = err

		var relayErr RelayError
		if errors.As(err, &relayErr) && relayErr.FirstByteSent {
			return Result{Account: acct, Attempts: attempt + 1}, err
		}
	}

	if lastErr == nil {
		lastErr = errors.New("no healthy account")
	}
	return Result{}, lastErr
}

// WriteSSE streams a response body as OpenAI-style server-sent events.
func WriteSSE(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	for _, chunk := range []string{"Subport ", "demo ", text} {
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", chunk)
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(15 * time.Millisecond)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}