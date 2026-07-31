package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"qq-status-bot/internal/domain"
)

type diskState struct {
	Settings         domain.Settings   `json:"settings"`
	AdminUsername    string            `json:"admin_username"`
	AdminPassHash    string            `json:"admin_password_hash"`
	EventLogs        []domain.EventLog `json:"event_logs"`
	AlertState       domain.AlertState `json:"alert_state"`
	DiscoveredGroups []string          `json:"discovered_groups"`
}

type session struct {
	Username string
	Expires  time.Time
}

type Store struct {
	path     string
	mu       sync.RWMutex
	state    diskState
	sessions map[string]session
}

func Open(path string, defaults domain.Settings) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("配置存储路径为空")
	}
	s := &Store{path: path, sessions: make(map[string]session), state: diskState{Settings: defaults}}
	if s.state.Settings.AlertFailureSamples <= 0 {
		s.state.Settings.AlertFailureSamples = 2
	}
	if s.state.Settings.AlertRecoverySamples <= 0 {
		s.state.Settings.AlertRecoverySamples = 2
	}
	if s.state.Settings.AlertGroups == nil {
		s.state.Settings.AlertGroups = []string{}
	}
	s.state.AlertState.Nodes = map[string]domain.AlertNodeState{}
	s.state.DiscoveredGroups = []string{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return nil, errors.New("配置存储文件格式无效")
	}
	s.state.Settings = s.state.Settings.MergeUpdate(defaults)
	if s.state.Settings.AlertFailureSamples <= 0 {
		s.state.Settings.AlertFailureSamples = 2
	}
	if s.state.Settings.AlertRecoverySamples <= 0 {
		s.state.Settings.AlertRecoverySamples = 2
	}
	if s.state.Settings.AlertGroups == nil {
		s.state.Settings.AlertGroups = []string{}
	}
	if s.state.AlertState.Nodes == nil {
		s.state.AlertState.Nodes = map[string]domain.AlertNodeState{}
	}
	s.state.DiscoveredGroups = mergeDiscoveredGroups(s.state.DiscoveredGroups, s.state.EventLogs)
	return s, nil
}

func (s *Store) Close() error { return nil }

func (s *Store) Settings() domain.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Settings
}

func (s *Store) UpdateSettings(next domain.Settings) (domain.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Settings = next.MergeUpdate(s.state.Settings)
	if err := s.persistLocked(); err != nil {
		return domain.Settings{}, err
	}
	return s.state.Settings, nil
}

func (s *Store) Setup(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	if len(password) < 8 {
		return errors.New("管理员密码至少 8 位")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.AdminPassHash != "" {
		return errors.New("管理员已初始化")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.state.AdminUsername = username
	s.state.AdminPassHash = string(hash)
	return s.persistLocked()
}

func (s *Store) SetupStatus() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.AdminPassHash != ""
}

func (s *Store) Login(username, password string) (string, error) {
	s.mu.RLock()
	storedUsername, storedHash := s.state.AdminUsername, s.state.AdminPassHash
	s.mu.RUnlock()
	username = strings.TrimSpace(username)
	if username == "" {
		username = storedUsername
	}
	if storedHash == "" || username != storedUsername || bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)) != nil {
		return "", errors.New("账号或密码错误")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tokenBytes)
	s.mu.Lock()
	s.sessions[token] = session{Username: username, Expires: time.Now().Add(24 * time.Hour)}
	s.mu.Unlock()
	return token, nil
}

func (s *Store) Authenticated(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(item.Expires) {
		delete(s.sessions, token)
		return false
	}
	return true
}

func (s *Store) Logout(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func (s *Store) AppendLog(item domain.EventLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(item.ID) == 0 {
		idBytes := make([]byte, 8)
		_, _ = rand.Read(idBytes)
		item.ID = hex.EncodeToString(idBytes)
	}
	if item.CreatedAt == "" {
		item.CreatedAt = time.Now().Format(time.RFC3339)
	}
	if item.Direction == "receive" && strings.TrimSpace(item.GroupOpenID) != "" {
		s.state.DiscoveredGroups = appendGroup(s.state.DiscoveredGroups, item.GroupOpenID)
	}
	s.state.EventLogs = append([]domain.EventLog{item}, s.state.EventLogs...)
	if len(s.state.EventLogs) > 500 {
		s.state.EventLogs = s.state.EventLogs[:500]
	}
	return s.persistLocked()
}

func (s *Store) DiscoveredGroups() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string{}, s.state.DiscoveredGroups...)
}

func mergeDiscoveredGroups(existing []string, logs []domain.EventLog) []string {
	groups := append([]string{}, existing...)
	for _, item := range logs {
		if item.Direction == "receive" {
			groups = appendGroup(groups, item.GroupOpenID)
		}
	}
	return groups
}

func appendGroup(groups []string, group string) []string {
	group = strings.TrimSpace(group)
	if group == "" {
		return groups
	}
	for _, existing := range groups {
		if existing == group {
			return groups
		}
	}
	return append(groups, group)
}

func (s *Store) Logs(limit int) []domain.EventLog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.state.EventLogs) {
		limit = len(s.state.EventLogs)
	}
	out := make([]domain.EventLog, limit)
	copy(out, s.state.EventLogs[:limit])
	return out
}

func (s *Store) AlertState() domain.AlertState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneAlertState(s.state.AlertState)
}

func (s *Store) UpdateAlertState(next domain.AlertState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if next.Nodes == nil {
		next.Nodes = map[string]domain.AlertNodeState{}
	}
	s.state.AlertState = cloneAlertState(next)
	return s.persistLocked()
}

func cloneAlertState(in domain.AlertState) domain.AlertState {
	out := in
	out.Nodes = make(map[string]domain.AlertNodeState, len(in.Nodes))
	for key, node := range in.Nodes {
		copyNode := node
		copyNode.OfflineAttempts = cloneIntMap(node.OfflineAttempts)
		copyNode.RecoveryAttempts = cloneIntMap(node.RecoveryAttempts)
		copyNode.NotifiedGroups = append([]string{}, node.NotifiedGroups...)
		out.Nodes[key] = copyNode
	}
	return out
}

func cloneIntMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".qq-status-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}
