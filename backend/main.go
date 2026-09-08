package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type Account struct {
	ID, Name, Provider  string
	BaseURL             string
	Priority            int
	Healthy             bool
	Load                float64
	CooldownUntil       string
	LastError           string
	ConsecutiveTimeouts int
	Consecutive403      int
}
type ChatRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}
type Server struct {
	mu         sync.RWMutex
	accounts   []Account
	store      *Store
	inviteCode string
}

// seedAccounts is the starting pool for a fresh store. Two accounts share
// tier 1 on purpose: that is what makes horizontal failover observable.
func seedAccounts() []Account {
	const base = "http://127.0.0.1:8080"
	return []Account{
		{ID: "acct-openai-1", Name: "OpenAI primary", Provider: "openai", BaseURL: base, Priority: 1, Healthy: true},
		{ID: "acct-openai-2", Name: "OpenAI sibling", Provider: "openai", BaseURL: base, Priority: 1, Healthy: true},
		{ID: "acct-anthropic-1", Name: "Anthropic backup", Provider: "anthropic", BaseURL: base, Priority: 2, Healthy: true},
	}
}

type RelayError struct {
	Err           error
	FirstByteSent bool
}

func (e RelayError) Error() string { return e.Err.Error() }
func (e RelayError) Unwrap() error { return e.Err }

func (s *Server) pick(attempt int) (Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tiers := map[int][]Account{}
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
	return Account{}, false
}

func jsonOut(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Server) list(kind string, w http.ResponseWriter) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch kind {
	case "accounts":
		out := make([]map[string]any, 0, len(s.accounts))
		for _, a := range s.accounts {
			state := "healthy"
			if !a.Healthy {
				state = "paused"
			}
			out = append(out, map[string]any{"id": a.ID, "label": a.Name, "provider": a.Provider, "tier": a.Priority, "state": state, "load": a.Load, "cooldown_until": a.CooldownUntil, "last_error": a.LastError, "consecutive_timeouts": a.ConsecutiveTimeouts, "consecutive_403": a.Consecutive403})
		}
		jsonOut(w, out)
	case "channels":
		jsonOut(w, []map[string]any{{"id": "openai-main", "provider": "openai", "priority": 1, "status": "healthy"}, {"id": "anthropic-backup", "provider": "anthropic", "priority": 2, "status": "healthy"}})
	case "keys":
		jsonOut(w, []map[string]any{{"id": "demo-key", "name": "Local development key", "lastUsed": "never"}})
	}
}
func callUpstream(a Account, req ChatRequest) (string, error) {
	body, _ := json.Marshal(req)
	resp, err := http.Post(a.BaseURL+"/mock/upstream?account="+a.ID, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return "", RelayError{Err: err, FirstByteSent: false}
	}
	defer resp.Body.Close()
	var out struct {
		Content string `json:"content"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 {
		return "", RelayError{Err: fmt.Errorf("upstream status %d", resp.StatusCode), FirstByteSent: false}
	}
	return out.Content, nil
}
func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Model == "" {
		req.Model = "gpt-4o-mini"
	}
	var chosen Account
	var text string
	var err error
	succeeded := false
	for attempt := 0; attempt < 3; attempt++ {
		var ok bool
		chosen, ok = s.pick(attempt)
		if !ok {
			continue
		}
		text, err = callUpstream(chosen, req)
		log.Printf("attempt=%d account=%s -> %s", attempt, chosen.ID, map[bool]string{true: "200", false: "500"}[err == nil])
		if err == nil {
			succeeded = true
			break
		}
		var relayErr RelayError
		if errors.As(err, &relayErr) && relayErr.FirstByteSent {
			http.Error(w, err.Error(), 502)
			return
		}
	}
	if !succeeded {
		http.Error(w, "no healthy account", 503)
		return
	}
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		f, _ := w.(http.Flusher)
		for _, chunk := range []string{"Subport ", "demo ", "response via " + chosen.Provider + "."} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", chunk)
			f.Flush()
			time.Sleep(15 * time.Millisecond)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	jsonOut(w, map[string]any{"id": "chatcmpl-demo", "object": "chat.completion", "model": req.Model, "choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": text}, "finish_reason": "stop"}}})
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	switch {
	case r.URL.Path == "/mock/upstream" && r.Method == "POST":
		if r.URL.Query().Get("account") == "acct-openai-1" {
			http.Error(w, "simulated upstream failure", 500)
			return
		}
		jsonOut(w, map[string]string{"content": "Subport response via openai account " + r.URL.Query().Get("account") + "."})
	case r.URL.Path == "/healthz":
		jsonOut(w, map[string]string{"status": "ok"})
	case r.URL.Path == "/v1/chat/completions" && r.Method == "POST":
		s.chat(w, r)

	case r.URL.Path == "/api/auth/register" && r.Method == "POST":
		s.handleRegister(w, r)
	case r.URL.Path == "/api/auth/login" && r.Method == "POST":
		s.handleLogin(w, r)
	case r.URL.Path == "/api/auth/logout" && r.Method == "POST":
		s.handleLogout(w, r)

	// Console = the signed-in user's own data. Scoped by session, never by
	// an id in the URL.
	case strings.HasPrefix(r.URL.Path, "/api/console/"):
		s.consoleRoutes(w, r, strings.TrimPrefix(r.URL.Path, "/api/console/"))

	// Admin = the operator's view of the whole system. Gated on role.
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/"):
		if !s.requireAdmin(w, r) {
			return
		}
		s.list(strings.TrimPrefix(r.URL.Path, "/api/"), w)

	default:
		http.NotFound(w, r)
	}
}

func main() {
	dbPath := os.Getenv("SUBPORT_DB")
	if dbPath == "" {
		dbPath = "subport-data.json"
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		log.Fatalf("cannot open store %s: %v", dbPath, err)
	}

	invite, set := os.LookupEnv("SUBPORT_INVITE_CODE")
	if !set {
		invite = "subport-invite" // invite-gated by default; set to "" to open
	}

	s := &Server{accounts: store.Accounts(), store: store, inviteCode: invite}

	// Bootstrap an admin on first run so the operator can actually sign in.
	if _, err := store.Authenticate("admin", adminBootstrapPassword()); err != nil {
		if u, cerr := store.CreateUser("admin", adminBootstrapPassword(), "admin"); cerr == nil {
			log.Printf("created bootstrap admin %q - change this password", u.Username)
		}
	}

	addr := os.Getenv("SUBPORT_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("subport backend listening on %s (store=%s)", addr, dbPath)
	log.Fatal(http.ListenAndServe(addr, s))
}

func adminBootstrapPassword() string {
	if p := os.Getenv("SUBPORT_ADMIN_PASSWORD"); p != "" {
		return p
	}
	return "subport-admin"
}
