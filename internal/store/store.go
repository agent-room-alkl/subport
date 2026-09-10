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
// Every seeded account uses Provider "mock" because they all point at this
// process's own demo upstream. Naming them "openai" would route them through
// the real OpenAI adapter, which would call /v1/chat/completions on a server
// that only serves /mock/upstream - the demo would break with a confusing
// "no healthy account". A real account gets Provider "openai" AND a real
// BaseURL together; the two must never be set apart.
func seedAccounts(base string) []model.Account {
	return []model.Account{
		{ID: "acct-openai-1", Name: "OpenAI primary (demo)", Provider: "mock", BaseURL: base, Priority: 1, Healthy: true},
		{ID: "acct-openai-2", Name: "OpenAI sibling (demo)", Provider: "mock", BaseURL: base, Priority: 1, Healthy: true},
		{ID: "acct-anthropic-1", Name: "Anthropic backup (demo)", Provider: "mock", BaseURL: base, Priority: 2, Healthy: true},
		{ID: "acct-stream-break", Name: "Stream-break demo", Provider: "mock", BaseURL: base, Priority: 3, Healthy: false},
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

// ListAllKeys returns every API key for admin listing. Callers must omit
// SecretSHA from any response.
func (s *Store) ListAllKeys() []model.APIKey {
	keys, err := s.sqliteListAllKeys()
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

// UsageRecent returns the newest usage_logs across all users (admin view).
func (s *Store) UsageRecent(limit int) []model.UsageLog {
	logs, err := s.sqliteUsageRecent(limit)
	if err != nil || logs == nil {
		return []model.UsageLog{}
	}
	return logs
}

// CountUsageSince counts usage_logs with created_at >= since (RFC3339 / Nano).
func (s *Store) CountUsageSince(sinceRFC3339 string) int {
	n, err := s.sqliteCountUsageSince(sinceRFC3339)
	if err != nil {
		return 0
	}
	return n
}

// ReserveQuota admits a request by holding `amount` of the user's quota before
// the upstream call, and returns ErrQuotaExhausted when it does not fit.
//
// Every caller that reserves MUST later either SettleUsage with the same
// amount or ReleaseQuota it. A reservation that is never resolved is quota the
// user has silently lost.
func (s *Store) ReserveQuota(userID string, amount int64) error {
	if amount <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteReserveQuota(userID, amount)
}

// SetQuotaTotal changes a user's allowance. A total of 0 or less means
// unlimited, which is the convention the admission SQL already encodes.
func (s *Store) SetQuotaTotal(userID string, total int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE users SET quota_total = ? WHERE id=?`, total, userID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReleaseQuota returns a hold for a request that will never be billed - the
// upstream refused, the client vanished, the handler panicked.
func (s *Store) ReleaseQuota(userID string, amount int64) error {
	if amount <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteReleaseQuota(userID, amount)
}

// SettleUsage records a completed call and closes out its reservation: the log
// row, the real charge, and the release all commit together. Pass reserved=0
// when the call was never admitted through ReserveQuota.
func (s *Store) SettleUsage(l model.UsageLog, reserved int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	l.ID = NewID("use")
	l.CreatedAt = time.Now().UTC()
	return s.sqliteSettleUsage(l, reserved)
}

// AddUsage bills a call that held no reservation. It is SettleUsage with
// nothing to release rather than a second write path: two ways to write the
// same state is how the two halves drift apart, which is the whole reason the
// JSON mirror was removed.
func (s *Store) AddUsage(l model.UsageLog) error {
	return s.SettleUsage(l, 0)
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

// UpsertAccount inserts or updates one upstream account row. Credentials are
// never part of the row - only where to call (provider/base_url) and scheduling
// fields. Used by Claude subscription env bootstrap.
func (s *Store) UpsertAccount(a model.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteUpsertAccount(a)
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

// ---------------------------------------------------------------- pricing

// SetModelPrice writes or replaces one model's rate card.
func (s *Store) SetModelPrice(p model.ModelPrice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteSetModelPrice(p)
}

// ModelPrice returns a model's rate card, or ErrNotFound.
func (s *Store) ModelPrice(name string) (model.ModelPrice, error) {
	return s.sqliteModelPrice(name)
}

// ModelPrices lists the whole price list.
func (s *Store) ModelPrices() []model.ModelPrice {
	out, err := s.sqliteModelPrices()
	if err != nil {
		return []model.ModelPrice{}
	}
	return out
}

// SetGroupRatio sets a billing group's multiplier, in RatioScale units.
func (s *Store) SetGroupRatio(name string, ratio int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteSetGroupRatio(name, ratio)
}

// SetUserBilling puts a user in a billing group, optionally with a personal
// override. Pass override 0 to mean "no override" - a real 0x ratio would be
// free, which must be spelled out deliberately rather than fallen into.
func (s *Store) SetUserBilling(userID, group string, override int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteSetUserBilling(userID, group, override)
}

// EffectiveRatio resolves what a user is actually charged at.
//
// Order is: personal override, then their group, then list price. A missing
// group is list price, NOT free: a typo in a group name must cost the customer
// nothing and cost us nothing either.
func (s *Store) EffectiveRatio(userID string) int64 {
	group, override, err := s.sqliteUserBilling(userID)
	if err != nil {
		return DefaultRatio
	}
	if override > 0 {
		return override
	}
	if group == "" {
		return DefaultRatio
	}
	ratio, err := s.sqliteGroupRatio(group)
	if err != nil || ratio <= 0 {
		return DefaultRatio
	}
	return ratio
}

// PriceCall works out what a call costs and the snapshot to record with it.
//
// The snapshot is returned alongside the cost, not looked up again at write
// time, so the number billed and the number explaining it can never disagree.
// A model with no rate card falls back to flat 1:1, which is what billing did
// before any of this existed - unpriced must not mean free.
func (s *Store) PriceCall(userID, modelName string, counts model.TokenCounts) (int64, model.BilledRate) {
	price, err := s.sqliteModelPrice(modelName)
	if err != nil {
		price = model.FlatPrice(modelName, model.RateScale)
	}
	rate := model.RateFor(price, s.EffectiveRatio(userID))
	return model.Cost(counts, rate), rate
}
