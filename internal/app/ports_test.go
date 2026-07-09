package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"
)

// mockCards is an in-memory CardRepository for port-level tests.
type mockCards struct {
	mu    sync.Mutex
	byID  map[string]domain.ModelCard
	order []string
}

func newMockCards(cards ...domain.ModelCard) *mockCards {
	m := &mockCards{byID: map[string]domain.ModelCard{}}
	for _, c := range cards {
		m.byID[c.ID] = c
		m.order = append(m.order, c.ID)
	}
	return m
}

func (m *mockCards) Card(_ context.Context, id string) (domain.ModelCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.byID[id]
	if !ok {
		return domain.ModelCard{}, errors.New("card not found")
	}
	return c, nil
}

func (m *mockCards) ListCards(context.Context) ([]domain.ModelCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.ModelCard, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.byID[id])
	}
	return out, nil
}

func (m *mockCards) CreateCard(_ context.Context, c domain.ModelCard) (domain.ModelCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ID == "" {
		c.ID = "mock-" + c.Name
	}
	m.byID[c.ID] = c
	m.order = append(m.order, c.ID)
	return c, nil
}

func (m *mockCards) UpdateCard(_ context.Context, c domain.ModelCard) (domain.ModelCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[c.ID]; !ok {
		return domain.ModelCard{}, errors.New("card not found")
	}
	m.byID[c.ID] = c
	return c, nil
}

func (m *mockCards) DeleteCard(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byID, id)
	next := m.order[:0]
	for _, x := range m.order {
		if x != id {
			next = append(next, x)
		}
	}
	m.order = next
	return nil
}

func (m *mockCards) UpdateCardOrder(_ context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.order = append([]string{}, ids...)
	return nil
}

func (m *mockCards) UpdateCardProbeState(_ context.Context, id, lastError string, failureCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.byID[id]
	if !ok {
		return errors.New("card not found")
	}
	c.LastError = lastError
	c.FailureCount = failureCount
	m.byID[id] = c
	return nil
}

func (m *mockCards) UpdateCardSchedulerAutoDisabled(_ context.Context, id string, disabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.byID[id]
	if !ok {
		return errors.New("card not found")
	}
	c.SchedulerAutoDisabled = disabled
	m.byID[id] = c
	return nil
}

type mockNotifier struct {
	mu   sync.Mutex
	msgs []string
	err  error
}

func (n *mockNotifier) Send(_ context.Context, message string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.msgs = append(n.msgs, message)
	return n.err
}

type mockProber struct {
	result monitor.ProbeResult
	calls  int
}

func (p *mockProber) Probe(context.Context, string, string, string) monitor.ProbeResult {
	p.calls++
	return p.result
}

func TestPortsWireOnNew(t *testing.T) {
	st, err := store.Open(t.Context(), t.TempDir()+"/ports.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	if svc.Cards == nil || svc.Notify == nil || svc.Prober == nil {
		t.Fatalf("ports not wired: cards=%v notify=%v prober=%v", svc.Cards, svc.Notify, svc.Prober)
	}
	if _, ok := svc.Cards.(*store.Store); !ok {
		t.Fatalf("Cards default should be *store.Store, got %T", svc.Cards)
	}
}

func TestProbeUsesProbeRunnerPort(t *testing.T) {
	st, err := store.Open(t.Context(), t.TempDir()+"/ports-probe.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	cards := newMockCards(domain.ModelCard{
		ID: "c1", Name: "mock", BaseURL: "https://example.test", APIKey: "k",
		Enabled: true, PoolEnabled: true, PoolEnabledSet: true,
	})
	prober := &mockProber{result: monitor.ProbeResult{Success: true, Status: monitor.StatusOperational}}
	svc.Cards = cards
	svc.Prober = prober

	if err := svc.CheckCard(t.Context(), "c1"); err != nil {
		t.Fatal(err)
	}
	if prober.calls != 1 {
		t.Fatalf("Probe calls = %d, want 1", prober.calls)
	}
	got, err := cards.Card(t.Context(), "c1")
	if err != nil {
		t.Fatal(err)
	}
	if got.FailureCount != 0 || got.LastError != "" {
		t.Fatalf("probe state = failures=%d err=%q", got.FailureCount, got.LastError)
	}
}

func TestAlertUsesNotifierPort(t *testing.T) {
	st, err := store.Open(t.Context(), t.TempDir()+"/ports-notify.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	n := &mockNotifier{}
	svc.Notify = n

	cfg, err := st.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg.TelegramBotToken = "t"
	cfg.TelegramChatID = "1"
	if _, err := st.UpdateSettings(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := svc.TestNotification(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(n.msgs) != 1 || n.msgs[0] != "通知规则测试" {
		t.Fatalf("msgs = %#v", n.msgs)
	}
}

func TestGetCardUsesCardRepository(t *testing.T) {
	svc := &Service{
		Cards: newMockCards(domain.ModelCard{ID: "x", Name: "via-port"}),
	}
	got, err := svc.GetCard(t.Context(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "via-port" {
		t.Fatalf("name = %q", got.Name)
	}
}

func TestExportDataDTOIsAppType(t *testing.T) {
	st, err := store.Open(t.Context(), t.TempDir()+"/ports-export.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	out, err := svc.ExportData(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != "1" || out.Tables == nil {
		t.Fatalf("export = %+v", out)
	}
	// Round-trip through app DTO must not require store types at the call site.
	if err := svc.ImportData(t.Context(), out); err != nil {
		t.Fatal(err)
	}
}
