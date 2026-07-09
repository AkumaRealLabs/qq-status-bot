package store

import (
	"context"
	"database/sql"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	return n, s.row(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (domain.User, error) {
	u := domain.User{ID: NewID(), Username: username, PasswordHash: passwordHash, CreatedAt: time.Now().UTC()}
	_, err := s.exec(ctx, `INSERT INTO users (id, username, password_hash, created_at) VALUES (?, ?, ?, ?)`, u.ID, u.Username, u.PasswordHash, u.CreatedAt.Format(time.RFC3339Nano))
	return u, err
}

func (s *Store) CreateInitialUser(ctx context.Context, username, passwordHash string) (domain.User, error) {
	u := domain.User{ID: InitialUserID, Username: username, PasswordHash: passwordHash, CreatedAt: time.Now().UTC()}
	res, err := s.exec(ctx, `INSERT INTO users (id, username, password_hash, created_at)
		SELECT ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`, u.ID, u.Username, u.PasswordHash, u.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return u, err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return u, ErrInitialUserExists
	}
	return u, nil
}

func (s *Store) UserByUsername(ctx context.Context, username string) (domain.User, error) {
	return s.scanUser(s.row(ctx, `SELECT id, username, password_hash, created_at FROM users WHERE username=?`, username))
}

func (s *Store) UserBySessionToken(ctx context.Context, token string) (domain.User, error) {
	hash := HashToken(token)
	return s.scanUser(s.row(ctx, `SELECT u.id, u.username, u.password_hash, u.created_at
		FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?`, hash, nowText()))
}

func (s *Store) scanUser(row *sql.Row) (domain.User, error) {
	var u domain.User
	var created string
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &created); err != nil {
		return u, err
	}
	u.CreatedAt = parseTime(created)
	return u, nil
}

func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	token := NewToken()
	_, err := s.exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		NewID(), userID, HashToken(token), time.Now().UTC().Add(ttl).Format(time.RFC3339Nano), nowText())
	return token, err
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.exec(ctx, `DELETE FROM sessions WHERE token_hash=?`, HashToken(token))
	return err
}

func (s *Store) CleanupSessions(ctx context.Context) error {
	_, err := s.exec(ctx, `DELETE FROM sessions WHERE expires_at<=?`, nowText())
	return err
}
