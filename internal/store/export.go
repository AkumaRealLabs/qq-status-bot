package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type ExportData struct {
	Version string              `json:"version"`
	Tables  map[string][]RowMap `json:"tables"`
}

type RowMap map[string]any

var exportTables = []string{
	"settings",
	"upstreams",
	"api_keys",
	"scheduler_cost_bindings",
	"scheduler_cost_field_ownership",
	"balance_snapshots",
	"alert_events",
	"ops_events",
	"audit_logs",
	"revenue_snapshots",
	"balance_recharge_logs",
	"scheduler_logs",
	"revenue_cards",
}

func (s *Store) ExportData(ctx context.Context) (ExportData, error) {
	out := ExportData{Version: "1", Tables: map[string][]RowMap{}}
	for _, table := range exportTables {
		rows, err := s.query(ctx, `SELECT * FROM `+quoteIdent(table))
		if err != nil {
			return out, err
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return out, err
		}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return out, err
			}
			row := RowMap{}
			for i, col := range cols {
				if b, ok := vals[i].([]byte); ok {
					row[col] = string(b)
				} else {
					row[col] = vals[i]
				}
			}
			out.Tables[table] = append(out.Tables[table], row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return out, err
		}
		rows.Close()
	}
	return out, nil
}

func (s *Store) ImportData(ctx context.Context, in ExportData) error {
	if in.Version != "1" {
		return errors.New("unsupported export version")
	}
	if len(in.Tables) == 0 {
		return errors.New("empty import data")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = tx.Rollback()
		}
	}()
	for i := len(exportTables) - 1; i >= 0; i-- {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+quoteIdent(exportTables[i])); err != nil {
			return err
		}
	}
	for _, table := range exportTables {
		cols, err := queryColumns(ctx, tx, table)
		if err != nil {
			return err
		}
		for _, row := range in.Tables[table] {
			var names []string
			var vals []any
			for name, val := range row {
				if cols[name] {
					names = append(names, name)
					vals = append(vals, val)
				}
			}
			if len(names) == 0 {
				continue
			}
			q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`,
				quoteIdent(table), quoteIdents(names), strings.TrimRight(strings.Repeat("?,", len(names)), ","))
			if _, err := tx.ExecContext(ctx, s.rebind(q), vals...); err != nil {
				return err
			}
		}
	}
	if err := s.normalizeRetiredSettingsTx(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`INSERT INTO settings (id, check_interval_minutes, site_name) VALUES ('default', 5, 'AI 上游监控') ON CONFLICT(id) DO NOTHING`)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	done = true
	return s.ensureDefaultRevenueCard(ctx)
}

func queryColumns(ctx context.Context, db tableQueryer, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM `+quoteIdent(table)+` WHERE 1=0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, col := range cols {
		out[col] = true
	}
	return out, nil
}
