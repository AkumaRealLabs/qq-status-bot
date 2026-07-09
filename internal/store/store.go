package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type Store struct {
	DB     *sql.DB
	Driver string
}

const InitialUserID = "initial-user"

var ErrInitialUserExists = errors.New("initial user already exists")

func Open(ctx context.Context, dsn string) (*Store, error) {
	if dsn == "" {
		dsn = "/app/data/monitor.sqlite"
	}
	driver := "sqlite"
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		driver = "postgres"
	} else if !strings.HasPrefix(dsn, "file:") {
		if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{DB: db, Driver: driver}
	if driver == "sqlite" {
		db.SetMaxOpenConns(1)
		for _, pragma := range []string{
			"PRAGMA foreign_keys=ON",
			"PRAGMA busy_timeout=10000",
			"PRAGMA journal_mode=WAL",
			"PRAGMA synchronous=NORMAL",
		} {
			if _, err := db.ExecContext(ctx, pragma); err != nil {
				_ = db.Close()
				return nil, err
			}
		}
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.DB.Close()
}

func (s *Store) exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return s.DB.ExecContext(ctx, s.rebind(q), args...)
}

func (s *Store) query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return s.DB.QueryContext(ctx, s.rebind(q), args...)
}

func (s *Store) row(ctx context.Context, q string, args ...any) *sql.Row {
	return s.DB.QueryRowContext(ctx, s.rebind(q), args...)
}

func (s *Store) rebind(q string) string {
	if s.Driver != "postgres" {
		return q
	}
	var n int
	var b strings.Builder
	for _, r := range q {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func NewID() string {
	return mustRandomHex(16)
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func NewToken() string {
	return mustRandomHex(32)
}

func mustRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func parseTime(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04:05.000Z"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseOptionalTime(v string) *time.Time {
	t := parseTime(v)
	if t.IsZero() {
		return nil
	}
	return &t
}

func formatOptionalTime(v *time.Time) string {
	if v == nil || v.IsZero() {
		return ""
	}
	return v.UTC().Format(time.RFC3339Nano)
}

func cardAutoDisabledAt(c domain.ModelCard) string {
	if !c.SchedulerAutoDisabled {
		return ""
	}
	return formatOptionalTime(c.SchedulerAutoDisabledAt)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func boolFromInt(v int) bool { return v != 0 }
