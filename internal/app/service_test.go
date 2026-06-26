package app

import (
	"path/filepath"
	"testing"

	"ai-upstream-monitor/internal/store"
)

func TestSetupOnlyOnce(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	if _, err := svc.Setup(t.Context(), "admin", "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Setup(t.Context(), "admin2", "secret"); err == nil {
		t.Fatal("second setup succeeded")
	}
}
