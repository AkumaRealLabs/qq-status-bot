package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/store"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	tgauth "github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

type TGSessionStatus struct {
	Configured     bool   `json:"configured"`
	Authorized     bool   `json:"authorized"`
	Phone          string `json:"phone,omitempty"`
	APIID          int    `json:"api_id,omitempty"`
	PasswordNeeded bool   `json:"password_needed"`
	LastError      string `json:"last_error,omitempty"`
}

func (s *Service) TGSessionStatus(ctx context.Context) (TGSessionStatus, error) {
	sess, err := s.Store.TGSession(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return TGSessionStatus{}, nil
	}
	if err != nil {
		return TGSessionStatus{}, err
	}
	return tgSessionStatus(sess), nil
}

func (s *Service) StartTGSession(ctx context.Context, apiID int, apiHash, phone string) (TGSessionStatus, error) {
	apiHash, phone = strings.TrimSpace(apiHash), strings.TrimSpace(phone)
	if apiID <= 0 || apiHash == "" || phone == "" {
		return TGSessionStatus{}, ErrBadRequest("api_id, api_hash and phone are required")
	}
	sess := domain.TGSession{ID: "default", APIID: apiID, APIHash: apiHash, Phone: phone}
	if _, err := s.Store.SaveTGSession(ctx, sess); err != nil {
		return TGSessionStatus{}, err
	}
	err := s.withTGClient(ctx, sess, func(ctx context.Context, client *telegram.Client) error {
		sent, err := client.Auth().SendCode(ctx, phone, tgauth.SendCodeOptions{})
		if err != nil {
			return err
		}
		code, ok := sent.(*tg.AuthSentCode)
		if !ok {
			sess.Authorized = true
			sess.CodeHash = ""
			return nil
		}
		sess.CodeHash = code.PhoneCodeHash
		return nil
	})
	if err != nil {
		sess.LastError = err.Error()
		_, _ = s.Store.SaveTGSession(ctx, sess)
		return TGSessionStatus{}, err
	}
	sess.LastError = ""
	sess.PasswordNeeded = false
	sess, err = s.Store.SaveTGSession(ctx, sess)
	return tgSessionStatus(sess), err
}

func (s *Service) VerifyTGSession(ctx context.Context, code string) (TGSessionStatus, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return TGSessionStatus{}, ErrBadRequest("code is required")
	}
	sess, err := s.Store.TGSession(ctx)
	if err != nil {
		return TGSessionStatus{}, err
	}
	if sess.CodeHash == "" {
		return TGSessionStatus{}, ErrBadRequest("请先发送登录验证码")
	}
	err = s.withTGClient(ctx, sess, func(ctx context.Context, client *telegram.Client) error {
		_, err := client.Auth().SignIn(ctx, sess.Phone, code, sess.CodeHash)
		if errors.Is(err, tgauth.ErrPasswordAuthNeeded) {
			sess.PasswordNeeded = true
			return nil
		}
		if err != nil {
			return err
		}
		sess.Authorized = true
		sess.PasswordNeeded = false
		sess.CodeHash = ""
		return nil
	})
	if err != nil {
		sess.LastError = err.Error()
		_, _ = s.Store.SaveTGSession(ctx, sess)
		return TGSessionStatus{}, err
	}
	sess.LastError = ""
	sess, err = s.Store.SaveTGSession(ctx, sess)
	return tgSessionStatus(sess), err
}

func (s *Service) TGSessionPassword(ctx context.Context, password string) (TGSessionStatus, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return TGSessionStatus{}, ErrBadRequest("password is required")
	}
	sess, err := s.Store.TGSession(ctx)
	if err != nil {
		return TGSessionStatus{}, err
	}
	err = s.withTGClient(ctx, sess, func(ctx context.Context, client *telegram.Client) error {
		if _, err := client.Auth().Password(ctx, password); err != nil {
			return err
		}
		sess.Authorized = true
		sess.PasswordNeeded = false
		sess.CodeHash = ""
		return nil
	})
	if err != nil {
		sess.LastError = err.Error()
		_, _ = s.Store.SaveTGSession(ctx, sess)
		return TGSessionStatus{}, err
	}
	sess.LastError = ""
	sess, err = s.Store.SaveTGSession(ctx, sess)
	return tgSessionStatus(sess), err
}

func (s *Service) SaveTGChannel(ctx context.Context, id string, in domain.TGChannel) (domain.TGChannel, error) {
	ch := domain.TGChannel{
		DisplayName:  strings.TrimSpace(in.DisplayName),
		Identifier:   strings.TrimSpace(in.Identifier),
		Username:     strings.TrimPrefix(strings.TrimSpace(in.Username), "@"),
		PeerID:       in.PeerID,
		AccessHash:   in.AccessHash,
		Enabled:      in.Enabled,
		MessageLimit: in.MessageLimit,
		PinnedOnly:   in.PinnedOnly,
	}
	if ch.MessageLimit <= 0 {
		ch.MessageLimit = 10
	}
	if ch.MessageLimit > 100 {
		return ch, ErrBadRequest("message_limit must be <= 100")
	}
	if id == "" {
		if ch.Identifier == "" && ch.PeerID == 0 {
			return ch, ErrBadRequest("identifier is required")
		}
		if ch.PeerID == 0 {
			resolved, err := s.resolveTGChannel(ctx, ch.Identifier)
			if err != nil {
				return ch, err
			}
			ch.PeerID, ch.AccessHash, ch.Username = resolved.PeerID, resolved.AccessHash, resolved.Username
			ch.AvatarURL = resolved.AvatarURL
			if ch.DisplayName == "" {
				ch.DisplayName = resolved.DisplayName
			}
		}
		if ch.DisplayName == "" {
			ch.DisplayName = ch.Identifier
		}
		ch.Enabled = true
		return s.Store.CreateTGChannel(ctx, ch)
	}
	old, err := s.Store.TGChannel(ctx, id)
	if err != nil {
		return ch, err
	}
	if ch.DisplayName == "" {
		ch.DisplayName = old.DisplayName
	}
	if ch.Identifier == "" {
		ch.Identifier = old.Identifier
	}
	if ch.Username == "" {
		ch.Username = old.Username
	}
	if ch.PeerID == 0 {
		ch.PeerID, ch.AccessHash = old.PeerID, old.AccessHash
	}
	if ch.AvatarURL == "" {
		ch.AvatarURL = old.AvatarURL
	}
	ch.ID, ch.LastSyncAt, ch.LastError, ch.CreatedAt = old.ID, old.LastSyncAt, old.LastError, old.CreatedAt
	ch, err = s.Store.UpdateTGChannel(ctx, ch)
	if err != nil {
		return ch, err
	}
	if old.PinnedOnly != ch.PinnedOnly {
		err = s.Store.DeleteTGMessages(ctx, ch.ID)
	}
	return ch, err
}

func (s *Service) SyncTGChannels(ctx context.Context) ([]domain.TGChannel, error) {
	var out []domain.TGChannel
	err := s.withAuthorizedTG(ctx, func(ctx context.Context, api *tg.Client, client *telegram.Client) error {
		dialogs, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{OffsetPeer: &tg.InputPeerEmpty{}, Limit: 100})
		if err != nil {
			return err
		}
		mod, ok := dialogs.AsModified()
		if !ok {
			return nil
		}
		for _, chat := range mod.GetChats() {
			c, ok := chat.(*tg.Channel)
			if !ok || c.Left || c.AccessHash == 0 || (!c.Broadcast && !c.Megagroup) {
				continue
			}
			ch := domain.TGChannel{
				DisplayName:  c.Title,
				Identifier:   tgIdentifier(c),
				Username:     c.Username,
				PeerID:       c.ID,
				AccessHash:   c.AccessHash,
				AvatarURL:    s.cacheTGChannelAvatar(ctx, client, c.ID, c.AccessHash, c.Photo),
				Enabled:      true,
				MessageLimit: 10,
			}
			saved, err := s.Store.CreateTGChannel(ctx, ch)
			if err != nil {
				return err
			}
			out = append(out, saved)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DisplayName < out[j].DisplayName })
	return out, nil
}

func (s *Service) RefreshTGMessagesDue(ctx context.Context) error {
	s.mu.Lock()
	if s.tgRunning || (!s.tgLastRun.IsZero() && time.Since(s.tgLastRun) < 5*time.Minute) {
		s.mu.Unlock()
		return nil
	}
	s.tgRunning = true
	s.tgLastRun = time.Now()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.tgRunning = false
		s.mu.Unlock()
	}()
	return s.RefreshTGMessages(ctx, "")
}

func (s *Service) RefreshTGMessages(ctx context.Context, channelID string) error {
	channels, err := s.Store.ListTGChannels(ctx)
	if err != nil {
		return err
	}
	return s.withAuthorizedTG(ctx, func(ctx context.Context, api *tg.Client, client *telegram.Client) error {
		var firstErr error
		matched := false
		for _, ch := range channels {
			if !ch.Enabled || (channelID != "" && ch.ID != channelID) {
				continue
			}
			matched = true
			if err := s.refreshTGChannel(ctx, api, client, ch); err != nil {
				ch.LastError = err.Error()
				_, _ = s.Store.UpdateTGChannel(ctx, ch)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		if channelID != "" && !matched {
			return ErrBadRequest("频道不存在或未启用")
		}
		return firstErr
	})
}

func (s *Service) refreshTGChannel(ctx context.Context, api *tg.Client, client *telegram.Client, ch domain.TGChannel) error {
	limit := ch.MessageLimit
	if limit <= 0 {
		limit = 10
	}
	peer := &tg.InputPeerChannel{ChannelID: ch.PeerID, AccessHash: ch.AccessHash}
	if ch.AvatarURL == "" {
		ch.AvatarURL = s.fetchTGChannelAvatar(ctx, api, client, ch)
	}
	messages, err := tgChannelMessages(ctx, api, peer, limit, ch.PinnedOnly)
	if err != nil {
		return err
	}
	if err := s.Store.DeleteTGMessages(ctx, ch.ID); err != nil {
		return err
	}
	for _, item := range messages {
		msg, ok := item.(*tg.Message)
		if !ok {
			continue
		}
		out := domain.TGMessage{
			ChannelID:   ch.ID,
			RemoteID:    msg.ID,
			PublishedAt: time.Unix(int64(msg.Date), 0).UTC(),
			Text:        msg.Message,
			Link:        tgMessageLink(ch, msg.ID),
		}
		out.MediaType, out.MediaPath, out.MediaURL, out.MediaCached = s.cacheTGMedia(ctx, client, ch.ID, msg)
		if _, err := s.Store.SaveTGMessage(ctx, out); err != nil {
			return err
		}
	}
	ch.LastSyncAt, ch.LastError = time.Now().UTC(), ""
	_, err = s.Store.UpdateTGChannel(ctx, ch)
	return err
}

func tgChannelMessages(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, limit int, pinnedOnly bool) ([]tg.MessageClass, error) {
	if pinnedOnly {
		res, err := api.MessagesSearch(ctx, &tg.MessagesSearchRequest{Peer: peer, Q: "", Filter: &tg.InputMessagesFilterPinned{}, Limit: limit})
		if err != nil {
			return nil, err
		}
		return tgMessages(res), nil
	}
	res, err := api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, Limit: limit})
	if err != nil {
		return nil, err
	}
	return tgMessages(res), nil
}

func tgMessages(res tg.MessagesMessagesClass) []tg.MessageClass {
	if mod, ok := res.AsModified(); ok {
		return mod.GetMessages()
	}
	return nil
}

func (s *Service) withAuthorizedTG(ctx context.Context, fn func(context.Context, *tg.Client, *telegram.Client) error) error {
	sess, err := s.Store.TGSession(ctx)
	if err != nil {
		return err
	}
	if !sess.Authorized {
		return ErrBadRequest("Telegram 未登录")
	}
	return s.withTGClient(ctx, sess, func(ctx context.Context, client *telegram.Client) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !status.Authorized {
			sess.Authorized = false
			sess.LastError = "Telegram session expired"
			_, _ = s.Store.SaveTGSession(ctx, sess)
			return ErrBadRequest("Telegram 登录已失效")
		}
		return fn(ctx, client.API(), client)
	})
}

func (s *Service) withTGClient(ctx context.Context, sess domain.TGSession, fn func(context.Context, *telegram.Client) error) error {
	if sess.APIID <= 0 || strings.TrimSpace(sess.APIHash) == "" {
		return ErrBadRequest("Telegram API 配置不完整")
	}
	client := telegram.NewClient(sess.APIID, sess.APIHash, telegram.Options{
		SessionStorage: tgDBSession{store: s.Store},
		NoUpdates:      true,
		Device: telegram.DeviceConfig{
			DeviceModel:    "ai-upstream-monitor",
			AppVersion:     "1.0",
			SystemVersion:  "server",
			SystemLangCode: "zh-cn",
			LangCode:       "zh",
		},
	})
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return client.Run(runCtx, func(ctx context.Context) error { return fn(ctx, client) })
}

func (s *Service) resolveTGChannel(ctx context.Context, raw string) (domain.TGChannel, error) {
	username := tgUsername(raw)
	if username == "" {
		return domain.TGChannel{}, ErrBadRequest("只支持公开频道用户名/链接，私密频道请用同步频道选择")
	}
	var out domain.TGChannel
	err := s.withAuthorizedTG(ctx, func(ctx context.Context, api *tg.Client, client *telegram.Client) error {
		resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
		if err != nil {
			return err
		}
		for _, chat := range resolved.Chats {
			c, ok := chat.(*tg.Channel)
			if !ok {
				continue
			}
			out = domain.TGChannel{DisplayName: c.Title, Identifier: "@" + username, Username: username, PeerID: c.ID, AccessHash: c.AccessHash, AvatarURL: s.cacheTGChannelAvatar(ctx, client, c.ID, c.AccessHash, c.Photo)}
			return nil
		}
		return ErrBadRequest("未找到频道")
	})
	return out, err
}

func (s *Service) cacheTGMedia(ctx context.Context, client *telegram.Client, channelID string, msg *tg.Message) (mediaType, mediaPath, mediaURL string, cached bool) {
	loc, typ, ext := tgMediaLocation(msg)
	if loc == nil {
		return "", "", "", false
	}
	name := fmt.Sprintf("%s_%d%s", channelID, msg.ID, ext)
	if err := os.MkdirAll(s.TGMediaDir, 0o755); err != nil {
		return typ, "", "", false
	}
	path := filepath.Join(s.TGMediaDir, name)
	if _, err := os.Stat(path); err == nil {
		return typ, name, "/api/tg/media/" + name, true
	}
	if _, err := client.Download(loc).ToPath(ctx, path); err != nil {
		_ = os.Remove(path)
		return typ, "", "", false
	}
	return typ, name, "/api/tg/media/" + name, true
}

func (s *Service) fetchTGChannelAvatar(ctx context.Context, api *tg.Client, client *telegram.Client, ch domain.TGChannel) string {
	res, err := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{&tg.InputChannel{ChannelID: ch.PeerID, AccessHash: ch.AccessHash}})
	if err != nil {
		return ""
	}
	for _, item := range res.GetChats() {
		c, ok := item.(*tg.Channel)
		if ok && c.ID == ch.PeerID {
			return s.cacheTGChannelAvatar(ctx, client, c.ID, ch.AccessHash, c.Photo)
		}
	}
	return ""
}

func (s *Service) cacheTGChannelAvatar(ctx context.Context, client *telegram.Client, channelID, accessHash int64, photo tg.ChatPhotoClass) string {
	p, ok := photo.(*tg.ChatPhoto)
	if !ok || p.PhotoID == 0 || client == nil {
		return ""
	}
	name := fmt.Sprintf("avatar_%d_%d.jpg", channelID, p.PhotoID)
	if err := os.MkdirAll(s.TGMediaDir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(s.TGMediaDir, name)
	if _, err := os.Stat(path); err == nil {
		return "/api/tg/media/" + name
	}
	loc := &tg.InputPeerPhotoFileLocation{Big: false, Peer: &tg.InputPeerChannel{ChannelID: channelID, AccessHash: accessHash}, PhotoID: p.PhotoID}
	if _, err := client.Download(loc).ToPath(ctx, path); err != nil {
		_ = os.Remove(path)
		return ""
	}
	return "/api/tg/media/" + name
}

func tgMediaLocation(msg *tg.Message) (tg.InputFileLocationClass, string, string) {
	switch m := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		p, ok := m.Photo.(*tg.Photo)
		if !ok {
			return nil, "photo", ".jpg"
		}
		size := largestPhotoSize(p.Sizes)
		if size == "" {
			return nil, "photo", ".jpg"
		}
		return &tg.InputPhotoFileLocation{ID: p.ID, AccessHash: p.AccessHash, FileReference: p.FileReference, ThumbSize: size}, "photo", ".jpg"
	case *tg.MessageMediaDocument:
		d, ok := m.Document.(*tg.Document)
		if !ok {
			return nil, "document", ".bin"
		}
		typ, ext := tgDocumentType(d, m)
		thumb := largestPhotoSize(d.Thumbs)
		if thumb != "" {
			return &tg.InputDocumentFileLocation{ID: d.ID, AccessHash: d.AccessHash, FileReference: d.FileReference, ThumbSize: thumb}, typ, ".jpg"
		}
		if d.Size > 20<<20 {
			return nil, typ, ext
		}
		return &tg.InputDocumentFileLocation{ID: d.ID, AccessHash: d.AccessHash, FileReference: d.FileReference}, typ, ext
	default:
		return nil, "", ""
	}
}

func largestPhotoSize(sizes []tg.PhotoSizeClass) string {
	bestType := ""
	bestSize := -1
	for _, item := range sizes {
		switch s := item.(type) {
		case *tg.PhotoSize:
			score := s.W * s.H
			if score > bestSize {
				bestType, bestSize = s.Type, score
			}
		case *tg.PhotoSizeProgressive:
			score := s.W * s.H
			if score > bestSize {
				bestType, bestSize = s.Type, score
			}
		}
	}
	return bestType
}

func tgDocumentType(d *tg.Document, m *tg.MessageMediaDocument) (string, string) {
	mime := strings.ToLower(d.MimeType)
	switch {
	case m.Video || strings.HasPrefix(mime, "video/"):
		return "video", ".mp4"
	case m.Voice || strings.HasPrefix(mime, "audio/"):
		return "audio", ".ogg"
	case strings.HasPrefix(mime, "image/"):
		return "image", ".jpg"
	default:
		return "document", ".bin"
	}
}

func tgSessionStatus(sess domain.TGSession) TGSessionStatus {
	return TGSessionStatus{
		Configured:     sess.APIID > 0 && sess.APIHash != "" && sess.Phone != "",
		Authorized:     sess.Authorized,
		Phone:          sess.Phone,
		APIID:          sess.APIID,
		PasswordNeeded: sess.PasswordNeeded,
		LastError:      sess.LastError,
	}
}

func tgUsername(raw string) string {
	v := strings.TrimSpace(raw)
	v = strings.TrimPrefix(v, "https://")
	v = strings.TrimPrefix(v, "http://")
	v = strings.TrimPrefix(v, "t.me/")
	v = strings.TrimPrefix(v, "telegram.me/")
	v = strings.TrimPrefix(v, "@")
	v = strings.Trim(v, "/")
	if strings.Contains(v, "/") || strings.HasPrefix(v, "+") {
		return ""
	}
	return v
}

func tgIdentifier(c *tg.Channel) string {
	if c.Username != "" {
		return "@" + c.Username
	}
	return strconv.FormatInt(c.ID, 10)
}

func tgMessageLink(ch domain.TGChannel, id int) string {
	if ch.Username != "" {
		return fmt.Sprintf("https://t.me/%s/%d", ch.Username, id)
	}
	return fmt.Sprintf("https://t.me/c/%d/%d", ch.PeerID, id)
}

type tgDBSession struct {
	store *store.Store
}

func (s tgDBSession) LoadSession(ctx context.Context) ([]byte, error) {
	sess, err := s.store.TGSession(ctx)
	if errors.Is(err, sql.ErrNoRows) || len(sess.SessionBlob) == 0 {
		return nil, session.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return sess.SessionBlob, nil
}

func (s tgDBSession) StoreSession(ctx context.Context, data []byte) error {
	return s.store.StoreTGSessionBlob(ctx, data)
}
