package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) TGSession(ctx context.Context) (domain.TGSession, error) {
	var sess domain.TGSession
	var authorized, passwordNeeded int
	var created, updated string
	err := s.row(ctx, `SELECT id, api_id, api_hash, phone, code_hash, session_blob, authorized, password_needed, last_error, created_at, updated_at FROM tg_session WHERE id='default'`).
		Scan(&sess.ID, &sess.APIID, &sess.APIHash, &sess.Phone, &sess.CodeHash, &sess.SessionBlob, &authorized, &passwordNeeded, &sess.LastError, &created, &updated)
	sess.Authorized = boolFromInt(authorized)
	sess.PasswordNeeded = boolFromInt(passwordNeeded)
	sess.CreatedAt, sess.UpdatedAt = parseTime(created), parseTime(updated)
	return sess, err
}

func (s *Store) SaveTGSession(ctx context.Context, sess domain.TGSession) (domain.TGSession, error) {
	if sess.ID == "" {
		sess.ID = "default"
	}
	if sess.SessionBlob == nil {
		var blob []byte
		err := s.row(ctx, `SELECT session_blob FROM tg_session WHERE id=?`, sess.ID).Scan(&blob)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return sess, err
		}
		sess.SessionBlob = blob
	}
	now := nowText()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = parseTime(now)
	}
	sess.UpdatedAt = parseTime(now)
	_, err := s.exec(ctx, `INSERT INTO tg_session
		(id, api_id, api_hash, phone, code_hash, session_blob, authorized, password_needed, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET api_id=excluded.api_id, api_hash=excluded.api_hash, phone=excluded.phone,
			code_hash=excluded.code_hash, session_blob=excluded.session_blob, authorized=excluded.authorized,
			password_needed=excluded.password_needed, last_error=excluded.last_error, updated_at=excluded.updated_at`,
		sess.ID, sess.APIID, sess.APIHash, sess.Phone, sess.CodeHash, sess.SessionBlob, boolInt(sess.Authorized), boolInt(sess.PasswordNeeded), sess.LastError,
		sess.CreatedAt.Format(time.RFC3339Nano), sess.UpdatedAt.Format(time.RFC3339Nano))
	return sess, err
}

func (s *Store) StoreTGSessionBlob(ctx context.Context, data []byte) error {
	_, err := s.exec(ctx, `UPDATE tg_session SET session_blob=?, updated_at=? WHERE id='default'`, data, nowText())
	return err
}

func (s *Store) CreateTGChannel(ctx context.Context, c domain.TGChannel) (domain.TGChannel, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	if c.MessageLimit <= 0 {
		c.MessageLimit = 10
	}
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO tg_channels
		(id, display_name, identifier, username, peer_id, access_hash, avatar_url, enabled, message_limit, pinned_only, last_sync_at, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(peer_id) DO UPDATE SET display_name=excluded.display_name, identifier=excluded.identifier, username=excluded.username,
			access_hash=excluded.access_hash,
			avatar_url=CASE WHEN excluded.avatar_url <> '' THEN excluded.avatar_url ELSE tg_channels.avatar_url END,
			updated_at=excluded.updated_at`,
		c.ID, c.DisplayName, c.Identifier, c.Username, c.PeerID, c.AccessHash, c.AvatarURL, boolInt(c.Enabled), c.MessageLimit, boolInt(c.PinnedOnly),
		c.LastSyncAt.Format(time.RFC3339Nano), c.LastError, c.CreatedAt.Format(time.RFC3339Nano), c.UpdatedAt.Format(time.RFC3339Nano))
	return c, err
}

func (s *Store) UpdateTGChannel(ctx context.Context, c domain.TGChannel) (domain.TGChannel, error) {
	c.UpdatedAt = time.Now().UTC()
	if c.MessageLimit <= 0 {
		c.MessageLimit = 10
	}
	_, err := s.exec(ctx, `UPDATE tg_channels SET display_name=?, identifier=?, username=?, peer_id=?, access_hash=?, avatar_url=?, enabled=?, message_limit=?, pinned_only=?, last_sync_at=?, last_error=?, updated_at=? WHERE id=?`,
		c.DisplayName, c.Identifier, c.Username, c.PeerID, c.AccessHash, c.AvatarURL, boolInt(c.Enabled), c.MessageLimit, boolInt(c.PinnedOnly),
		c.LastSyncAt.Format(time.RFC3339Nano), c.LastError, c.UpdatedAt.Format(time.RFC3339Nano), c.ID)
	if err != nil {
		return c, err
	}
	return c, s.TrimTGMessages(ctx, c.ID, c.MessageLimit)
}

func (s *Store) DeleteTGChannel(ctx context.Context, id string) error {
	for _, stmt := range []string{`DELETE FROM tg_messages WHERE channel_id=?`, `DELETE FROM tg_channels WHERE id=?`} {
		if _, err := s.exec(ctx, stmt, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteTGMessages(ctx context.Context, channelID string) error {
	_, err := s.exec(ctx, `DELETE FROM tg_messages WHERE channel_id=?`, channelID)
	return err
}

func (s *Store) DeleteAllTGMessages(ctx context.Context) error {
	_, err := s.exec(ctx, `DELETE FROM tg_messages`)
	return err
}

func (s *Store) DeleteTGMessage(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `DELETE FROM tg_messages WHERE id=?`, id)
	return err
}

func (s *Store) TrimTGMessages(ctx context.Context, channelID string, limit int) error {
	if limit <= 0 {
		limit = 10
	}
	_, err := s.exec(ctx, `DELETE FROM tg_messages WHERE id IN (
		SELECT id FROM (
			SELECT id, ROW_NUMBER() OVER (ORDER BY published_at DESC, remote_id DESC) rn FROM tg_messages WHERE channel_id=?
		) ranked WHERE rn > ?
	)`, channelID, limit)
	return err
}

func (s *Store) TGChannel(ctx context.Context, id string) (domain.TGChannel, error) {
	return s.scanTGChannel(s.row(ctx, tgChannelSelectSQL()+` WHERE id=?`, id))
}

func (s *Store) ListTGChannels(ctx context.Context) ([]domain.TGChannel, error) {
	rows, err := s.query(ctx, tgChannelSelectSQL()+` ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TGChannel{}
	for rows.Next() {
		c, err := scanTGChannelRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) scanTGChannel(row *sql.Row) (domain.TGChannel, error) {
	var c domain.TGChannel
	var enabled, pinnedOnly int
	var lastSync, created, updated string
	err := row.Scan(&c.ID, &c.DisplayName, &c.Identifier, &c.Username, &c.PeerID, &c.AccessHash, &c.AvatarURL, &enabled, &c.MessageLimit, &pinnedOnly, &lastSync, &c.LastError, &created, &updated)
	c.Enabled = boolFromInt(enabled)
	c.PinnedOnly = boolFromInt(pinnedOnly)
	c.LastSyncAt, c.CreatedAt, c.UpdatedAt = parseTime(lastSync), parseTime(created), parseTime(updated)
	return c, err
}

func scanTGChannelRows(rows *sql.Rows) (domain.TGChannel, error) {
	var c domain.TGChannel
	var enabled, pinnedOnly int
	var lastSync, created, updated string
	err := rows.Scan(&c.ID, &c.DisplayName, &c.Identifier, &c.Username, &c.PeerID, &c.AccessHash, &c.AvatarURL, &enabled, &c.MessageLimit, &pinnedOnly, &lastSync, &c.LastError, &created, &updated)
	c.Enabled = boolFromInt(enabled)
	c.PinnedOnly = boolFromInt(pinnedOnly)
	c.LastSyncAt, c.CreatedAt, c.UpdatedAt = parseTime(lastSync), parseTime(created), parseTime(updated)
	return c, err
}

func tgChannelSelectSQL() string {
	return `SELECT id, display_name, identifier, username, peer_id, access_hash, avatar_url, enabled, message_limit, pinned_only, last_sync_at, last_error, created_at, updated_at FROM tg_channels`
}

func (s *Store) SaveTGMessage(ctx context.Context, msg domain.TGMessage) (domain.TGMessage, error) {
	if msg.ID == "" {
		msg.ID = NewID()
	}
	now := time.Now().UTC()
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = now
	}
	msg.UpdatedAt = now
	_, err := s.exec(ctx, `INSERT INTO tg_messages
		(id, channel_id, remote_id, published_at, text, media_type, media_path, media_url, media_cached, link, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel_id, remote_id) DO UPDATE SET published_at=excluded.published_at, text=excluded.text,
			media_type=excluded.media_type, media_path=excluded.media_path, media_url=excluded.media_url, media_cached=excluded.media_cached,
			link=excluded.link, updated_at=excluded.updated_at`,
		msg.ID, msg.ChannelID, msg.RemoteID, msg.PublishedAt.Format(time.RFC3339Nano), msg.Text, msg.MediaType, msg.MediaPath, msg.MediaURL,
		boolInt(msg.MediaCached), msg.Link, msg.CreatedAt.Format(time.RFC3339Nano), msg.UpdatedAt.Format(time.RFC3339Nano))
	return msg, err
}

func (s *Store) TGMessages(ctx context.Context, channelID string, limit int) ([]domain.TGMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT id, channel_id, display_name, remote_id, published_at, text, media_type, media_path, media_url, media_cached, link, created_at, updated_at FROM (
		SELECT m.id, m.channel_id, c.display_name, m.remote_id, m.published_at, m.text, m.media_type, m.media_path, m.media_url, m.media_cached, m.link, m.created_at, m.updated_at,
			ROW_NUMBER() OVER (PARTITION BY m.channel_id ORDER BY m.published_at DESC, m.remote_id DESC) rn, c.message_limit
		FROM tg_messages m JOIN tg_channels c ON c.id=m.channel_id`
	args := []any{}
	if channelID != "" {
		query += ` WHERE m.channel_id=?`
		args = append(args, channelID)
	}
	query += `) ranked WHERE rn <= message_limit ORDER BY published_at DESC, remote_id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TGMessage{}
	for rows.Next() {
		var msg domain.TGMessage
		var cached int
		var published, created, updated string
		if err := rows.Scan(&msg.ID, &msg.ChannelID, &msg.ChannelName, &msg.RemoteID, &published, &msg.Text, &msg.MediaType, &msg.MediaPath, &msg.MediaURL, &cached, &msg.Link, &created, &updated); err != nil {
			return nil, err
		}
		msg.PublishedAt, msg.CreatedAt, msg.UpdatedAt = parseTime(published), parseTime(created), parseTime(updated)
		msg.MediaCached = boolFromInt(cached)
		out = append(out, msg)
	}
	return out, rows.Err()
}
