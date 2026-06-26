package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMigrateIsRepeatable(t *testing.T) {
	s := testStore(t)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestPocketBaseMigrationIsIdempotent(t *testing.T) {
	ctx := context.Background()
	oldPath := filepath.Join(t.TempDir(), "data.db")
	old, err := sql.Open("sqlite", oldPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.ExecContext(ctx, `CREATE TABLE upstreams (id TEXT PRIMARY KEY, name TEXT, type TEXT, base_url TEXT, enabled INTEGER, created_at TEXT, updated_at TEXT);
		INSERT INTO upstreams VALUES ('u1', '上游', 'newapi', 'https://example.test', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_ = old.Close()

	s := testStore(t)
	if err := s.MigratePocketBase(ctx, oldPath); err != nil {
		t.Fatal(err)
	}
	if err := s.MigratePocketBase(ctx, oldPath); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListUpstreams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "上游" {
		t.Fatalf("rows = %+v", rows)
	}
}
