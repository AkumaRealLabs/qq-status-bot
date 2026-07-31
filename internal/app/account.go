package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"qq-status-bot/internal/domain"
	"qq-status-bot/internal/ggapi"
	"qq-status-bot/internal/mailer"
)

var sixDigits = regexp.MustCompile(`^[0-9]{6}$`)

type AccountVerifier interface {
	VerifyEmail(context.Context, string) (ggapi.User, error)
	GetUser(context.Context, string) (ggapi.User, error)
	Balance(context.Context, ggapi.User) (ggapi.Balance, error)
}

type AccountBindingStore interface {
	AccountBindings() []domain.AccountBinding
	AccountBinding(string) (domain.AccountBinding, bool)
	UpsertAccountBinding(domain.AccountBinding) error
	DeleteAccountBinding(string) (bool, error)
	DeleteAccountBindingForMember(string) (bool, error)
}

type accountKey struct {
	group  string
	member string
}

type pendingAccount struct {
	Email     string
	Masked    string
	Digest    [32]byte
	ExpiresAt time.Time
	LastSent  time.Time
	Attempts  int
}

type AccountService struct {
	settings SettingsStore
	store    AccountBindingStore
	verify   AccountVerifier
	mailer   mailer.Mailer

	mu      sync.Mutex
	pending map[accountKey]pendingAccount
	history map[string][]time.Time
	now     func() time.Time
}

var (
	ErrAccountNotConfigured   = errors.New("账号绑定功能未启用")
	ErrAccountBindingNotFound = errors.New("账号绑定不存在")
	ErrInvalidTestRecipient   = errors.New("测试收件邮箱格式无效")
)

func NewAccountService(settings SettingsStore, store AccountBindingStore, verify AccountVerifier, sender mailer.Mailer) *AccountService {
	return &AccountService{settings: settings, store: store, verify: verify, mailer: sender,
		pending: make(map[accountKey]pendingAccount), history: make(map[string][]time.Time), now: time.Now}
}

func (a *AccountService) Configure(verify AccountVerifier, sender mailer.Mailer) {
	a.mu.Lock()
	a.verify, a.mailer = verify, sender
	a.mu.Unlock()
}

func (a *AccountService) HasPending(group, member string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.pending[accountKey{group: strings.TrimSpace(group), member: strings.TrimSpace(member)}]
	return ok
}

func (a *AccountService) Configured() bool {
	if a == nil || a.store == nil || a.settings == nil || !a.settings.Settings().GGAPIBalanceEnabled {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.verify != nil && a.mailer != nil
}

func (a *AccountService) dependencies() (AccountVerifier, mailer.Mailer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.verify, a.mailer
}

func (a *AccountService) logAction(key accountKey, eventType, status string) {
	if a == nil || a.settings == nil {
		return
	}
	_ = a.settings.AppendLog(domain.EventLog{Direction: "receive", EventType: eventType,
		GroupOpenID: key.group, Status: status})
}

// Handle 处理一条已经确认来自 GROUP_AT_MESSAGE_CREATE 的成员消息。
// 返回 handled=false 表示它不是账号命令或流程输入，应交给其他业务处理。
func (a *AccountService) Handle(ctx context.Context, message domain.GroupMessage) (bool, string) {
	if a == nil || strings.TrimSpace(message.Author.MemberOpenID) == "" {
		return false, ""
	}
	content := domain.NormalizeContent(message.Content)
	key := accountKey{group: strings.TrimSpace(message.GroupOpenID), member: strings.TrimSpace(message.Author.MemberOpenID)}
	a.mu.Lock()
	pending, hasPending := a.pending[key]
	expiredPending := false
	if hasPending && a.now().After(pending.ExpiresAt) {
		delete(a.pending, key)
		hasPending = false
		expiredPending = true
	}
	a.mu.Unlock()
	if !hasPending && !expiredPending && !domain.IsAccountCommand(content) {
		return false, ""
	}
	if content == domain.CommandHelp {
		return true, accountHelp()
	}
	if content == domain.CommandCancel {
		return true, a.cancel(key)
	}
	if expiredPending && content == domain.CommandResend {
		return true, "绑定流程已过期，无法重发验证码。重新绑定示例：@机器人 绑定"
	}
	if !a.Configured() {
		hint := domain.CommandBind
		if content == domain.CommandBalance {
			hint = domain.CommandBalance
		}
		return true, "管理员尚未启用 GGAPI 余额功能。配置完成后重试示例：@机器人 " + hint
	}
	switch content {
	case domain.CommandBind:
		return true, a.startBinding(key)
	case domain.CommandResend:
		return true, a.resend(ctx, key)
	}
	if hasPending {
		return true, a.pendingInput(ctx, key, pending, content)
	}
	if expiredPending && content != domain.CommandCancel && content != domain.CommandBind && content != domain.CommandResend {
		return true, "验证码已过期，本次流程已失效。重新绑定示例：@机器人 绑定"
	}
	switch content {
	case domain.CommandBalance:
		return true, a.queryBalance(ctx, key.member)
	case domain.CommandUnbind:
		return true, a.unbind(key.member)
	}
	return false, ""
}

func accountHelp() string {
	return "可用命令示例：@机器人 状态；绑定示例：@机器人 绑定；余额示例：@机器人 余额；解绑示例：@机器人 解绑；绑定流程控制示例：@机器人 取消、@机器人 重发"
}

// TestEmail 发送一次性验证码样式邮件，不创建绑定流程。
func (a *AccountService) TestEmail(ctx context.Context, recipient string) error {
	if !validEmail(recipient) {
		return ErrInvalidTestRecipient
	}
	_, sender := a.dependencies()
	if sender == nil {
		return ErrAccountNotConfigured
	}
	code, err := randomCode()
	if err != nil {
		a.logAction(accountKey{}, "ACCOUNT_SMTP_TEST", "failed")
		return errors.New("测试邮件发送失败")
	}
	if err := sender.SendVerificationCode(ctx, recipient, code, a.now().Add(10*time.Minute)); err != nil {
		a.logAction(accountKey{}, "ACCOUNT_SMTP_TEST", "failed")
		return err
	}
	a.logAction(accountKey{}, "ACCOUNT_SMTP_TEST", "sent")
	return nil
}

func (a *AccountService) startBinding(key accountKey) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending[key] = pendingAccount{ExpiresAt: a.now().Add(10 * time.Minute)}
	a.logAction(key, "ACCOUNT_BIND_START", "started")
	if binding, ok := a.store.AccountBinding(key.member); ok {
		return fmt.Sprintf("当前已绑定 %s。发送新邮箱可在验证成功后替换旧绑定，旧绑定此前不会受影响。示例：@机器人 name@example.com", domain.MaskEmail(binding.Email))
	}
	return "请发送要绑定的邮箱（邮箱会出现在群聊中）。示例：@机器人 name@example.com"
}

func (a *AccountService) sendCodeForEmail(ctx context.Context, key accountKey, content string) string {
	if !validEmail(content) {
		a.logAction(key, "ACCOUNT_CODE_SEND", "invalid")
		return "邮箱格式不正确，请发送有效邮箱。示例：@机器人 name@example.com"
	}
	if !a.rateAllowed(key.member, content) {
		a.logAction(key, "ACCOUNT_CODE_SEND", "rate_limited")
		return "验证码发送次数已达到每小时上限，请稍后再试。取消流程示例：@机器人 取消"
	}
	code, err := randomCode()
	if err != nil {
		a.logAction(key, "ACCOUNT_CODE_SEND", "failed")
		return "验证码发送失败，旧绑定未受影响。稍后重试示例：@机器人 name@example.com"
	}
	expires := a.now().Add(10 * time.Minute)
	_, sender := a.dependencies()
	if sender == nil || sender.SendVerificationCode(ctx, content, code, expires) != nil {
		a.logAction(key, "ACCOUNT_CODE_SEND", "failed")
		return "验证码发送失败，旧绑定未受影响。稍后重试示例：@机器人 name@example.com"
	}
	digest := sha256.Sum256([]byte(code))
	a.mu.Lock()
	a.pending[key] = pendingAccount{Email: normalizeEmail(content), Masked: domain.MaskEmail(content), Digest: digest, ExpiresAt: expires, LastSent: a.now()}
	a.recordSendLocked(key.member, content)
	a.mu.Unlock()
	a.logAction(key, "ACCOUNT_CODE_SEND", "sent")
	return fmt.Sprintf("验证码已发送至 %s，有效期 10 分钟。示例：@机器人 123456；重发：@机器人 重发；取消：@机器人 取消", domain.MaskEmail(content))
}

func (a *AccountService) resend(ctx context.Context, key accountKey) string {
	a.mu.Lock()
	pending, ok := a.pending[key]
	if !ok {
		a.logAction(key, "ACCOUNT_CODE_RESEND", "missing")
		a.mu.Unlock()
		return "当前没有待验证的绑定流程。重新开始示例：@机器人 绑定"
	}
	if pending.Email == "" {
		a.logAction(key, "ACCOUNT_CODE_RESEND", "await_email")
		a.mu.Unlock()
		return "请先发送邮箱，再使用重发。示例：@机器人 name@example.com"
	}
	now := a.now()
	if now.After(pending.ExpiresAt) {
		a.logAction(key, "ACCOUNT_CODE_RESEND", "expired")
		delete(a.pending, key)
		a.mu.Unlock()
		return "绑定流程已失效。重新绑定示例：@机器人 绑定"
	}
	if wait := 60*time.Second - now.Sub(pending.LastSent); wait > 0 {
		a.logAction(key, "ACCOUNT_CODE_RESEND", "rate_limited")
		a.mu.Unlock()
		return fmt.Sprintf("请在 %d 秒后再重发验证码。输入示例：@机器人 123456", int(wait/time.Second)+1)
	}
	if !a.rateAllowedLocked(key.member, pending.Email, now) {
		a.logAction(key, "ACCOUNT_CODE_RESEND", "rate_limited")
		a.mu.Unlock()
		return "验证码发送次数已达到每小时上限，请稍后再试。取消流程示例：@机器人 取消"
	}
	a.mu.Unlock()
	code, err := randomCode()
	if err != nil {
		a.logAction(key, "ACCOUNT_CODE_RESEND", "failed")
		return "验证码发送失败，当前流程仍有效。稍后重试示例：@机器人 重发"
	}
	expires := a.now().Add(10 * time.Minute)
	_, sender := a.dependencies()
	if sender == nil || sender.SendVerificationCode(ctx, pending.Email, code, expires) != nil {
		a.logAction(key, "ACCOUNT_CODE_RESEND", "failed")
		return "验证码发送失败，当前流程仍有效。稍后重试示例：@机器人 重发"
	}
	digest := sha256.Sum256([]byte(code))
	a.mu.Lock()
	if current, exists := a.pending[key]; exists && current.Email == pending.Email {
		current.Digest, current.ExpiresAt, current.LastSent = digest, expires, a.now()
		a.pending[key] = current
		a.recordSendLocked(key.member, pending.Email)
	}
	a.mu.Unlock()
	a.logAction(key, "ACCOUNT_CODE_RESEND", "sent")
	return "新的验证码已发送，有效期 10 分钟。示例：@机器人 123456"
}

func (a *AccountService) pendingInputCode(ctx context.Context, key accountKey, pending pendingAccount, content string) string {
	if !sixDigits.MatchString(content) {
		a.logAction(key, "ACCOUNT_CODE_VERIFY", "invalid")
		return "验证码应为六位数字。示例：@机器人 123456；重发：@机器人 重发；取消：@机器人 取消"
	}
	digest := sha256.Sum256([]byte(content))
	a.mu.Lock()
	current, ok := a.pending[key]
	if !ok || current.Email != pending.Email || current.Digest != pending.Digest {
		a.logAction(key, "ACCOUNT_CODE_VERIFY", "expired")
		a.mu.Unlock()
		return "绑定流程已失效。重新绑定示例：@机器人 绑定"
	}
	if !hmac.Equal(current.Digest[:], digest[:]) {
		current.Attempts++
		left := 5 - current.Attempts
		if left <= 0 {
			a.logAction(key, "ACCOUNT_CODE_VERIFY", "failed")
			delete(a.pending, key)
			a.mu.Unlock()
			return "验证码错误次数过多，绑定流程已取消，旧绑定未受影响。重新绑定示例：@机器人 绑定"
		}
		a.pending[key] = current
		a.mu.Unlock()
		a.logAction(key, "ACCOUNT_CODE_VERIFY", "wrong")
		return fmt.Sprintf("验证码错误，还可尝试 %d 次。示例：@机器人 123456", left)
	}
	delete(a.pending, key)
	a.mu.Unlock()

	verifier, _ := a.dependencies()
	if verifier == nil {
		return "账号功能暂时不可用，旧绑定未受影响。稍后重试示例：@机器人 绑定"
	}
	user, err := verifier.VerifyEmail(ctx, pending.Email)
	if err != nil {
		a.logAction(key, "ACCOUNT_BIND_RESULT", "rejected")
		return verificationFailureReply(err)
	}
	if user.ID == "" || normalizeEmail(user.Email) != normalizeEmail(pending.Email) || !enabledUser(user) {
		a.logAction(key, "ACCOUNT_BIND_RESULT", "rejected")
		return "邮箱对应的 GGAPI 账号状态或角色不符合绑定要求，旧绑定未受影响。请确认账号已启用且角色有效。重新绑定示例：@机器人 绑定"
	}
	if err := a.saveBinding(key, pending.Email, user); err != nil {
		a.logAction(key, "ACCOUNT_BIND_RESULT", "failed")
		return "绑定保存失败，旧绑定未受影响。稍后重试示例：@机器人 绑定"
	}
	a.logAction(key, "ACCOUNT_BIND_RESULT", "success")
	return fmt.Sprintf("绑定成功：%s。查询示例：@机器人 余额", domain.MaskEmail(pending.Email))
}

func verificationFailureReply(err error) string {
	switch {
	case errors.Is(err, ggapi.ErrEmailNotFound):
		return "未找到该邮箱对应的 GGAPI 账号，旧绑定未受影响。请确认邮箱后重试。重新绑定示例：@机器人 绑定"
	case errors.Is(err, ggapi.ErrEmailAmbiguous):
		return "该邮箱对应多个 GGAPI 账号，无法安全绑定，旧绑定未受影响。请使用只对应一个账号的邮箱。重新绑定示例：@机器人 绑定"
	case errors.Is(err, ggapi.ErrAccountRoleInvalid):
		return "该邮箱对应的 GGAPI 账号角色不受支持，旧绑定未受影响。请确认账号角色有效。重新绑定示例：@机器人 绑定"
	case errors.Is(err, ggapi.ErrAccountDisabled):
		return "该 GGAPI 账号未启用，旧绑定未受影响。请启用账号后重试。重新绑定示例：@机器人 绑定"
	case errors.Is(err, ggapi.ErrAccountDeleted):
		return "该 GGAPI 账号已删除，旧绑定未受影响。请使用有效的 GGAPI 邮箱。重新绑定示例：@机器人 绑定"
	default:
		return "邮箱对应的 GGAPI 账号无法验证，旧绑定未受影响。重新绑定示例：@机器人 绑定"
	}
}

func (a *AccountService) pendingInput(ctx context.Context, key accountKey, pending pendingAccount, content string) string {
	if pending.Email == "" {
		if !validEmail(content) {
			return "需要邮箱地址。示例：@机器人 name@example.com；取消：@机器人 取消"
		}
		return a.sendCodeForEmail(ctx, key, content)
	}
	if validEmail(content) {
		return a.sendCodeForEmail(ctx, key, content)
	}
	return a.pendingInputCode(ctx, key, pending, content)
}

func (a *AccountService) saveBinding(key accountKey, email string, user ggapi.User) error {
	firstGroup := key.group
	if existing, ok := a.store.AccountBinding(key.member); ok && existing.FirstGroupOpenID != "" {
		firstGroup = existing.FirstGroupOpenID
	}
	return a.store.UpsertAccountBinding(domain.AccountBinding{MemberOpenID: key.member, Email: normalizeEmail(email),
		GGAPIUserID: user.ID, Username: user.Username, FirstGroupOpenID: firstGroup, BoundAt: a.now().Format(time.RFC3339)})
}

func (a *AccountService) queryBalance(ctx context.Context, member string) string {
	binding, ok := a.store.AccountBinding(member)
	if !ok {
		a.logAction(accountKey{member: member}, "ACCOUNT_BALANCE", "missing")
		return "你还没有绑定 GGAPI 账号。绑定示例：@机器人 绑定"
	}
	verifier, _ := a.dependencies()
	if verifier == nil {
		return "余额查询暂时失败，未解除绑定。稍后重试示例：@机器人 余额"
	}
	user, err := verifier.GetUser(ctx, binding.GGAPIUserID)
	if err != nil {
		if errors.Is(err, ggapi.ErrNotFound) {
			a.logAction(accountKey{member: member}, "ACCOUNT_BALANCE", "identity_changed")
			_, _ = a.store.DeleteAccountBinding(binding.ID)
			return "绑定账号已失效，已自动解除绑定。重新绑定示例：@机器人 绑定"
		}
		a.logAction(accountKey{member: member}, "ACCOUNT_BALANCE", "failed")
		return "余额查询暂时失败，未解除绑定。稍后重试示例：@机器人 余额"
	}
	if normalizeEmail(user.Email) != normalizeEmail(binding.Email) || user.ID != binding.GGAPIUserID || user.Deleted || !enabledUser(user) {
		a.logAction(accountKey{member: member}, "ACCOUNT_BALANCE", "identity_changed")
		_, _ = a.store.DeleteAccountBinding(binding.ID)
		return "绑定账号已失效，已自动解除绑定。重新绑定示例：@机器人 绑定"
	}
	balance, err := verifier.Balance(ctx, user)
	if err != nil {
		a.logAction(accountKey{member: member}, "ACCOUNT_BALANCE", "failed")
		return "余额查询暂时失败，未解除绑定。稍后重试示例：@机器人 余额"
	}
	currency := strings.TrimSpace(balance.Currency)
	if currency == "" {
		currency = "余额"
	}
	a.logAction(accountKey{member: member}, "ACCOUNT_BALANCE", "success")
	return fmt.Sprintf("账号：%s\n当前余额：%.2f %s\n再次查询：@机器人 余额；解绑：@机器人 解绑", domain.MaskEmail(binding.Email), balance.Amount, currency)
}

func (a *AccountService) unbind(member string) string {
	binding, ok := a.store.AccountBinding(member)
	if !ok {
		a.logAction(accountKey{member: member}, "ACCOUNT_UNBIND", "missing")
		return "当前没有绑定账号。重新绑定示例：@机器人 绑定"
	}
	if _, err := a.store.DeleteAccountBinding(binding.ID); err != nil {
		a.logAction(accountKey{member: member}, "ACCOUNT_UNBIND", "failed")
		return "解绑失败，原绑定仍然保留。稍后重试示例：@机器人 解绑"
	}
	a.logAction(accountKey{member: member}, "ACCOUNT_UNBIND", "success")
	return "已解除账号绑定，GGAPI 账号本身未被修改。重新绑定示例：@机器人 绑定"
}

func (a *AccountService) cancel(key accountKey) string {
	a.mu.Lock()
	_, existed := a.pending[key]
	delete(a.pending, key)
	a.mu.Unlock()
	if !existed {
		return "当前没有待取消的绑定流程，已有绑定未受影响。重新开始示例：@机器人 绑定"
	}
	return "已取消当前绑定流程，旧绑定未受影响。重新开始示例：@机器人 绑定"
}

func (a *AccountService) Bindings() []domain.AccountBindingView {
	if a == nil || a.store == nil {
		return []domain.AccountBindingView{}
	}
	items := a.store.AccountBindings()
	sort.Slice(items, func(i, j int) bool { return items[i].BoundAt > items[j].BoundAt })
	views := make([]domain.AccountBindingView, 0, len(items))
	for _, item := range items {
		views = append(views, item.PublicView())
	}
	return views
}

func (a *AccountService) Revoke(id string) (bool, error) {
	if a == nil || a.store == nil {
		return false, errors.New("账号绑定功能未启用")
	}
	return a.store.DeleteAccountBinding(id)
}

func (a *AccountService) rateAllowed(member, email string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rateAllowedLocked(member, normalizeEmail(email), a.now())
}

func (a *AccountService) rateAllowedLocked(member, email string, now time.Time) bool {
	memberKey := "member:" + strings.TrimSpace(member)
	emailKey := "email:" + normalizeEmail(email)
	cutoff := now.Add(-time.Hour)
	for _, key := range []string{memberKey, emailKey} {
		history := a.history[key]
		kept := history[:0]
		for _, timestamp := range history {
			if timestamp.After(cutoff) {
				kept = append(kept, timestamp)
			}
		}
		a.history[key] = kept
		if len(kept) >= 5 {
			return false
		}
	}
	return true
}

func (a *AccountService) recordSendLocked(member, email string) {
	now := a.now()
	for _, key := range []string{"member:" + strings.TrimSpace(member), "email:" + normalizeEmail(email)} {
		a.history[key] = append(a.history[key], now)
	}
}

func randomCode() (string, error) {
	number, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", number.Int64()), nil
}

func validEmail(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n \t") || len(raw) > 254 {
		return false
	}
	parsed, err := mail.ParseAddress(raw)
	return err == nil && strings.EqualFold(parsed.Address, raw) && strings.Contains(raw, "@")
}

func normalizeEmail(raw string) string { return strings.ToLower(strings.TrimSpace(raw)) }

func enabledUser(user ggapi.User) bool {
	status := strings.ToLower(strings.TrimSpace(user.Status))
	if user.Deleted || (status != "1" && status != "active" && status != "enabled" && status != "normal" && status != "正常") {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(user.Role)) {
	case "1", "10", "100", "user", "normal", "普通用户", "common",
		"admin", "administrator", "root", "superadmin", "管理员":
		return true
	default:
		return false
	}
}
