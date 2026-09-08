package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	mu       sync.RWMutex
	accounts []Account
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
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/"):
		s.list(strings.TrimPrefix(r.URL.Path, "/api/"), w)
	default:
		http.NotFound(w, r)
	}
}
func main() {
	s := &Server{accounts: []Account{{ID: "acct-openai-1", Name: "OpenAI primary", Provider: "openai", BaseURL: "http://127.0.0.1:8080", Priority: 1, Healthy: true}, {ID: "acct-openai-2", Name: "OpenAI sibling", Provider: "openai", BaseURL: "http://127.0.0.1:8080", Priority: 1, Healthy: true}, {ID: "acct-anthropic-1", Name: "Anthropic backup", Provider: "anthropic", BaseURL: "http://127.0.0.1:8080", Priority: 2, Healthy: true}}}
	log.Println("subport backend listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", s))
}
