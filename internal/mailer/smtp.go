package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Mailer interface {
	SendVerificationCode(ctx context.Context, recipient, code string, expiresAt time.Time) error
}

type SMTPMailer struct {
	Host        string
	Port        int
	Username    string
	Password    string
	From        string
	FromName    string
	TLSMode     string
	Timeout     time.Duration
	TLSConfig   *tls.Config
	DialContext func(context.Context, string, string) (net.Conn, error)
}

func (m SMTPMailer) SendVerificationCode(ctx context.Context, recipient, code string, expiresAt time.Time) error {
	recipient = strings.TrimSpace(recipient)
	if _, err := mail.ParseAddress(recipient); err != nil {
		return errors.New("收件邮箱格式无效")
	}
	if len(code) != 6 || strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return errors.New("验证码格式无效")
	}
	fromAddress, err := mail.ParseAddress(strings.TrimSpace(m.From))
	if err != nil || fromAddress.Address == "" {
		return errors.New("SMTP 发件人格式无效")
	}
	if strings.ContainsAny(m.FromName, "\r\n") {
		return errors.New("SMTP 发件人名称格式无效")
	}
	host := strings.TrimSpace(m.Host)
	if host == "" || m.Port < 1 || m.Port > 65535 || strings.TrimSpace(m.Username) == "" || m.Password == "" {
		return errors.New("SMTP 配置不完整")
	}
	mode := strings.ToLower(strings.TrimSpace(m.TLSMode))
	if mode == "implicit-tls" || mode == "tls" || mode == "implicit" {
		mode = "implicit_tls"
	}
	if mode == "" {
		mode = "starttls"
	}
	if mode != "implicit_tls" && mode != "starttls" {
		return errors.New("SMTP TLS 模式无效")
	}
	address := net.JoinHostPort(host, strconv.Itoa(m.Port))
	deadline := time.Now().Add(m.timeout())
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	dialer := &net.Dialer{Timeout: m.timeout()}
	var conn net.Conn
	if mode == "implicit_tls" {
		if m.DialContext != nil {
			conn, err = m.DialContext(ctx, "tcp", address)
			if err == nil {
				config := m.tlsConfig(host)
				tlsConn := tls.Client(conn, config)
				err = tlsConn.HandshakeContext(ctx)
				if err == nil {
					conn = tlsConn
				}
			}
		} else {
			conn, err = tls.DialWithDialer(dialer, "tcp", address, m.tlsConfig(host))
		}
	} else {
		if m.DialContext != nil {
			conn, err = m.DialContext(ctx, "tcp", address)
		} else {
			conn, err = dialer.DialContext(ctx, "tcp", address)
		}
	}
	if err != nil {
		return errors.New("SMTP 连接失败")
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return errors.New("SMTP 握手失败")
	}
	defer client.Close()
	if mode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP 服务器不支持 STARTTLS")
		}
		if err := client.StartTLS(m.tlsConfig(host)); err != nil {
			return errors.New("SMTP STARTTLS 失败")
		}
	}
	if auth := smtp.PlainAuth("", m.Username, m.Password, host); auth != nil {
		if err := client.Auth(auth); err != nil {
			return errors.New("SMTP 鉴权失败")
		}
	}
	if err := client.Mail(fromAddress.Address); err != nil {
		return errors.New("SMTP 发件人被拒绝")
	}
	if err := client.Rcpt(recipient); err != nil {
		return errors.New("SMTP 收件人被拒绝")
	}
	writer, err := client.Data()
	if err != nil {
		return errors.New("SMTP 开始发送失败")
	}
	message := buildMessage(fromAddress.Address, m.FromName, recipient, code, expiresAt)
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return errors.New("SMTP 写入邮件失败")
	}
	if err := writer.Close(); err != nil {
		return errors.New("SMTP 完成发送失败")
	}
	if err := client.Quit(); err != nil {
		return errors.New("SMTP 结束会话失败")
	}
	return nil
}

func (m SMTPMailer) tlsConfig(host string) *tls.Config {
	if m.TLSConfig != nil {
		config := m.TLSConfig.Clone()
		if config.ServerName == "" {
			config.ServerName = host
		}
		if config.MinVersion == 0 {
			config.MinVersion = tls.VersionTLS12
		}
		return config
	}
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
}

func (m SMTPMailer) timeout() time.Duration {
	if m.Timeout > 0 && m.Timeout <= 15*time.Second {
		return m.Timeout
	}
	return 15 * time.Second
}

func buildMessage(from, fromName, recipient, code string, expiresAt time.Time) string {
	displayName := strings.TrimSpace(fromName)
	fromHeader := from
	if displayName != "" {
		fromHeader = mime.QEncoding.Encode("UTF-8", displayName) + " <" + from + ">"
	}
	expires := expiresAt.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("15:04")
	return fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\nQQ 状态机器人绑定验证码：%s\n验证码有效期至 %s（北京时间）。如果不是本人操作，请忽略此邮件。\r\n", fromHeader, recipient, mime.QEncoding.Encode("UTF-8", "账号绑定验证码"), code, expires)
}
