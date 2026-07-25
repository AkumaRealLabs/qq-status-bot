package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) MigrationDone(ctx context.Context, source string) (bool, error) {
	var one int
	err := s.row(ctx, `SELECT 1 FROM migration_records WHERE source=?`, source).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) MarkMigration(ctx context.Context, source string) error {
	_, err := s.exec(ctx, `INSERT INTO migration_records (source, migrated_at) VALUES (?, ?) ON CONFLICT(source) DO NOTHING`, source, nowText())
	return err
}

func (s *Store) MigratePocketBase(ctx context.Context, oldPath string) error {
	if oldPath == "" {
		oldPath = "/app/pb_data/data.db"
	}
	if _, err := os.Stat(oldPath); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	source := "pocketbase:" + oldPath
	done, err := s.MigrationDone(ctx, source)
	if err != nil || done {
		return err
	}
	if s.Driver == "sqlite" {
		return s.migratePocketBaseSQLite(ctx, oldPath, source)
	}
	oldDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(oldPath)+"?mode=ro&cache=shared")
	if err != nil {
		return err
	}
	defer oldDB.Close()
	if err := copyPBTable(ctx, oldDB, s, "upstreams", "upstreams", map[string]string{}); err != nil {
		return err
	}
	if err := copyPBTable(ctx, oldDB, s, "upstream_keys", "api_keys", map[string]string{"upstream": "upstream_id", "group": "group_name"}); err != nil {
		return err
	}
	if err := copyPBCostBindings(ctx, oldDB, s); err != nil {
		return err
	}
	for _, table := range []string{"balance_snapshots", "alert_events"} {
		if err := copyPBTable(ctx, oldDB, s, table, table, map[string]string{"upstream": "upstream_id", "card": "card_id"}); err != nil {
			return err
		}
	}
	if err := migratePBSettings(ctx, oldDB, s); err != nil {
		return err
	}
	return s.MarkMigration(ctx, source)
}

func (s *Store) migratePocketBaseSQLite(ctx context.Context, oldPath, source string) error {
	oldDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(oldPath)+"?mode=ro&cache=shared")
	if err != nil {
		return err
	}
	defer oldDB.Close()
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout=10000"); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE `+quoteSQLString("file:"+filepath.ToSlash(oldPath)+"?mode=ro&cache=shared")+` AS pb`); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `DETACH DATABASE pb`) //nolint:errcheck
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(context.Background(), `ROLLBACK`) //nolint:errcheck
		}
	}()
	if err := copyPBTableSQLite(ctx, conn, oldDB, "upstreams", "upstreams", map[string]string{}); err != nil {
		return err
	}
	if err := copyPBTableSQLite(ctx, conn, oldDB, "upstream_keys", "api_keys", map[string]string{"upstream": "upstream_id", "group": "group_name"}); err != nil {
		return err
	}
	if err := copyPBCostBindingsSQLite(ctx, conn, oldDB); err != nil {
		return err
	}
	for _, table := range []string{"balance_snapshots", "alert_events"} {
		if err := copyPBTableSQLite(ctx, conn, oldDB, table, table, map[string]string{"upstream": "upstream_id", "card": "card_id"}); err != nil {
			return err
		}
	}
	if err := migratePBSettingsSQLite(ctx, conn, oldDB); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO migration_records (source, migrated_at) VALUES (?, ?) ON CONFLICT(source) DO NOTHING`, source, nowText()); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

type tableQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type tableRowQueryer interface {
	tableQueryer
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func tableExists(ctx context.Context, db tableRowQueryer, table string) bool {
	var name string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	return err == nil
}

func tableColumns(ctx context.Context, db tableQueryer, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+quoteIdent(table)+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteIdents(cols []string) string {
	out := make([]string, len(cols))
	for i, col := range cols {
		out[i] = quoteIdent(col)
	}
	return strings.Join(out, ",")
}

func quoteSQLString(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

func copyPBTableSQLite(ctx context.Context, conn *sql.Conn, oldDB *sql.DB, sourceTable, dstTable string, aliases map[string]string) error {
	return copyPBTableSQLiteWhere(ctx, conn, oldDB, sourceTable, dstTable, aliases, "")
}

func copyPBCostBindingsSQLite(ctx context.Context, conn *sql.Conn, oldDB *sql.DB) error {
	if !tableExists(ctx, oldDB, "model_cards") {
		return nil
	}
	where := ""
	if cols, err := tableColumns(ctx, oldDB, "model_cards"); err != nil {
		return err
	} else if cols["pool_enabled"] {
		where = ` WHERE COALESCE(` + quoteIdent("pool_enabled") + `, 1)=1`
	}
	return copyPBTableSQLiteWhere(ctx, conn, oldDB, "model_cards", "scheduler_cost_bindings", map[string]string{"upstream": "upstream_id", "key": "key_id"}, where)
}

func copyPBTableSQLiteWhere(ctx context.Context, conn *sql.Conn, oldDB *sql.DB, sourceTable, dstTable string, aliases map[string]string, where string) error {
	if !tableExists(ctx, oldDB, sourceTable) {
		return nil
	}
	oldCols, err := tableColumns(ctx, oldDB, sourceTable)
	if err != nil {
		return err
	}
	dstCols, err := tableColumns(ctx, conn, dstTable)
	if err != nil {
		return err
	}
	var selectExprs, insertCols []string
	for oldCol := range oldCols {
		newCol := oldCol
		if v := aliases[oldCol]; v != "" {
			newCol = v
		}
		if dstCols[newCol] {
			selectExprs = append(selectExprs, quoteIdent(oldCol))
			insertCols = append(insertCols, newCol)
		}
	}
	for _, col := range []string{"created_at", "updated_at"} {
		if dstCols[col] && !oldCols[col] {
			selectExprs = append(selectExprs, quoteSQLString(nowText()))
			insertCols = append(insertCols, col)
		}
	}
	if len(insertCols) == 0 {
		return nil
	}
	_, err = conn.ExecContext(ctx, fmt.Sprintf(
		`INSERT OR IGNORE INTO %s (%s) SELECT %s FROM pb.%s%s`,
		quoteIdent(dstTable), quoteIdents(insertCols), strings.Join(selectExprs, ","), quoteIdent(sourceTable), where,
	))
	return err
}

func copyPBTable(ctx context.Context, oldDB *sql.DB, dst *Store, sourceTable, dstTable string, aliases map[string]string) error {
	return copyPBTableWhere(ctx, oldDB, dst, sourceTable, dstTable, aliases, "")
}

func copyPBCostBindings(ctx context.Context, oldDB *sql.DB, dst *Store) error {
	if !tableExists(ctx, oldDB, "model_cards") {
		return nil
	}
	where := ""
	if cols, err := tableColumns(ctx, oldDB, "model_cards"); err != nil {
		return err
	} else if cols["pool_enabled"] {
		where = ` WHERE COALESCE(` + quoteIdent("pool_enabled") + `, 1)=1`
	}
	return copyPBTableWhere(ctx, oldDB, dst, "model_cards", "scheduler_cost_bindings", map[string]string{"upstream": "upstream_id", "key": "key_id"}, where)
}

func copyPBTableWhere(ctx context.Context, oldDB *sql.DB, dst *Store, sourceTable, dstTable string, aliases map[string]string, where string) error {
	if !tableExists(ctx, oldDB, sourceTable) {
		return nil
	}
	oldCols, err := tableColumns(ctx, oldDB, sourceTable)
	if err != nil {
		return err
	}
	dstCols, err := tableColumns(ctx, dst.DB, dstTable)
	if err != nil {
		return err
	}
	var selectCols, insertCols, defaultCols []string
	for oldCol := range oldCols {
		newCol := oldCol
		if v := aliases[oldCol]; v != "" {
			newCol = v
		}
		if dstCols[newCol] {
			selectCols = append(selectCols, oldCol)
			insertCols = append(insertCols, newCol)
		}
	}
	for _, col := range []string{"created_at", "updated_at"} {
		if dstCols[col] && !oldCols[col] {
			insertCols = append(insertCols, col)
			defaultCols = append(defaultCols, col)
		}
	}
	if len(insertCols) == 0 {
		return nil
	}
	var realSelectCols, quotedSelectCols []string
	for _, col := range selectCols {
		realSelectCols = append(realSelectCols, col)
		quotedSelectCols = append(quotedSelectCols, quoteIdent(col))
	}
	rows, err := oldDB.QueryContext(ctx, `SELECT `+strings.Join(quotedSelectCols, ",")+` FROM `+quoteIdent(sourceTable)+where)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		vals := make([]any, len(realSelectCols))
		ptrs := make([]any, len(realSelectCols))
		for i := range realSelectCols {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		for range defaultCols {
			vals = append(vals, nowText())
		}
		q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(id) DO NOTHING`,
			quoteIdent(dstTable), quoteIdents(insertCols), strings.TrimRight(strings.Repeat("?,", len(insertCols)), ","))
		if _, err := dst.exec(ctx, q, vals...); err != nil {
			return err
		}
	}
	return rows.Err()
}

func migratePBSettingsSQLite(ctx context.Context, conn *sql.Conn, oldDB *sql.DB) error {
	if !tableExists(ctx, oldDB, "settings") {
		return nil
	}
	cols, err := tableColumns(ctx, oldDB, "settings")
	if err != nil {
		return err
	}
	pick := func(name string) string {
		if cols[name] {
			return quoteIdent(name)
		}
		return "''"
	}
	row := oldDB.QueryRowContext(ctx, `SELECT `+pick("telegram_bot_token")+`, `+pick("telegram_chat_id")+`, `+pick("check_interval_minutes")+` FROM settings LIMIT 1`)
	var token, chat string
	var interval any
	if err := row.Scan(&token, &chat, &interval); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	minutes := 5
	switch v := interval.(type) {
	case int64:
		minutes = int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			minutes = n
		}
	}
	_, err = conn.ExecContext(ctx, `UPDATE settings SET telegram_bot_token=?, telegram_chat_id=?, check_interval_minutes=? WHERE id='default'`,
		token, chat, domain.NormalizeCheckInterval(minutes))
	return err
}

func migratePBSettings(ctx context.Context, oldDB *sql.DB, dst *Store) error {
	if !tableExists(ctx, oldDB, "settings") {
		return nil
	}
	cols, err := tableColumns(ctx, oldDB, "settings")
	if err != nil {
		return err
	}
	pick := func(name string) string {
		if cols[name] {
			return name
		}
		return "''"
	}
	row := oldDB.QueryRowContext(ctx, `SELECT `+pick("telegram_bot_token")+`, `+pick("telegram_chat_id")+`, `+pick("check_interval_minutes")+` FROM settings LIMIT 1`)
	var token, chat string
	var interval any
	if err := row.Scan(&token, &chat, &interval); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	minutes := 5
	switch v := interval.(type) {
	case int64:
		minutes = int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			minutes = n
		}
	}
	_, err = dst.exec(ctx, `UPDATE settings SET telegram_bot_token=?, telegram_chat_id=?, check_interval_minutes=? WHERE id='default'`,
		token, chat, domain.NormalizeCheckInterval(minutes))
	return err
}
