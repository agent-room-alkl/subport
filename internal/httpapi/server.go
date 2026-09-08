// Package httpapi wires the routes and enforces the access boundary.
//
// The boundary in one sentence: a /api/console/* handler NEVER reads a user id
// from the request, only from the authenticated session. "Read someone else's
// data" is therefore unrepresentable in the API shape, rather than blocked by
// a check that a future handler could forget to add.
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/agent-room-alkl/subport/internal/gateway"
	"github.com/agent-room-alkl/subport/internal/model"
	"github.com/agent-room-alkl/subport/internal/store"
)

type Server struct {
	Store      *store.Store
	Sched      *gateway.Scheduler
	InviteCode string
	WebDir     string
}

func New(st *store.Store, sched *gateway.Scheduler, inviteCode, webDir string) *Server {
	return &Server{Store: st, Sched: sched, InviteCode: inviteCode, WebDir: webDir}
}

func jsonOut(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	p := r.URL.Path
	switch {
	case p == "/healthz":
		jsonOut(w, map[string]string{"status": "ok"})

	// Stand-in for a provider so the failover path can be exercised end to end.
	case p == "/mock/upstream" && r.Method == http.MethodPost:
		if r.URL.Query().Get("account") == "acct-openai-1" {
			http.Error(w, "simulated upstream failure", http.StatusInternalServerError)
			return
		}
		jsonOut(w, map[string]string{
			"content": "Subport response via openai account " + r.URL.Query().Get("account") + ".",
		})

	case p == "/v1/chat/completions" && r.Method == http.MethodPost:
		s.handleChat(w, r)

	case p == "/api/auth/register" && r.Method == http.MethodPost:
		s.handleRegister(w, r)
	case p == "/api/auth/login" && r.Method == http.MethodPost:
		s.handleLogin(w, r)
	case p == "/api/auth/logout" && r.Method == http.MethodPost:
		s.handleLogout(w, r)

	// The signed-in user's own data, scoped by session.
	case strings.HasPrefix(p, "/api/console/"):
		s.consoleRoutes(w, r, strings.TrimPrefix(p, "/api/console/"))

	// The operator's view of the whole system, gated on role.
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/api/"):
		if !s.requireAdmin(w, r) {
			return
		}
		s.adminList(w, strings.TrimPrefix(p, "/api/"))

	default:
		if s.WebDir != "" {
			http.FileServer(http.Dir(s.WebDir)).ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	// The gateway is authenticated by API key, not by session. Without this,
	// anyone reachable could spend upstream capacity, usage could not be
	// attributed to anyone, and the quota on every user would be unenforceable.
	key, owner, keyErr := s.Store.KeyBySecret(bearer(r))
	if keyErr != nil {
		// One message whether the key is absent, unknown, or disabled - a
		// caller should not be able to probe which keys exist.
		fail(w, http.StatusUnauthorized, "invalid or missing API key")
		return
	}
	if owner.QuotaTotal > 0 && owner.QuotaUsed >= owner.QuotaTotal {
		fail(w, http.StatusPaymentRequired, "quota exhausted")
		return
	}

	var req gateway.ChatRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Model == "" {
		req.Model = "gpt-4o-mini"
	}

	res, relayErr := s.Sched.Relay(req)
	s.Store.TouchKey(key.ID, time.Now().UTC().Format("2006-01-02 15:04"))

	if relayErr != nil {
		var re gateway.RelayError
		broken := asRelay(relayErr, &re) && re.FirstByteSent

		// Recorded either way so the user can see what happened, but a failure
		// is not billed. A stream cut after output began IS billed for what was
		// produced, and is marked so it can be appealed - see docs/MERGE_REPORT.md.
		status := "failed"
		if broken {
			status = "stream_broken"
		}
		_ = s.Store.AddUsage(model.UsageLog{
			UserID: owner.ID, KeyID: key.ID, Model: req.Model,
			AccountID: res.Account.ID, Status: status, StreamBroken: broken,
			Attempts: res.Attempts,
		})

		if broken {
			// Output already began; report the cut rather than replaying it.
			http.Error(w, relayErr.Error(), http.StatusBadGateway)
			return
		}
		http.Error(w, "no healthy account", http.StatusServiceUnavailable)
		return
	}

	// Billed against the owner resolved from the key, never from the request.
	tokens := int64(len(res.Text))
	_ = s.Store.AddUsage(model.UsageLog{
		UserID: owner.ID, KeyID: key.ID, Model: req.Model,
		AccountID: res.Account.ID, Tokens: tokens, Cost: tokens,
		Status: "success", Attempts: res.Attempts,
	})

	if req.Stream {
		gateway.WriteSSE(w, res.Text)
		return
	}
	jsonOut(w, map[string]any{
		"id": "chatcmpl-demo", "object": "chat.completion", "model": req.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]string{"role": "assistant", "content": res.Text},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"total_tokens": tokens},
	})
}

func asRelay(err error, target *gateway.RelayError) bool {
	re, ok := err.(gateway.RelayError)
	if ok {
		*target = re
	}
	return ok
}

func (s *Server) adminList(w http.ResponseWriter, kind string) {
	switch kind {
	case "accounts":
		out := []map[string]any{}
		for _, a := range s.Sched.Accounts() {
			out = append(out, model.PublicAccount(a))
		}
		jsonOut(w, out)
	case "channels":
		jsonOut(w, []map[string]any{
			{"id": "openai-main", "provider": "openai", "priority": 1, "status": "healthy"},
			{"id": "anthropic-backup", "provider": "anthropic", "priority": 2, "status": "healthy"},
		})
	default:
		fail(w, http.StatusNotFound, "no such admin endpoint")
	}
}
