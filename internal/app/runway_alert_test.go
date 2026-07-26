package app

import (
	"fmt"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func insertRunwaySnapshot(t *testing.T, svc *Service, upstreamID string, at time.Time, remain float64) {
	t.Helper()
	_, err := svc.Store.DB.ExecContext(t.Context(),
		`INSERT INTO balance_snapshots (id, upstream_id, checked_at, balance, used, remain, requests, error, latency_ms)
		 VALUES (?, ?, ?, ?, 0, ?, 0, '', 0)`,
		fmt.Sprintf("snap-%d", at.UnixNano()), upstreamID, at.UTC().Format(time.RFC3339Nano), remain, remain)
	if err != nil {
		t.Fatal(err)
	}
}

// 消耗速率快到预计支撑不足阈值时，要产生 balance_runway_low 事件；恢复后要出恢复事件。
func TestCheckBalanceRunwayEmitsAndRecovers(t *testing.T) {
	svc, st := newOpsTestService(t)
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{
		Name: "上游A", Type: "sub2api", BaseURL: "http://x", Enabled: true, RunwayWarningHours: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// 12 小时烧掉 6，剩 4 → 预计 8 小时耗尽 < 阈值 24。
	insertRunwaySnapshot(t, svc, u.ID, now.Add(-12*time.Hour), 10)
	insertRunwaySnapshot(t, svc, u.ID, now, 4)

	svc.checkBalanceRunway(t.Context(), u)
	events, err := st.OpsEvents(t.Context(), domain.OpsEventFilter{Type: "balance_runway_low", Limit: 10})
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if events[0].Severity != "warning" || events[0].TargetID != u.ID {
		t.Fatalf("event=%+v", events[0])
	}

	// 充值后窗口内余额净回升（Burning=false）→ 同类型事件出一条 success 的恢复。
	insertRunwaySnapshot(t, svc, u.ID, now.Add(time.Minute), 100)
	insertRunwaySnapshot(t, svc, u.ID, now.Add(2*time.Hour), 99)
	svc.checkBalanceRunway(t.Context(), u)
	events, err = st.OpsEvents(t.Context(), domain.OpsEventFilter{Type: "balance_runway_low", Limit: 10})
	if err != nil || len(events) != 2 {
		t.Fatalf("recover events=%+v err=%v", events, err)
	}
	if events[0].Severity != "success" {
		t.Fatalf("最新事件应为恢复(success)：%+v", events[0])
	}
	if events[0].Message != "上游A 余额消耗速度 已恢复" {
		t.Fatalf("恢复文案 = %q", events[0].Message)
	}
}

// 样本不足（跨度 < 1 小时）时既不告警也不恢复。
func TestCheckBalanceRunwaySkipsInsufficientHistory(t *testing.T) {
	svc, st := newOpsTestService(t)
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{
		Name: "上游B", Type: "sub2api", BaseURL: "http://x", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	insertRunwaySnapshot(t, svc, u.ID, now.Add(-30*time.Minute), 10)
	insertRunwaySnapshot(t, svc, u.ID, now, 1)

	svc.checkBalanceRunway(t.Context(), u)
	events, err := st.OpsEvents(t.Context(), domain.OpsEventFilter{Type: "balance_runway_low", Limit: 10})
	if err != nil || len(events) != 0 {
		t.Fatalf("不应产生事件：events=%+v err=%v", events, err)
	}
}

func TestBalanceTrendPointsDownsamples(t *testing.T) {
	u := domain.Upstream{Type: "sub2api", BalanceRate: 1}
	base := time.Now().UTC().Add(-24 * time.Hour)
	history := make([]domain.BalanceSnapshot, 0, 300)
	for i := 0; i < 300; i++ {
		history = append(history, domain.BalanceSnapshot{CheckedAt: base.Add(time.Duration(i) * 5 * time.Minute), Remain: float64(300 - i)})
	}
	// 混入错误快照，应被剔除。
	history = append(history, domain.BalanceSnapshot{CheckedAt: base.Add(time.Hour), Error: "查询失败"})

	points := balanceTrendPoints(u, history, 48)
	if len(points) != 48 {
		t.Fatalf("应降采样到 48 点，实际 %d", len(points))
	}
	if points[0].Remain != 300 || points[len(points)-1].Remain != 1 {
		t.Fatalf("首尾必须保留：first=%+v last=%+v", points[0], points[len(points)-1])
	}
	for i := 1; i < len(points); i++ {
		if !points[i].At.After(points[i-1].At) {
			t.Fatalf("点必须按时间递增：%d", i)
		}
	}
	if got := balanceTrendPoints(u, history[:5], 48); len(got) != 5 {
		t.Fatalf("样本少于上限时全部保留，实际 %d", len(got))
	}
}
