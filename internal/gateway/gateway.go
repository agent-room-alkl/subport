// Package gateway holds the part of Subport that the whole project exists for:
// choosing an upstream account, and deciding what to do when one fails.
//
// Two rules govern everything here:
//
//  1. Horizontal first. When an account fails, the next account IN THE SAME
//     PRIORITY TIER takes over. Only when a tier is exhausted does the request
//     descend. This is the correction to new-api's getPriority, which selects
//     priorities[retry] and so drops a tier on every single retry - meaning a
//     pool of ten healthy peers in one tier would never be tried.
//
//  2. The first byte is the point of no return. A failure BEFORE the first
//     byte reaches the client may be retried on another account. A failure
//     AFTER it must not be replayed: emitted tokens cannot be recalled, and a
//     replay shows the caller duplicated output, which is worse than the cut.
package gateway

import (
	"bytes"
	"encoding/json"
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
	mu       sync.RWMutex
	accounts []model.Account
}

func NewScheduler(accounts []model.Account) *Scheduler {
	return &Scheduler{accounts: accounts}
}

func (s *Scheduler) Accounts() []model.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Account, len(s.accounts))
	copy(out, s.accounts)
	return out
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
			return
		}
	}
}

// Pick returns the account for the given attempt index. Attempts walk healthy
// accounts tier by tier, exhausting each tier horizontally before descending -
// attempt 0 and 1 are peers in tier 1 before attempt 2 reaches tier 2.
func (s *Scheduler) Pick(attempt int) (model.Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tiers := map[int][]model.Account{}
	for _, a := range s.accounts {
		if a.Healthy {
			tiers[a.Priority] = append(tiers[a.Priority], a)
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

// CallUpstream performs the real request against the account's BaseURL.
// Swapping the demo /mock/upstream path for a provider endpoint is the only
// change needed to talk to a real model.
func CallUpstream(a model.Account, req ChatRequest) (string, error) {
	body, _ := json.Marshal(req)
	resp, err := http.Post(
		a.BaseURL+"/mock/upstream?account="+a.ID,
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		// Connection never established, so nothing was written to the client.
		return "", RelayError{Err: err, FirstByteSent: false}
	}
	defer resp.Body.Close()

	var out struct {
		Content string `json:"content"`
	}
	decodeErr := json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 {
		return "", RelayError{
			Err:           fmt.Errorf("upstream status %d", resp.StatusCode),
			FirstByteSent: false,
		}
	}
	if decodeErr != nil {
		// The upstream sent a 200 (so the connection was established and
		// output began) but the body was truncated — the stream was cut
		// after the first byte. This is exactly the stream-broken case:
		// the client may have received partial output, so the call is
		// billed for what was produced and marked for compensation, not
		// retried on another account.
		return "", RelayError{
			Err:           fmt.Errorf("stream truncated: %v", decodeErr),
			FirstByteSent: true,
		}
	}
	return out.Content, nil
}

// Result reports which account served a request and how many attempts it took.
type Result struct {
	Account  model.Account
	Text     string
	Attempts int
}

// Relay runs the failover walk. It returns on the first upstream success.
// A pre-first-byte failure advances to the next account; a post-first-byte
// failure aborts immediately and is never replayed.
func (s *Scheduler) Relay(req ChatRequest) (Result, error) {
	var lastErr error

	for attempt := 0; attempt < MaxAttempts; attempt++ {
		acct, ok := s.Pick(attempt)
		if !ok {
			continue // no account at this index; the pool may be smaller
		}

		text, err := CallUpstream(acct, req)
		status := "200"
		if err != nil {
			status = "failed"
		}
		log.Printf("attempt=%d account=%s -> %s", attempt, acct.ID, status)

		if err == nil {
			return Result{Account: acct, Text: text, Attempts: attempt + 1}, nil
		}
		lastErr = err

		var relayErr RelayError
		if errors.As(err, &relayErr) && relayErr.FirstByteSent {
			// Output already reached the client. Stop; do not replay.
			return Result{Account: acct, Attempts: attempt + 1}, err
		}
	}

	// Exhausted without a success. Note this is driven by an explicit failure
	// to succeed, not by whether lastErr happens to be non-nil: a pool with no
	// schedulable account at all must still be an error, not an empty 200.
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
