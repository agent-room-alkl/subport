package httpapi

// Quota admission policy.
//
// The store decides whether a reservation FITS; this file decides HOW MUCH to
// reserve. Keeping the two apart matters: the sizing heuristic will change as
// we learn what real traffic costs, and none of that should be able to reach
// into the atomicity of admission.
//
// This is deliberately separate from handleChat so it can be tested and
// reviewed on its own, and so wiring it in is a small, obvious edit rather
// than a rewrite of the request path.

import (
	"errors"
	"os"
	"strconv"

	"github.com/agent-room-alkl/subport/internal/gateway"
	"github.com/agent-room-alkl/subport/internal/model"
	"github.com/agent-room-alkl/subport/internal/store"
)

// defaultReservation is what a request holds when nothing better is known.
// It is a guess, and it is meant to be: settlement charges the real cost and
// releases the whole hold, so the only thing an estimate controls is how many
// requests one user can have in flight before being told to wait.
//
// Too low and admission stops protecting the quota; too high and a user with
// plenty of quota gets refused while nothing is actually being spent. 4000 is
// roughly one ordinary completion.
const defaultReservation int64 = 4000

// reservationFor sizes the hold for one request. Longer prompts cost more, so
// the estimate scales with what was sent - the only part of the cost we can
// see before the call happens.
func reservationFor(req gateway.ChatRequest) int64 {
	var chars int64
	for _, m := range req.Messages {
		chars += int64(len(m.Content))
	}
	// Roughly 4 characters per token, doubled to cover the response, which is
	// the part we cannot see at all.
	est := (chars / 4) * 2
	if est < defaultReservation {
		return defaultReservation
	}
	return est
}

// maxReservation caps the estimate so one enormous prompt cannot hold a user's
// entire allowance hostage for the duration of a single call.
func maxReservation() int64 {
	if v := os.Getenv("SUBPORT_MAX_RESERVATION"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 200_000
}

// admit reserves quota for a request and returns the amount held, which the
// caller MUST later pass to settle or release. A returned error is already the
// right thing to tell the client: ErrQuotaExhausted means 402.
func admit(s *store.Store, owner model.User, req gateway.ChatRequest) (int64, error) {
	amount := reservationFor(req)
	if limit := maxReservation(); amount > limit {
		amount = limit
	}
	if err := s.ReserveQuota(owner.ID, amount); err != nil {
		return 0, err
	}
	return amount, nil
}

// isQuotaExhausted keeps the 402 decision in one place, so a caller cannot
// accidentally turn a storage failure into "you are out of quota".
func isQuotaExhausted(err error) bool {
	return errors.Is(err, store.ErrQuotaExhausted)
}
