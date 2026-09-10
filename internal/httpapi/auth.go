package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/agent-room-alkl/subport/internal/model"
)

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}

type currentUser struct {
	model.User
	ok bool
}

func (s *Server) current(r *http.Request) currentUser {
	tok := bearer(r)
	if tok == "" {
		return currentUser{}
	}
	u, err := s.Store.SessionUser(tok)
	if err != nil {
		return currentUser{}
	}
	return currentUser{User: u, ok: true}
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	me := s.current(r)
	if !me.ok {
		fail(w, http.StatusUnauthorized, "not signed in")
		return false
	}
	if me.Role != model.RoleAdmin {
		fail(w, http.StatusForbidden, "admin only")
		return false
	}
	return true
}

// ---------------------------------------------------------------- register

// registerInput accepts the invite code under both spellings. Every response
// this API emits is snake_case, so a client will reasonably send invite_code;
// encoding/json matches field names case-insensitively but does not ignore
// underscores, so "invite_code" would silently arrive empty and the request
// would fail as "invalid invite code" - an error pointing at the wrong thing.
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
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if len(strings.TrimSpace(in.Username)) < 3 || len(in.Password) < 8 {
		fail(w, http.StatusBadRequest, "username must be 3+ chars and password 8+ chars")
		return
	}
	// Invite-gated by default so a fresh deployment cannot be mass-registered.
	// Set SUBPORT_INVITE_CODE="" to open registration.
	if s.InviteCode != "" && in.invite() != s.InviteCode {
		fail(w, http.StatusForbidden, "invalid invite code")
		return
	}
	u, err := s.Store.CreateUser(in.Username, in.Password, model.RoleUser)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	sess, err := s.Store.NewSession(u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not start session")
		return
	}
	jsonOut(w, map[string]any{"token": sess.Token, "user": model.PublicUser(u)})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	u, err := s.Store.Authenticate(in.Username, in.Password)
	if err != nil {
		// One message for both unknown user and wrong password, so the
		// endpoint cannot be used to enumerate usernames.
		fail(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	sess, err := s.Store.NewSession(u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not start session")
		return
	}
	jsonOut(w, map[string]any{"token": sess.Token, "user": model.PublicUser(u)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if tok := bearer(r); tok != "" {
		_ = s.Store.DropSession(tok)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- console
//
// Every branch below passes me.ID - the id from the session - into the store.
// No branch reads an id from the URL or body. That is the whole boundary.

func (s *Server) consoleRoutes(w http.ResponseWriter, r *http.Request, rest string) {
	me := s.current(r)
	if !me.ok {
		fail(w, http.StatusUnauthorized, "not signed in")
		return
	}

	if s.consoleRechargeRoutes(w, r, rest, me) {
		return
	}

	switch {
	case rest == "me" && r.Method == http.MethodGet:
		jsonOut(w, model.PublicUser(me.User))

	case rest == "quota" && r.Method == http.MethodGet:
		jsonOut(w, map[string]any{
			"quota_total": me.QuotaTotal,
			"quota_used":  me.QuotaUsed,
			"remaining":   me.QuotaTotal - me.QuotaUsed,
		})

	case rest == "keys" && r.Method == http.MethodGet:
		out := []map[string]any{}
		for _, k := range s.Store.KeysOf(me.ID) {
			out = append(out, model.PublicKey(k))
		}
		jsonOut(w, out)

	case rest == "keys" && r.Method == http.MethodPost:
		var in struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if strings.TrimSpace(in.Name) == "" {
			in.Name = "未命名密钥"
		}
		k, secret, err := s.Store.CreateKey(me.ID, in.Name)
		if err != nil {
			fail(w, http.StatusInternalServerError, "could not create key")
			return
		}
		// The plaintext secret is returned exactly once, here.
		jsonOut(w, map[string]any{"key": model.PublicKey(k), "secret": secret})

	case strings.HasPrefix(rest, "keys/") && r.Method == http.MethodDelete:
		// Scoped by me.ID: another user's key reads as not-found, so an id
		// probe cannot even learn whether it exists.
		if err := s.Store.DeleteKey(me.ID, strings.TrimPrefix(rest, "keys/")); err != nil {
			fail(w, http.StatusNotFound, "key not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case strings.HasPrefix(rest, "keys/") && r.Method == http.MethodPatch:
		var in struct {
			Enabled *bool `json:"enabled"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Enabled == nil {
			fail(w, http.StatusBadRequest, "enabled is required")
			return
		}
		if err := s.Store.SetKeyEnabled(me.ID, strings.TrimPrefix(rest, "keys/"), *in.Enabled); err != nil {
			fail(w, http.StatusNotFound, "key not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case rest == "usage" && r.Method == http.MethodGet:
		jsonOut(w, s.Store.UsageOf(me.ID, 100))

	default:
		fail(w, http.StatusNotFound, "no such console endpoint")
	}
}
