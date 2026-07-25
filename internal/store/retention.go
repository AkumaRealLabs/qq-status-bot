package store

import (
	"context"
	"time"
)

// 时序/日志表的保留窗口，避免长期个人主机上 SQLite 无限增长。
const (
	BalanceSnapshotRetention    = 30 * 24 * time.Hour
	AlertEventRetention         = 30 * 24 * time.Hour
	OpsEventRetention           = 60 * 24 * time.Hour
	AuditLogRetention           = 90 * 24 * time.Hour
	RevenueSnapshotRetention    = 90 * 24 * time.Hour
	SchedulerLogRetention       = 60 * 24 * time.Hour
	BalanceRechargeLogRetention = 90 * 24 * time.Hour
)

type RetentionStats struct {
	BalanceSnapshots    int64 `json:"balance_snapshots"`
	AlertEvents         int64 `json:"alert_events"`
	OpsEvents           int64 `json:"ops_events"`
	AuditLogs           int64 `json:"audit_logs"`
	RevenueSnapshots    int64 `json:"revenue_snapshots"`
	SchedulerLogs       int64 `json:"scheduler_logs"`
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
		{`DELETE FROM balance_snapshots WHERE checked_at<?`, BalanceSnapshotRetention, &stats.BalanceSnapshots},
		{`DELETE FROM alert_events WHERE created_at<?`, AlertEventRetention, &stats.AlertEvents},
		{`DELETE FROM ops_events WHERE created_at<?`, OpsEventRetention, &stats.OpsEvents},
		{`DELETE FROM audit_logs WHERE created_at<?`, AuditLogRetention, &stats.AuditLogs},
		{`DELETE FROM revenue_snapshots WHERE checked_at<?`, RevenueSnapshotRetention, &stats.RevenueSnapshots},
		{`DELETE FROM scheduler_logs WHERE created_at<?`, SchedulerLogRetention, &stats.SchedulerLogs},
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
	return stats.BalanceSnapshots + stats.AlertEvents + stats.OpsEvents +
		stats.AuditLogs + stats.RevenueSnapshots + stats.SchedulerLogs + stats.BalanceRechargeLogs + stats.ExpiredSessions
}
