package store

import (
	"database/sql"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

func scanSQLiteUser(row *sql.Row) (model.User, error) {
	var u model.User
	var created string
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Salt, &u.Role, &u.QuotaTotal, &u.QuotaUsed, &u.QuotaReserved, &created)
	if err != nil {
		return u, err
	}
	u.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	return u, err
}

func (s *Store) sqliteCreateUser(u model.User) error {
	_, err := s.db.Exec(`INSERT INTO users(id,username,password_hash,password_salt,role,quota_total,quota_used,created_at) VALUES(?,?,?,?,?,?,?,?)`, u.ID, u.Username, u.PasswordHash, u.Salt, u.Role, u.QuotaTotal, u.QuotaUsed, u.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) sqliteUserByName(username string) (model.User, error) {
	return scanSQLiteUser(s.db.QueryRow(`SELECT id,username,password_hash,password_salt,role,quota_total,quota_used,quota_reserved,created_at FROM users WHERE username=?`, username))
}

func (s *Store) sqliteUserByID(id string) (model.User, error) {
	return scanSQLiteUser(s.db.QueryRow(`SELECT id,username,password_hash,password_salt,role,quota_total,quota_used,quota_reserved,created_at FROM users WHERE id=?`, id))
}

func (s *Store) sqliteNewSession(sess model.Session) error {
	_, err := s.db.Exec(`INSERT INTO sessions(token,user_id,expires_at) VALUES(?,?,?)`, sess.Token, sess.UserID, sess.ExpiresAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) sqliteSessionUser(token string) (string, time.Time, error) {
	var userID, expires string
	err := s.db.QueryRow(`SELECT user_id,expires_at FROM sessions WHERE token=?`, token).Scan(&userID, &expires)
	if err != nil {
		return "", time.Time{}, err
	}
	exp, err := time.Parse(time.RFC3339Nano, expires)
	return userID, exp, err
}

func (s *Store) sqliteDropSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token=?`, token)
	return err
}

// sqliteUsernameTaken compares on the normalised form so "Admin" cannot be
// registered alongside "admin".
func (s *Store) sqliteUsernameTaken(norm string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username=?`, norm).Scan(&n)
	return n > 0, err
}

// sqliteImportUsers is part of the one-way JSON import; it is a no-op once the
// users table has anything in it.
func (s *Store) sqliteImportUsers(users []model.User) error {
	for _, u := range users {
		if err := s.sqliteCreateUser(u); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) sqliteImportSessions(sessions []model.Session) error {
	for _, sess := range sessions {
		if err := s.sqliteNewSession(sess); err != nil {
			return err
		}
	}
	return nil
}
