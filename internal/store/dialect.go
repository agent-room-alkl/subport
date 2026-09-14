package store

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	driverSQLite   = "sqlite"
	driverPostgres = "postgres"
)

// Driver returns "sqlite" or "postgres".
func (s *Store) Driver() string {
	if s == nil || s.driver == "" {
		return driverSQLite
	}
	return s.driver
}

func (s *Store) isPostgres() bool {
	return s.Driver() == driverPostgres
}

// rebindPostgres converts ? placeholders to $1..$n for pgx.
func rebindPostgres(query string) string {
	n := 0
	var b strings.Builder
	b.Grow(len(query) + 8)
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

func (s *Store) rebind(query string) string {
	if s.isPostgres() {
		return rebindPostgres(query)
	}
	return query
}

func (s *Store) exec(query string, args ...any) (sql.Result, error) {
	return s.db.Exec(s.rebind(query), args...)
}

func (s *Store) query(query string, args ...any) (*sql.Rows, error) {
	return s.db.Query(s.rebind(query), args...)
}

func (s *Store) queryRow(query string, args ...any) *sql.Row {
	return s.db.QueryRow(s.rebind(query), args...)
}

func (s *Store) begin() (*storeTx, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	return &storeTx{tx: tx, postgres: s.isPostgres()}, nil
}

type storeTx struct {
	tx       *sql.Tx
	postgres bool
}

func (t *storeTx) rebind(query string) string {
	if t.postgres {
		return rebindPostgres(query)
	}
	return query
}

func (t *storeTx) Exec(query string, args ...any) (sql.Result, error) {
	return t.tx.Exec(t.rebind(query), args...)
}

func (t *storeTx) QueryRow(query string, args ...any) *sql.Row {
	return t.tx.QueryRow(t.rebind(query), args...)
}

func (t *storeTx) Commit() error   { return t.tx.Commit() }
func (t *storeTx) Rollback() error { return t.tx.Rollback() }

// boolArg returns 0/1 for boolean-ish columns. Azure Subport uses SMALLINT for
// these flags (and SQLite uses INTEGER); binding a Go bool breaks pgx against int2.
func (s *Store) boolArg(v bool) any {
	return boolInt(v)
}

func (t *storeTx) boolArg(v bool) any {
	return boolInt(v)
}

// sqlBool scans SQLite INTEGER 0/1 and Postgres BOOLEAN into a Go bool.
type sqlBool bool

func (b *sqlBool) Scan(src any) error {
	if src == nil {
		*b = false
		return nil
	}
	switch v := src.(type) {
	case bool:
		*b = sqlBool(v)
	case int64:
		*b = v != 0
	case int32:
		*b = v != 0
	case int:
		*b = v != 0
	case []byte:
		s := strings.ToLower(string(v))
		*b = s == "1" || s == "t" || s == "true"
	case string:
		s := strings.ToLower(v)
		*b = s == "1" || s == "t" || s == "true"
	default:
		return fmt.Errorf("store: cannot scan %T into sqlBool", src)
	}
	return nil
}

func (b sqlBool) Bool() bool { return bool(b) }

// sqlTime scans TEXT (SQLite) or TIMESTAMPTZ (Postgres) into time.Time.
type sqlTime time.Time

func (t *sqlTime) Scan(src any) error {
	if src == nil {
		*t = sqlTime{}
		return nil
	}
	switch v := src.(type) {
	case time.Time:
		*t = sqlTime(v.UTC())
		return nil
	case string:
		parsed, err := parseDBTime(v)
		if err != nil {
			return err
		}
		*t = sqlTime(parsed)
		return nil
	case []byte:
		parsed, err := parseDBTime(string(v))
		if err != nil {
			return err
		}
		*t = sqlTime(parsed)
		return nil
	default:
		return fmt.Errorf("store: cannot scan %T into sqlTime", src)
	}
}

func (t sqlTime) Time() time.Time { return time.Time(t) }

// sqlTimeText scans TEXT or TIMESTAMPTZ into an RFC3339Nano string.
type sqlTimeText string

func (t *sqlTimeText) Scan(src any) error {
	if src == nil {
		*t = ""
		return nil
	}
	switch v := src.(type) {
	case time.Time:
		*t = sqlTimeText(v.UTC().Format(time.RFC3339Nano))
		return nil
	case string:
		*t = sqlTimeText(v)
		return nil
	case []byte:
		*t = sqlTimeText(string(v))
		return nil
	default:
		return fmt.Errorf("store: cannot scan %T into sqlTimeText", src)
	}
}

func (t sqlTimeText) String() string { return string(t) }

func parseDBTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999Z07",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05Z07",
		"2006-01-02T15:04:05.999999999Z07",
		"2006-01-02T15:04:05Z07",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("store: cannot parse time %q", s)
}
