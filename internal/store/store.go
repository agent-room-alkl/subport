// Package store is Subport's persistence layer, backed by SQLite.
//
// There is exactly one storage path. An earlier revision wrote both a JSON
// snapshot and SQLite on every mutation; that left two sources of truth which
// could drift apart silently, which is the failure mode a storage layer exists
// to prevent. A legacy JSON file is now imported ONCE on first boot and never
// written again.
//
// This package is the seam: gateway/ and httpapi/ know only the exported
// method set below and were not edited when the backing store changed.
package store

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

var ErrNotFound = errors.New("not found")

// legacyData is the shape of the pre-SQLite JSON file. It exists only so an
// existing deployment's data can be imported once; nothing writes it.
type legacyData struct {
	Users    []model.User     `json:"users"`
	Keys     []model.APIKey   `json:"keys"`
	Usage    []model.UsageLog `json:"usage"`
	Accounts []model.Account  `json:"accounts"`
	Sessions []model.Session  `json:"sessions"`
}

type Store struct {
	mu sync.Mutex // serialises multi-statement work; SQLite handles the rest
	db *sql.DB
}

// seedAccounts is the starting pool for a fresh store. Two accounts share
// tier 1 on purpose: that is what makes horizontal failover observable.
//
// base must be the address this process is actually reachable on, because the
// demo upstream is served by this same process. Hardcoding a port here breaks
// the gateway silently whenever the server runs anywhere else.
func seedAccounts(base string) []model.Account {
	return []model.Account{
		{ID: "acct-openai-1", Name: "OpenAI primary", Provider: "openai", BaseURL: base, Priority: 1, Healthy: true},
		{ID: "acct-openai-2", Name: "OpenAI sibling", Provider: "openai", BaseURL: base, Priority: 1, Healthy: true},
		{ID: "acct-anthropic-1", Name: "Anthropic backup", Provider: "anthropic", BaseURL: base, Priority: 2, Healthy: true},
		{ID: "acct-stream-break", Name: "Stream-break demo", Provider: "openai", BaseURL: base, Priority: 3, Healthy: false},
	}
}

// OpenStore opens the SQLite database beside path. If path names a legacy JSON
// snapshot and the database is still empty, its contents are imported once.
func OpenStore(path, seedBaseURL string) (*Store, error) {
	db, err := openSQLite(path + ".sqlite")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}

	if err := s.importLegacyJSON(path); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.sqliteEnsureAccounts(seedAccounts(seedBaseURL)); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// importLegacyJSON is one-way and runs at most once: it bails out the moment
// the database already holds users, so the JSON file is never a live mirror.
func (s *Store) importLegacyJSON(path string) error {
	var users int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		return err
	}
	if users > 0 {
		return nil // already migrated; leave the old file untouched
	}

	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh install, nothing to import
		}
		return err
	}
	// Strip a UTF-8 BOM. Any Windows tool that has ever opened and saved the
	// legacy file may have added one, and json.Unmarshal rejects it outright -
	// which would otherwise refuse to start the whole service over a byte
	// order mark, with an error nobody can act on.
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))

	var d legacyData
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	if err := s.sqliteImportUsers(d.Users); err != nil {
		return err
	}
	if err := s.sqliteEnsureKeys(d.Keys); err != nil {
		return err
	}
	if err := s.sqliteEnsureUsage(d.Usage); err != nil {
		return err
	}
	if err := s.sqliteImportSessions(d.Sessions); err != nil {
		return err
	}
	return s.sqliteEnsureAccounts(d.Accounts)
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
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

// NormUsername folds case and trims space. Usernames are compared on this form
// so that "Admin" cannot be registered alongside "admin" - otherwise a user
// could pick a name that reads as the operator's in any listing.
func NormUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ---------------------------------------------------------------- users

func (s *Store) CreateUser(username, password, role string) (model.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	username = NormUsername(username)
	if username == "" {
		return model.User{}, errors.New("username is required")
	}
	taken, err := s.sqliteUsernameTaken(username)
	if err != nil {
		return model.User{}, err
	}
	if taken {
		return model.User{}, errors.New("username already taken")
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
	if err := s.sqliteCreateUser(u); err != nil {
		return model.User{}, err
	}
	return u, nil
}

func (s *Store) Authenticate(username, password string) (model.User, error) {
	u, err := s.sqliteUserByName(NormUsername(username))
	if err != nil {
		return model.User{}, ErrNotFound
	}
	if u.PasswordHash != HashWithSalt(password, u.Salt) {
		return model.User{}, ErrNotFound
	}
	return u, nil
}

func (s *Store) UserByID(id string) (model.User, error) {
	u, err := s.sqliteUserByID(id)
	if err != nil {
		return model.User{}, ErrNotFound
	}
	return u, nil
}

// ---------------------------------------------------------------- sessions

func (s *Store) NewSession(userID string) (model.Session, error) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	sess := model.Session{
		Token:     hex.EncodeToString(b),
		UserID:    userID,
		ExpiresAt: time.Now().UTC().Add(7 * 24 * time.Hour),
	}
	if err := s.sqliteNewSession(sess); err != nil {
		return model.Session{}, err
	}
	return sess, nil
}

func (s *Store) SessionUser(token string) (model.User, error) {
	if token == "" {
		return model.User{}, ErrNotFound
	}
	userID, expires, err := s.sqliteSessionUser(token)
	if err != nil {
		return model.User{}, ErrNotFound
	}
	if time.Now().UTC().After(expires) {
		return model.User{}, ErrNotFound
	}
	return s.UserByID(userID)
}

func (s *Store) DropSession(token string) error {
	return s.sqliteDropSession(token)
}

// ---------------------------------------------------------------- api keys
//
// Every read below filters on userID. That filtering is the security boundary:
// handlers pass the id from the authenticated session and never from the
// request, so a caller cannot ask for someone else's rows.

func (s *Store) KeysOf(userID string) []model.APIKey {
	keys, err := s.sqliteKeysOf(userID)
	if err != nil || keys == nil {
		return []model.APIKey{}
	}
	return keys
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
	if err := s.sqliteInsertKey(k); err != nil {
		return model.APIKey{}, "", err
	}
	return k, secret, nil
}

// KeyBySecret resolves a presented API key to its record and owner. This is the
// gateway's authentication path. Disabled keys are refused here rather than by
// the caller, so a revoked key stops working everywhere at once.
func (s *Store) KeyBySecret(secret string) (model.APIKey, model.User, error) {
	if secret == "" {
		return model.APIKey{}, model.User{}, ErrNotFound
	}
	k, err := s.sqliteKeyBySecretHash(HashWithSalt(secret, ""))
	if err != nil || !k.Enabled {
		return model.APIKey{}, model.User{}, ErrNotFound
	}
	u, err := s.UserByID(k.UserID)
	if err != nil {
		return model.APIKey{}, model.User{}, ErrNotFound
	}
	return k, u, nil
}

// TouchKey records that a key was just used, for the "last used" column.
func (s *Store) TouchKey(keyID, when string) {
	_ = s.sqliteTouchKey(keyID, when)
}

// KeyByID is scoped: a key belonging to another user reads as not-found, so
// guessing an id leaks nothing about whether it exists.
func (s *Store) KeyByID(userID, keyID string) (model.APIKey, error) {
	k, err := s.sqliteKeyByID(userID, keyID)
	if err != nil {
		return model.APIKey{}, ErrNotFound
	}
	return k, nil
}

func (s *Store) SetKeyEnabled(userID, keyID string, enabled bool) error {
	return s.sqliteSetKeyEnabled(userID, keyID, enabled)
}

func (s *Store) DeleteKey(userID, keyID string) error {
	return s.sqliteDeleteKey(userID, keyID)
}

// ---------------------------------------------------------------- usage

func (s *Store) UsageOf(userID string, limit int) []model.UsageLog {
	logs, err := s.sqliteUsageOf(userID, limit)
	if err != nil || logs == nil {
		return []model.UsageLog{}
	}
	return logs
}

func (s *Store) AddUsage(l model.UsageLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	l.ID = NewID("use")
	l.CreatedAt = time.Now().UTC()
	// The insert and the quota increment share one transaction, so a crash
	// cannot bill a user for a call that was never recorded.
	return s.sqliteAddUsage(l)
}

// ---------------------------------------------------------------- accounts

func (s *Store) Accounts() []model.Account {
	accounts, err := s.sqliteAccounts()
	if err != nil || accounts == nil {
		return []model.Account{}
	}
	return accounts
}

func (s *Store) SetAccountHealthy(id string, healthy bool) error {
	return s.sqliteSetAccountHealthy(id, healthy)
}

// ---------------------------------------------------------------- compensation

// CompensateBrokenStreams credits back the cost of a user's uncompensated
// broken streams once their broken rate crosses the configured threshold.
// It is idempotent: a second run finds nothing uncompensated and credits zero.
func (s *Store) CompensateBrokenStreams(userID string, cfg model.CompensationConfig) (model.CompensationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	recent, err := s.sqliteUsageOf(userID, cfg.WindowSize)
	if err != nil {
		return model.CompensationResult{}, err
	}

	result := model.CompensationResult{TotalCount: len(recent)}
	for _, l := range recent {
		if l.StreamBroken {
			result.BrokenCount++
		}
	}
	if result.TotalCount > 0 {
		result.Rate = float64(result.BrokenCount) / float64(result.TotalCount)
	}
	// Two guards: too few broken calls to trust the rate, or a rate below the
	// threshold. Either way, nothing is credited.
	if result.BrokenCount < cfg.MinBroken || result.Rate < cfg.Threshold {
		return result, nil
	}

	var credit int64
	var ids []string
	for _, l := range recent {
		if l.StreamBroken && !l.Compensated {
			credit += l.Cost
			ids = append(ids, l.ID)
		}
	}
	if err := s.sqliteMarkUsageCompensated(ids); err != nil {
		return model.CompensationResult{}, err
	}
	if err := s.sqliteCreditQuota(userID, credit); err != nil {
		return model.CompensationResult{}, err
	}

	result.Triggered = true
	result.Credited = credit
	result.CompensatedCount = len(ids)
	return result, nil
}

func (s *Store) Compensations() (pending, completed []model.UsageLog) {
	p, c, err := s.sqliteCompensations()
	if err != nil {
		return []model.UsageLog{}, []model.UsageLog{}
	}
	if p == nil {
		p = []model.UsageLog{}
	}
	if c == nil {
		c = []model.UsageLog{}
	}
	return p, c
}
