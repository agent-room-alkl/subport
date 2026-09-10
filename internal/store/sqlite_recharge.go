package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

func (s *Store) sqliteListUsers() ([]model.User, error) {
	rows, err := s.db.Query(`SELECT id,username,password_hash,password_salt,role,quota_total,quota_used,quota_reserved,created_at FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		var u model.User
		var created string
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Salt, &u.Role, &u.QuotaTotal, &u.QuotaUsed, &u.QuotaReserved, &created); err != nil {
			return nil, err
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, u)
	}
	return out, rows.Err()
}

// sqliteAddQuotaCredit raises quota_total so historical quota_used stays meaningful.
func (s *Store) sqliteAddQuotaCredit(userID string, credit int64) (total, used int64, err error) {
	res, err := s.db.Exec(`UPDATE users SET quota_total = quota_total + ? WHERE id=?`, credit, userID)
	if err != nil {
		return 0, 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, 0, ErrNotFound
	}
	err = s.db.QueryRow(`SELECT quota_total, quota_used FROM users WHERE id=?`, userID).Scan(&total, &used)
	return total, used, err
}

func (s *Store) sqliteCreatePaymentOrder(o model.PaymentOrder) error {
	_, err := s.db.Exec(
		`INSERT INTO payment_orders(id,user_id,provider,package_id,amount_fiat_cents,currency,quota_credit,status,provider_trade_no,pay_url,idempotency_key,created_at,paid_at,expires_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.UserID, o.Provider, o.PackageID, o.AmountFiatCents, o.Currency, o.QuotaCredit, o.Status,
		o.ProviderTradeNo, o.PayURL, o.IdempotencyKey, o.CreatedAt, o.PaidAt, o.ExpiresAt,
	)
	return err
}

func scanPaymentOrder(scanner interface{ Scan(dest ...any) error }) (model.PaymentOrder, error) {
	var o model.PaymentOrder
	err := scanner.Scan(
		&o.ID, &o.UserID, &o.Provider, &o.PackageID, &o.AmountFiatCents, &o.Currency, &o.QuotaCredit,
		&o.Status, &o.ProviderTradeNo, &o.PayURL, &o.IdempotencyKey, &o.CreatedAt, &o.PaidAt, &o.ExpiresAt,
	)
	return o, err
}

const paymentOrderCols = `id,user_id,provider,package_id,amount_fiat_cents,currency,quota_credit,status,provider_trade_no,pay_url,idempotency_key,created_at,paid_at,expires_at`

func (s *Store) sqliteGetPaymentOrder(id string) (model.PaymentOrder, error) {
	o, err := scanPaymentOrder(s.db.QueryRow(`SELECT `+paymentOrderCols+` FROM payment_orders WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

func (s *Store) sqliteGetPaymentOrderByIdem(userID, key string) (model.PaymentOrder, error) {
	o, err := scanPaymentOrder(s.db.QueryRow(`SELECT `+paymentOrderCols+` FROM payment_orders WHERE user_id=? AND idempotency_key=?`, userID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

func (s *Store) sqliteUpdateOrderPayURL(id, payURL string) error {
	_, err := s.db.Exec(`UPDATE payment_orders SET pay_url=? WHERE id=?`, payURL, id)
	return err
}

// sqliteMarkOrderPaidAndCredit transitions pending→paid once, inserts a topup,
// and raises quota_total. A second call for the same order is a no-op success.
func (s *Store) sqliteMarkOrderPaidAndCredit(orderID, tradeNo, source, operatorID, note string) (model.PaymentOrder, model.QuotaTopup, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return model.PaymentOrder{}, model.QuotaTopup{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	o, err := scanPaymentOrder(tx.QueryRow(`SELECT `+paymentOrderCols+` FROM payment_orders WHERE id=?`, orderID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.PaymentOrder{}, model.QuotaTopup{}, false, ErrNotFound
	}
	if err != nil {
		return model.PaymentOrder{}, model.QuotaTopup{}, false, err
	}
	if o.Status == model.OrderPaid {
		// Already credited — return existing topup if any.
		var t model.QuotaTopup
		terr := tx.QueryRow(
			`SELECT id,user_id,order_id,credit,source,operator_id,note,created_at FROM quota_topups WHERE order_id=?`,
			orderID,
		).Scan(&t.ID, &t.UserID, &t.OrderID, &t.Credit, &t.Source, &t.OperatorID, &t.Note, &t.CreatedAt)
		if terr != nil && !errors.Is(terr, sql.ErrNoRows) {
			return o, t, false, terr
		}
		return o, t, false, nil
	}
	if o.Status != model.OrderPending {
		return o, model.QuotaTopup{}, false, errors.New("order not pending")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if tradeNo == "" {
		tradeNo = o.ProviderTradeNo
	}
	res, err := tx.Exec(
		`UPDATE payment_orders SET status=?, provider_trade_no=?, paid_at=? WHERE id=? AND status=?`,
		model.OrderPaid, tradeNo, now, orderID, model.OrderPending,
	)
	if err != nil {
		return model.PaymentOrder{}, model.QuotaTopup{}, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Lost the race; re-read as paid.
		o2, err := scanPaymentOrder(tx.QueryRow(`SELECT `+paymentOrderCols+` FROM payment_orders WHERE id=?`, orderID))
		return o2, model.QuotaTopup{}, false, err
	}

	top := model.QuotaTopup{
		ID:         NewID("top"),
		UserID:     o.UserID,
		OrderID:    o.ID,
		Credit:     o.QuotaCredit,
		Source:     source,
		OperatorID: operatorID,
		Note:       note,
		CreatedAt:  now,
	}
	if _, err := tx.Exec(
		`INSERT INTO quota_topups(id,user_id,order_id,credit,source,operator_id,note,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		top.ID, top.UserID, top.OrderID, top.Credit, top.Source, top.OperatorID, top.Note, top.CreatedAt,
	); err != nil {
		return model.PaymentOrder{}, model.QuotaTopup{}, false, err
	}
	if _, err := tx.Exec(`UPDATE users SET quota_total = quota_total + ? WHERE id=?`, o.QuotaCredit, o.UserID); err != nil {
		return model.PaymentOrder{}, model.QuotaTopup{}, false, err
	}

	o.Status = model.OrderPaid
	o.ProviderTradeNo = tradeNo
	o.PaidAt = now
	if err := tx.Commit(); err != nil {
		return model.PaymentOrder{}, model.QuotaTopup{}, false, err
	}
	return o, top, true, nil
}

func (s *Store) sqliteInsertTopup(t model.QuotaTopup) error {
	_, err := s.db.Exec(
		`INSERT INTO quota_topups(id,user_id,order_id,credit,source,operator_id,note,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		t.ID, t.UserID, t.OrderID, t.Credit, t.Source, t.OperatorID, t.Note, t.CreatedAt,
	)
	return err
}

func (s *Store) sqliteListTopups(limit int) ([]model.QuotaTopup, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id,user_id,order_id,credit,source,operator_id,note,created_at FROM quota_topups ORDER BY created_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTopups(rows)
}

func (s *Store) sqliteListTopupsOf(userID string, limit int) ([]model.QuotaTopup, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id,user_id,order_id,credit,source,operator_id,note,created_at FROM quota_topups WHERE user_id=? ORDER BY created_at DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTopups(rows)
}

func scanTopups(rows *sql.Rows) ([]model.QuotaTopup, error) {
	var out []model.QuotaTopup
	for rows.Next() {
		var t model.QuotaTopup
		if err := rows.Scan(&t.ID, &t.UserID, &t.OrderID, &t.Credit, &t.Source, &t.OperatorID, &t.Note, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if out == nil {
		out = []model.QuotaTopup{}
	}
	return out, rows.Err()
}

func (s *Store) sqliteListPaymentOrders(limit int) ([]model.PaymentOrder, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT `+paymentOrderCols+` FROM payment_orders ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PaymentOrder
	for rows.Next() {
		o, err := scanPaymentOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if out == nil {
		out = []model.PaymentOrder{}
	}
	return out, rows.Err()
}
