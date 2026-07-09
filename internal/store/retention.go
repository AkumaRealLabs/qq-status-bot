package store

import (
	"context"
	"time"
)

// 时序/日志表的保留窗口。保留足够给图表与
// 运维回看使用，同时避免长期个人主机上 SQLite 无限增长。
const (
	ProbeRunRetention              = 14 * 24 * time.Hour
	BalanceSnapshotRetention       = 30 * 24 * time.Hour
	AlertEventRetention            = 30 * 24 * time.Hour
	OpsEventRetention              = 60 * 24 * time.Hour
	AuditLogRetention              = 90 * 24 * time.Hour
	RevenueSnapshotRetention       = 90 * 24 * time.Hour
	CLIProxyQuotaRetention         = 30 * 24 * time.Hour
	SchedulerLogRetention          = 60 * 24 * time.Hour
	SchedulerCostSnapshotRetention = 180 * 24 * time.Hour
	SchedulerSaleSnapshotRetention = 180 * 24 * time.Hour
	BalanceRechargeLogRetention    = 90 * 24 * time.Hour
)

type RetentionStats struct {
	ProbeRuns           int64 `json:"probe_runs"`
	BalanceSnapshots    int64 `json:"balance_snapshots"`
	AlertEvents         int64 `json:"alert_events"`
	OpsEvents           int64 `json:"ops_events"`
	AuditLogs           int64 `json:"audit_logs"`
	RevenueSnapshots    int64 `json:"revenue_snapshots"`
	CLIProxyQuota       int64 `json:"cliproxy_quota_snapshots"`
	SchedulerLogs       int64 `json:"scheduler_logs"`
	SchedulerCostSnaps  int64 `json:"scheduler_channel_cost_snapshots"`
	SchedulerSaleSnaps  int64 `json:"scheduler_group_sale_snapshots"`
	BalanceRechargeLogs int64 `json:"balance_recharge_logs"`
	ExpiredSessions     int64 `json:"expired_sessions"`
}

func (s *Store) CleanupExpiredData(ctx context.Context) (RetentionStats, error) {
	var stats RetentionStats
	now := time.Now().UTC()

	type job struct {
		sql  string
		age  time.Duration
		dest *int64
	}
	jobs := []job{
		{`DELETE FROM probe_runs WHERE checked_at<?`, ProbeRunRetention, &stats.ProbeRuns},
		{`DELETE FROM balance_snapshots WHERE checked_at<?`, BalanceSnapshotRetention, &stats.BalanceSnapshots},
		{`DELETE FROM alert_events WHERE created_at<?`, AlertEventRetention, &stats.AlertEvents},
		{`DELETE FROM ops_events WHERE created_at<?`, OpsEventRetention, &stats.OpsEvents},
		{`DELETE FROM audit_logs WHERE created_at<?`, AuditLogRetention, &stats.AuditLogs},
		{`DELETE FROM revenue_snapshots WHERE checked_at<?`, RevenueSnapshotRetention, &stats.RevenueSnapshots},
		{`DELETE FROM cliproxy_quota_snapshots WHERE checked_at<?`, CLIProxyQuotaRetention, &stats.CLIProxyQuota},
		{`DELETE FROM scheduler_logs WHERE created_at<?`, SchedulerLogRetention, &stats.SchedulerLogs},
		{`DELETE FROM scheduler_channel_cost_snapshots WHERE effective_at<?`, SchedulerCostSnapshotRetention, &stats.SchedulerCostSnaps},
		{`DELETE FROM scheduler_group_sale_snapshots WHERE effective_at<?`, SchedulerSaleSnapshotRetention, &stats.SchedulerSaleSnaps},
		{`DELETE FROM balance_recharge_logs WHERE created_at<?`, BalanceRechargeLogRetention, &stats.BalanceRechargeLogs},
	}
	for _, j := range jobs {
		cutoff := now.Add(-j.age).Format(time.RFC3339Nano)
		res, err := s.exec(ctx, j.sql, cutoff)
		if err != nil {
			return stats, err
		}
		n, _ := res.RowsAffected()
		*j.dest = n
	}
	res, err := s.exec(ctx, `DELETE FROM sessions WHERE expires_at<=?`, nowText())
	if err != nil {
		return stats, err
	}
	stats.ExpiredSessions, _ = res.RowsAffected()
	return stats, nil
}

func (stats RetentionStats) DeletedTotal() int64 {
	return stats.ProbeRuns + stats.BalanceSnapshots + stats.AlertEvents + stats.OpsEvents +
		stats.AuditLogs + stats.RevenueSnapshots + stats.CLIProxyQuota + stats.SchedulerLogs +
		stats.SchedulerCostSnaps + stats.SchedulerSaleSnaps + stats.BalanceRechargeLogs + stats.ExpiredSessions
}
