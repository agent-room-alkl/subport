package store

// Quota admission.
//
// The bug this replaces: quota was READ before the upstream call and only
// written after it. Between those two points a request holds nothing, so N
// concurrent requests from one user all read the same quota_used, all pass,
// and all bill. A user with 1 quota left and 50 requests in flight overshoots
// by 49. For a product that exists to bill subscription quota accurately,
// that is the wrong direction to be wrong in.
//
// The fix is to make admission a single conditional UPDATE. SQLite applies it
// atomically, so the check and the hold cannot be separated by anything - no
// window, no lock ordering to get right, no read-modify-write.

import (
	"errors"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

// ErrQuotaExhausted means admission was refused: the user's remaining quota
// could not cover this request's reservation. It is a normal outcome, not a
// failure - callers turn it into a 402.
var ErrQuotaExhausted = errors.New("quota exhausted")

// sqliteReserveQuota holds `amount` against a user, atomically, and only if it
// fits. quota_total <= 0 means unlimited, matching the existing convention.
//
// The whole point is that the condition lives INSIDE the UPDATE. Reading the
// row first and deciding in Go would reintroduce exactly the race being fixed.
func (s *Store) sqliteReserveQuota(userID string, amount int64) error {
	res, err := s.db.Exec(
		`UPDATE users SET quota_reserved = quota_reserved + ?
		 WHERE id = ? AND (quota_total <= 0 OR quota_used + quota_reserved + ? <= quota_total)`,
		amount, userID, amount,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}

	// Zero rows means either "would not fit" or "no such user". They are
	// different bugs and must not be reported as the same one.
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id=?`, userID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	return ErrQuotaExhausted
}

// sqliteReleaseQuota gives back a hold that will never be billed. The floor at
// zero is deliberate: a double release must not manufacture quota out of a
// negative reservation.
func (s *Store) sqliteReleaseQuota(userID string, amount int64) error {
	_, err := s.db.Exec(
		`UPDATE users SET quota_reserved = CASE WHEN quota_reserved > ? THEN quota_reserved - ? ELSE 0 END WHERE id=?`,
		amount, amount, userID,
	)
	return err
}

// sqliteSettleUsage is the single write path for billing. It records the call,
// charges the real cost, and releases the hold - in one transaction, so a
// crash can never leave a charge without a record or a hold without a request.
//
// reserved is what admission held; cost is what actually happened. They are
// independent numbers: over-reserving and under-reserving both settle
// correctly, because the hold is released in full and the charge is applied in
// full rather than one being derived from the other.
func (s *Store) sqliteSettleUsage(l model.UsageLog, reserved int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`INSERT INTO usage_logs(id,user_id,key_id,model,tokens,cost,status,account_id,attempts,stream_broken,compensated,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.ID, l.UserID, l.KeyID, l.Model, l.Tokens, l.Cost, l.Status, l.AccountID, l.Attempts, boolInt(l.StreamBroken), boolInt(l.Compensated), l.CreatedAt.Format(time.RFC3339Nano),
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE users SET quota_used = quota_used + ? WHERE id=?`, l.Cost, l.UserID); err != nil {
		return err
	}
	if reserved > 0 {
		if _, err := tx.Exec(
			`UPDATE users SET quota_reserved = CASE WHEN quota_reserved > ? THEN quota_reserved - ? ELSE 0 END WHERE id=?`,
			reserved, reserved, l.UserID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}
