package main

// File-backed store. Deliberately stdlib-only: the v1 goal is that `go run .`
// works with no database to install and no modules to download. The Store
// interface is the seam - swap this for Postgres without touching handlers.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	Salt         string    `json:"salt"`
	Role         string    `json:"role"` // "admin" | "user"
	QuotaTotal   int64     `json:"quota_total"`
	QuotaUsed    int64     `json:"quota_used"`
	CreatedAt    time.Time `json:"created_at"`
}

type APIKey struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	SecretSHA string    `json:"secret_sha"` // only the hash is stored
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  string    `json:"last_used"`
}

// UsageLog is one billable attempt. StreamBroken and Compensated exist from
// day one so the stream-break billing policy can be decided later without a
// migration - see the report's section on pre-consume/settle/refund.
type UsageLog struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	KeyID        string    `json:"key_id"`
	Model        string    `json:"model"`
	AccountID    string    `json:"account_id"`
	Tokens       int64     `json:"tokens"`
	Cost         int64     `json:"cost"`
	Status       string    `json:"status"` // success | failed | stream_broken
	StreamBroken bool      `json:"stream_broken"`
	Compensated  bool      `json:"compensated"`
	Attempts     int       `json:"attempts"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	Token     string    `json:"token"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type data struct {
	Users    []User     `json:"users"`
	Keys     []APIKey   `json:"keys"`
	Usage    []UsageLog `json:"usage"`
	Accounts []Account  `json:"accounts"`
	Sessions []Session  `json:"sessions"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	d    data
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &s.d); err != nil {
			return nil, err
		}
		return s, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	s.d = data{Accounts: seedAccounts()}
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

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func newSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "sk-sp-" + hex.EncodeToString(b)
}

func hashWithSalt(secret, salt string) string {
	sum := sha256.Sum256([]byte(salt + secret))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- users

// normUsername folds case and trims space. Usernames are compared on this
// form so that "Admin" cannot be registered alongside "admin" - otherwise a
// user could pick a name that reads as the operator's in any listing.
func normUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (s *Store) CreateUser(username, password, role string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	username = normUsername(username)
	if username == "" {
		return User{}, errors.New("username is required")
	}
	for _, u := range s.d.Users {
		if normUsername(u.Username) == username {
			return User{}, errors.New("username already taken")
		}
	}
	saltBytes := make([]byte, 8)
	_, _ = rand.Read(saltBytes)
	salt := hex.EncodeToString(saltBytes)
	u := User{
		ID:           newID("usr"),
		Username:     username,
		PasswordHash: hashWithSalt(password, salt),
		Salt:         salt,
		Role:         role,
		QuotaTotal:   1_000_000,
		CreatedAt:    time.Now().UTC(),
	}
	s.d.Users = append(s.d.Users, u)
	return u, s.flush()
}

func (s *Store) Authenticate(username, password string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	username = normUsername(username)
	for _, u := range s.d.Users {
		if normUsername(u.Username) == username && u.PasswordHash == hashWithSalt(password, u.Salt) {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

func (s *Store) UserByID(id string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.d.Users {
		if u.ID == id {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

// ---------------------------------------------------------------- sessions

func (s *Store) NewSession(userID string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	sess := Session{
		Token:     hex.EncodeToString(b),
		UserID:    userID,
		ExpiresAt: time.Now().UTC().Add(7 * 24 * time.Hour),
	}
	s.d.Sessions = append(s.d.Sessions, sess)
	return sess, s.flush()
}

func (s *Store) SessionUser(token string) (User, error) {
	s.mu.RLock()
	var userID string
	for _, sess := range s.d.Sessions {
		if sess.Token == token {
			if time.Now().UTC().After(sess.ExpiresAt) {
				s.mu.RUnlock()
				return User{}, ErrNotFound
			}
			userID = sess.UserID
			break
		}
	}
	s.mu.RUnlock()
	if userID == "" {
		return User{}, ErrNotFound
	}
	return s.UserByID(userID)
}

func (s *Store) DropSession(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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

func (s *Store) KeysOf(userID string) []APIKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []APIKey{}
	for _, k := range s.d.Keys {
		if k.UserID == userID {
			out = append(out, k)
		}
	}
	return out
}

// CreateKey returns the plaintext secret exactly once; only its hash is stored.
func (s *Store) CreateKey(userID, name string) (APIKey, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret := newSecret()
	k := APIKey{
		ID:        newID("key"),
		UserID:    userID,
		Name:      name,
		Prefix:    secret[:12],
		SecretSHA: hashWithSalt(secret, ""),
		Enabled:   true,
		CreatedAt: time.Now().UTC(),
		LastUsed:  "never",
	}
	s.d.Keys = append(s.d.Keys, k)
	return k, secret, s.flush()
}

// KeyByID is scoped: a key belonging to another user reads as not-found, so
// guessing an id leaks nothing about whether it exists.
func (s *Store) KeyByID(userID, keyID string) (APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.d.Keys {
		if k.ID == keyID && k.UserID == userID {
			return k, nil
		}
	}
	return APIKey{}, ErrNotFound
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

func (s *Store) UsageOf(userID string, limit int) []UsageLog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []UsageLog{}
	for i := len(s.d.Usage) - 1; i >= 0 && len(out) < limit; i-- {
		if s.d.Usage[i].UserID == userID {
			out = append(out, s.d.Usage[i])
		}
	}
	return out
}

func (s *Store) AddUsage(l UsageLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l.ID = newID("use")
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

func (s *Store) Accounts() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Account, len(s.d.Accounts))
	copy(out, s.d.Accounts)
	return out
}

func (s *Store) SetAccountHealthy(id string, healthy bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Accounts {
		if s.d.Accounts[i].ID == id {
			s.d.Accounts[i].Healthy = healthy
			return s.flush()
		}
	}
	return ErrNotFound
}
