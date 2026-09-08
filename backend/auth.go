package main

// Authentication and the user-scoped console API.
//
// The rule this file exists to enforce: a console handler NEVER takes a user id
// from the request. It takes it from the authenticated session. That is why
// there is no /api/console/users/{id}/keys style route anywhere - the shape of
// the API makes the "read someone else's data" request unrepresentable rather
// than relying on a check that someone might forget.

import (
	"encoding/json"
	"net/http"
	"strings"
)

type ctxUser struct {
	User
	ok bool
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

func (s *Server) current(r *http.Request) ctxUser {
	tok := bearer(r)
	if tok == "" {
		return ctxUser{}
	}
	u, err := s.store.SessionUser(tok)
	if err != nil {
		return ctxUser{}
	}
	return ctxUser{User: u, ok: true}
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ---------------------------------------------------------------- auth routes

// registerInput accepts the invite code under both spellings. Every response
// this API emits is snake_case, so a client will reasonably send invite_code;
// encoding/json matches field names case-insensitively but an underscore is
// not ignored, so "invite_code" would silently arrive empty and the request
// would fail as "invalid invite code" with no hint why. Accept both.
type registerInput struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	InviteCodeSnake string `json:"invite_code"`
	InviteCodeCamel string `json:"inviteCode"`
}

func (in registerInput) invite() string {
	if in.InviteCodeSnake != "" {
		return in.InviteCodeSnake
	}
	return in.InviteCodeCamel
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var in registerInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, 400, "bad request")
		return
	}
	if len(in.Username) < 3 || len(in.Password) < 8 {
		fail(w, 400, "username must be 3+ chars and password 8+ chars")
		return
	}
	// Invite-gated by default so a fresh deployment cannot be mass-registered.
	// Set SUBPORT_INVITE_CODE="" to open registration.
	if s.inviteCode != "" && in.invite() != s.inviteCode {
		fail(w, 403, "invalid invite code")
		return
	}
	u, err := s.store.CreateUser(in.Username, in.Password, "user")
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	sess, err := s.store.NewSession(u.ID)
	if err != nil {
		fail(w, 500, "could not start session")
		return
	}
	jsonOut(w, map[string]any{"token": sess.Token, "user": publicUser(u)})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, 400, "bad request")
		return
	}
	u, err := s.store.Authenticate(in.Username, in.Password)
	if err != nil {
		// Same message for unknown user and wrong password - do not reveal
		// which usernames exist.
		fail(w, 401, "invalid username or password")
		return
	}
	sess, err := s.store.NewSession(u.ID)
	if err != nil {
		fail(w, 500, "could not start session")
		return
	}
	jsonOut(w, map[string]any{"token": sess.Token, "user": publicUser(u)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if tok := bearer(r); tok != "" {
		_ = s.store.DropSession(tok)
	}
	w.WriteHeader(204)
}

// publicUser strips the password hash and salt. Never serialise User directly.
func publicUser(u User) map[string]any {
	return map[string]any{
		"id": u.ID, "username": u.Username, "role": u.Role,
		"quota_total": u.QuotaTotal, "quota_used": u.QuotaUsed,
		"created_at": u.CreatedAt,
	}
}

// ---------------------------------------------------------------- console API

func (s *Server) consoleRoutes(w http.ResponseWriter, r *http.Request, rest string) {
	me := s.current(r)
	if !me.ok {
		fail(w, 401, "not signed in")
		return
	}

	switch {
	case rest == "me" && r.Method == "GET":
		jsonOut(w, publicUser(me.User))

	case rest == "keys" && r.Method == "GET":
		out := []map[string]any{}
		for _, k := range s.store.KeysOf(me.ID) {
			out = append(out, publicKey(k))
		}
		jsonOut(w, out)

	case rest == "keys" && r.Method == "POST":
		var in struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Name == "" {
			in.Name = "未命名密钥"
		}
		k, secret, err := s.store.CreateKey(me.ID, in.Name)
		if err != nil {
			fail(w, 500, "could not create key")
			return
		}
		// The plaintext secret is returned exactly once, here.
		jsonOut(w, map[string]any{"key": publicKey(k), "secret": secret})

	case strings.HasPrefix(rest, "keys/") && r.Method == "DELETE":
		id := strings.TrimPrefix(rest, "keys/")
		if err := s.store.DeleteKey(me.ID, id); err != nil {
			fail(w, 404, "key not found")
			return
		}
		w.WriteHeader(204)

	case strings.HasPrefix(rest, "keys/") && r.Method == "PATCH":
		id := strings.TrimPrefix(rest, "keys/")
		var in struct{ Enabled *bool }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Enabled == nil {
			fail(w, 400, "enabled is required")
			return
		}
		if err := s.store.SetKeyEnabled(me.ID, id, *in.Enabled); err != nil {
			fail(w, 404, "key not found")
			return
		}
		w.WriteHeader(204)

	case rest == "usage" && r.Method == "GET":
		jsonOut(w, s.store.UsageOf(me.ID, 100))

	case rest == "quota" && r.Method == "GET":
		jsonOut(w, map[string]any{
			"quota_total": me.QuotaTotal,
			"quota_used":  me.QuotaUsed,
			"remaining":   me.QuotaTotal - me.QuotaUsed,
		})

	default:
		fail(w, 404, "no such console endpoint")
	}
}

func publicKey(k APIKey) map[string]any {
	return map[string]any{
		"id": k.ID, "name": k.Name, "prefix": k.Prefix,
		"enabled": k.Enabled, "created_at": k.CreatedAt, "last_used": k.LastUsed,
	}
}

// ---------------------------------------------------------------- admin gate

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	me := s.current(r)
	if !me.ok {
		fail(w, 401, "not signed in")
		return false
	}
	if me.Role != "admin" {
		fail(w, 403, "admin only")
		return false
	}
	return true
}
