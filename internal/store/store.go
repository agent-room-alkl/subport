// Package store is file-backed persistence for Subport.
//
// Deliberately stdlib-only: `go run ./cmd/subport` must work with no database
// to install and no modules to download. This package is the seam - T-12
// swaps it for SQLite and nothing in gateway/ or httpapi/ should change.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

var ErrNotFound = errors.New("not found")

// seedAccounts is the starting pool for a fresh store. Two accounts share
// tier 1 on purpose: that is what makes horizontal failover observable.
//
// base must be the address this process is actually reachable on, because the
// demo upstream is served by this same process. Hardcoding a port here breaks
// the gateway silently whenever the server runs anywhere else: every attempt
// fails to connect and the caller gets 503 with nothing pointing at the cause.
func seedAccounts(base string) []model.Account {
	return []model.Account{
		{ID: "acct-openai-1", Name: "OpenAI primary", Provider: "openai", BaseURL: base, Priority: 1, Healthy: true},
		{ID: "acct-openai-2", Name: "OpenAI sibling", Provider: "openai", BaseURL: base, Priority: 1, Healthy: true},
		{ID: "acct-anthropic-1", Name: "Anthropic backup", Provider: "anthropic", BaseURL: base, Priority: 2, Healthy: true},
		// acct-stream-break simulates a mid-stream cut: the mock upstream
		// sends a 200 + partial JSON then closes the connection. This is
		// how stream_broken usage logs are produced end-to-end for the
		// compensation proof. In a real deployment this account would not
		// exist — a real upstream that cuts mid-stream produces the same
		// code path through CallUpstream's truncated-body detection.
		{ID: "acct-stream-break", Name: "Stream break test", Provider: "openai", BaseURL: base, Priority: 3, Healthy: true},
	}
}

type data struct {
	Users    []model.User     `json:"users"`
	Keys     []model.APIKey   `json:"keys"`
	Usage    []model.UsageLog `json:"usage"`
	Accounts []model.Account  `json:"accounts"`
	Sessions []model.Session  `json:"sessions"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	d    data
	db   *sql.DB
}

func OpenStore(path, seedBaseURL string) (*Store, error) {
	// Keep the existing JSON snapshot as the migration source of truth while
	// every store now boots the SQLite schema. CRUD migration follows in T-12B-E.
	s := &Store{path: path}
	db, err := openSQLite(path + ".sqlite")
	if err != nil {
		return nil, err
	}
	s.db = db
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &s.d); err != nil {
			_ = db.Close()
			return nil, err
		}
		if err := s.sqliteEnsureAccounts(s.d.Accounts); err != nil {
			_ = db.Close()
			return nil, err
		}
		return s, nil
	}
	if !os.IsNotExist(err) {
		_ = db.Close()
		return nil, err
	}
	s.d = data{Accounts: seedAccounts(seedBaseURL)}
	if err := s.sqliteEnsureAccounts(s.d.Accounts); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, s.flush()
}

// flush writes atomically: a crash mid-write must not leave a truncated file
// that would lose every user on the next start.
func (s *Store) flush() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flush()
}

// ---------------------------------------------------------------- ids, hashing

func NewID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func NewSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "sk-sp-" + hex.EncodeToString(b)
}

func HashWithSalt(secret, salt string) string {
	sum := sha256.Sum256([]byte(salt + secret))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- users

// normUsername folds case and trims space. Usernames are compared on this
// form so that "Admin" cannot be registered alongside "admin" - otherwise a
// user could pick a name that reads as the operator's in any listing.
func NormUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (s *Store) CreateUser(username, password, role string) (model.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	username = NormUsername(username)
	if username == "" {
		return model.User{}, errors.New("username is required")
	}
	for _, u := range s.d.Users {
		if NormUsername(u.Username) == username {
			return model.User{}, errors.New("username already taken")
		}
	}
	saltBytes := make([]byte, 8)
	_, _ = rand.Read(saltBytes)
	salt := hex.EncodeToString(saltBytes)
	u := model.User{
		ID:           NewID("usr"),
		Username:     username,
		PasswordHash: HashWithSalt(password, salt),
		Salt:         salt,
		Role:         role,
		QuotaTotal:   1_000_000,
		CreatedAt:    time.Now().UTC(),
	}
	if s.db != nil {
		if _, err := s.sqliteUserByName(username); err == nil {
			return model.User{}, errors.New("username already taken")
		}
		if err := s.sqliteCreateUser(u); err != nil {
			return model.User{}, err
		}
		return u, nil
	}
	s.d.Users = append(s.d.Users, u)
	return u, s.flush()
}

func (s *Store) Authenticate(username, password string) (model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	username = NormUsername(username)
	if s.db != nil {
		u, err := s.sqliteUserByName(username)
		if err != nil || u.PasswordHash != HashWithSalt(password, u.Salt) {
			return model.User{}, ErrNotFound
		}
		return u, nil
	}
	for _, u := range s.d.Users {
		if NormUsername(u.Username) == username && u.PasswordHash == HashWithSalt(password, u.Salt) {
			return u, nil
		}
	}
	return model.User{}, ErrNotFound
}

func (s *Store) UserByID(id string) (model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db != nil {
		return s.sqliteUserByID(id)
	}
	for _, u := range s.d.Users {
		if u.ID == id {
			return u, nil
		}
	}
	return model.User{}, ErrNotFound
}

// ---------------------------------------------------------------- sessions

func (s *Store) NewSession(userID string) (model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	sess := model.Session{
		Token:     hex.EncodeToString(b),
		UserID:    userID,
		ExpiresAt: time.Now().UTC().Add(7 * 24 * time.Hour),
	}
	if s.db != nil {
		return sess, s.sqliteNewSession(sess)
	}
	s.d.Sessions = append(s.d.Sessions, sess)
	return sess, s.flush()
}

func (s *Store) SessionUser(token string) (model.User, error) {
	s.mu.RLock()
	if s.db != nil {
		userID, expires, err := s.sqliteSessionUser(token)
		s.mu.RUnlock()
		if err != nil || time.Now().UTC().After(expires) {
			return model.User{}, ErrNotFound
		}
		return s.UserByID(userID)
	}
	var userID string
	for _, sess := range s.d.Sessions {
		if sess.Token == token {
			if time.Now().UTC().After(sess.ExpiresAt) {
				s.mu.RUnlock()
				return model.User{}, ErrNotFound
			}
			userID = sess.UserID
			break
		}
	}
	s.mu.RUnlock()
	if userID == "" {
		return model.User{}, ErrNotFound
	}
	return s.UserByID(userID)
}

func (s *Store) DropSession(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		_, err := s.db.Exec(`DELETE FROM sessions WHERE token=?`, token)
		return err
	}
	out := s.d.Sessions[:0]
	for _, sess := range s.d.Sessions {
		if sess.Token != token {
			out = append(out, sess)
		}
	}
	s.d.Sessions = out
	return s.flush()
}

// ---------------------------------------------------------------- api keys
//
// Every read below filters on userID. That filtering is the security boundary:
// handlers pass the id from the authenticated session and never from the
// request, so a caller cannot ask for someone else's rows.

func (s *Store) KeysOf(userID string) []model.APIKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.APIKey{}
	for _, k := range s.d.Keys {
		if k.UserID == userID {
			out = append(out, k)
		}
	}
	return out
}

// CreateKey returns the plaintext secret exactly once; only its hash is stored.
func (s *Store) CreateKey(userID, name string) (model.APIKey, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret := NewSecret()
	k := model.APIKey{
		ID:        NewID("key"),
		UserID:    userID,
		Name:      name,
		Prefix:    secret[:12],
		SecretSHA: HashWithSalt(secret, ""),
		Enabled:   true,
		CreatedAt: time.Now().UTC(),
		LastUsed:  "never",
	}
	s.d.Keys = append(s.d.Keys, k)
	return k, secret, s.flush()
}

// KeyBySecret resolves a presented API key to its record and owner.
// This is the gateway's authentication path: without it, /v1/* would serve
// anyone, usage could not be attributed, and quota could not be enforced.
// Disabled keys are refused here rather than by the caller, so a revoked key
// stops working everywhere at once.
func (s *Store) KeyBySecret(secret string) (model.APIKey, model.User, error) {
	if secret == "" {
		return model.APIKey{}, model.User{}, ErrNotFound
	}
	sum := HashWithSalt(secret, "")

	s.mu.RLock()
	var found model.APIKey
	ok := false
	for _, k := range s.d.Keys {
		if k.SecretSHA == sum {
			found = k
			ok = true
			break
		}
	}
	s.mu.RUnlock()

	if !ok || !found.Enabled {
		return model.APIKey{}, model.User{}, ErrNotFound
	}
	u, err := s.UserByID(found.UserID)
	if err != nil {
		return model.APIKey{}, model.User{}, ErrNotFound
	}
	return found, u, nil
}

// TouchKey records that a key was just used, for the "last used" column.
func (s *Store) TouchKey(keyID, when string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Keys {
		if s.d.Keys[i].ID == keyID {
			s.d.Keys[i].LastUsed = when
			_ = s.flush()
			return
		}
	}
}

// KeyByID is scoped: a key belonging to another user reads as not-found, so
// guessing an id leaks nothing about whether it exists.
func (s *Store) KeyByID(userID, keyID string) (model.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.d.Keys {
		if k.ID == keyID && k.UserID == userID {
			return k, nil
		}
	}
	return model.APIKey{}, ErrNotFound
}

func (s *Store) SetKeyEnabled(userID, keyID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Keys {
		if s.d.Keys[i].ID == keyID && s.d.Keys[i].UserID == userID {
			s.d.Keys[i].Enabled = enabled
			return s.flush()
		}
	}
	return ErrNotFound
}

func (s *Store) DeleteKey(userID, keyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	out := s.d.Keys[:0]
	for _, k := range s.d.Keys {
		if k.ID == keyID && k.UserID == userID {
			found = true
			continue
		}
		out = append(out, k)
	}
	if !found {
		return ErrNotFound
	}
	s.d.Keys = out
	return s.flush()
}

// ---------------------------------------------------------------- usage

func (s *Store) UsageOf(userID string, limit int) []model.UsageLog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.UsageLog{}
	for i := len(s.d.Usage) - 1; i >= 0 && len(out) < limit; i-- {
		if s.d.Usage[i].UserID == userID {
			out = append(out, s.d.Usage[i])
		}
	}
	return out
}

func (s *Store) AddUsage(l model.UsageLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l.ID = NewID("use")
	l.CreatedAt = time.Now().UTC()
	s.d.Usage = append(s.d.Usage, l)
	for i := range s.d.Users {
		if s.d.Users[i].ID == l.UserID {
			s.d.Users[i].QuotaUsed += l.Cost
		}
	}
	return s.flush()
}

// ---------------------------------------------------------------- accounts

func (s *Store) Accounts() []model.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db != nil {
		if out, err := s.sqliteAccounts(); err == nil {
			return out
		}
	}
	out := make([]model.Account, len(s.d.Accounts))
	copy(out, s.d.Accounts)
	return out
}

func (s *Store) SetAccountHealthy(id string, healthy bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.sqliteSetAccountHealthy(id, healthy)
	}
	for i := range s.d.Accounts {
		if s.d.Accounts[i].ID == id {
			s.d.Accounts[i].Healthy = healthy
			return s.flush()
		}
	}
	return ErrNotFound
}

// ---------------------------------------------------------------- compensation

// CompensateBrokenStreams is the auto-compensation engine. It looks at a
// user's recent calls (the last WindowSize), and if the stream_broken rate
// crosses Threshold with at least MinBroken broken calls, it credits the
// total cost of uncompensated broken calls back to the user's quota and
// marks each one Compensated=true. Running it again on already-compensated
// logs credits nothing — the compensated flag is the idempotency guard.
func (s *Store) CompensateBrokenStreams(userID string, cfg model.CompensationConfig) (model.CompensationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Collect the user's recent calls, most-recent-first.
	var recent []model.UsageLog
	for i := len(s.d.Usage) - 1; i >= 0 && len(recent) < cfg.WindowSize; i-- {
		if s.d.Usage[i].UserID == userID {
			recent = append(recent, s.d.Usage[i])
		}
	}

	result := model.CompensationResult{
		TotalCount:  len(recent),
		BrokenCount: 0,
	}
	for _, l := range recent {
		if l.StreamBroken {
			result.BrokenCount++
		}
	}
	if result.TotalCount > 0 {
		result.Rate = float64(result.BrokenCount) / float64(result.TotalCount)
	}

	// Guard: not enough broken calls to trust the rate.
	if result.BrokenCount < cfg.MinBroken {
		return result, nil
	}
	// Guard: rate below threshold — don't compensate yet.
	if result.Rate < cfg.Threshold {
		return result, nil
	}

	// Gather uncompensated broken logs and their total cost.
	var credit int64
	compensatedIDs := map[string]bool{}
	for _, l := range recent {
		if l.StreamBroken && !l.Compensated {
			credit += l.Cost
			compensatedIDs[l.ID] = true
		}
	}

	// Set compensated=true on each broken log that was credited.
	for i := range s.d.Usage {
		if compensatedIDs[s.d.Usage[i].ID] {
			s.d.Usage[i].Compensated = true
		}
	}

	// Credit the user's quota_used back.
	for i := range s.d.Users {
		if s.d.Users[i].ID == userID {
			s.d.Users[i].QuotaUsed -= credit
			if s.d.Users[i].QuotaUsed < 0 {
				s.d.Users[i].QuotaUsed = 0
			}
		}
	}

	result.Triggered = true
	result.Credited = credit
	result.CompensatedCount = len(compensatedIDs)
	return result, s.flush()
}

// Compensations returns all stream_broken usage logs, for the admin view.
// Pending = not yet compensated; Completed = compensated.
func (s *Store) Compensations() (pending, completed []model.UsageLog) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, l := range s.d.Usage {
		if !l.StreamBroken {
			continue
		}
		if l.Compensated {
			completed = append(completed, l)
		} else {
			pending = append(pending, l)
		}
	}
	return pending, completed
}
