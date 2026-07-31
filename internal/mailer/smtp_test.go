package mailer

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSMTPMailerSendsSTARTTLSChineseCode(t *testing.T) {
	listener, tlsConfig, received := smtpFixture(t, false)
	mailer := SMTPMailer{Host: "localhost", Port: 587, Username: "user", Password: "password", From: "bot@example.com", FromName: "GGAPI", TLSMode: "starttls", TLSConfig: tlsConfig, DialContext: func(context.Context, string, string) (net.Conn, error) {
		return net.Dial("tcp", listener.Addr().String())
	}}
	if err := mailer.SendVerificationCode(context.Background(), "name@example.com", "012345", time.Now().Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	message := <-received
	if !strings.Contains(message, "012345") || !strings.Contains(message, "QQ 状态机器人绑定验证码") || !strings.Contains(message, "To: name@example.com") {
		t.Fatalf("邮件内容缺少收件人、中文正文或验证码: %q", message)
	}
}

func TestSMTPMailerSendsImplicitTLS(t *testing.T) {
	listener, tlsConfig, received := smtpFixture(t, true)
	mailer := SMTPMailer{Host: "localhost", Port: 465, Username: "user", Password: "password", From: "bot@example.com", TLSMode: "implicit_tls", TLSConfig: tlsConfig, DialContext: func(context.Context, string, string) (net.Conn, error) {
		return net.Dial("tcp", listener.Addr().String())
	}}
	if err := mailer.SendVerificationCode(context.Background(), "name@example.com", "654321", time.Now().Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(<-received, "654321") {
		t.Fatal("隐式 TLS 邮件缺少验证码")
	}
}

func smtpFixture(t *testing.T, implicit bool) (net.Listener, *tls.Config, <-chan string) {
	t.Helper()
	certServer := httptest.NewTLSServer(nil)
	defer certServer.Close()
	cert := certServer.TLS.Certificates[0]
	config := &tls.Config{Certificates: []tls.Certificate{cert}, InsecureSkipVerify: true} // 测试证书
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan string, 2)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if implicit {
			conn = tls.Server(conn, config.Clone())
			if err := conn.(*tls.Conn).Handshake(); err != nil {
				return
			}
		}
		writeSMTP := func(message string) { _, _ = fmt.Fprint(conn, message) }
		writeSMTP("220 localhost\r\n")
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case strings.HasPrefix(strings.ToUpper(line), "EHLO"):
				if !implicit && strings.Contains(strings.ToUpper(line), "EHLO") {
					writeSMTP("250-localhost\r\n250-STARTTLS\r\n250 AUTH PLAIN\r\n")
				} else {
					writeSMTP("250-localhost\r\n250 AUTH PLAIN\r\n")
				}
			case strings.HasPrefix(strings.ToUpper(line), "STARTTLS"):
				writeSMTP("220 go ahead\r\n")
				tlsConn := tls.Server(conn, config.Clone())
				if err := tlsConn.Handshake(); err != nil {
					return
				}
				conn = tlsConn
				reader = bufio.NewReader(conn)
			case strings.HasPrefix(strings.ToUpper(line), "AUTH"):
				writeSMTP("235 authenticated\r\n")
			case strings.HasPrefix(strings.ToUpper(line), "MAIL"):
				writeSMTP("250 ok\r\n")
			case strings.HasPrefix(strings.ToUpper(line), "RCPT"):
				writeSMTP("250 ok\r\n")
			case strings.EqualFold(line, "DATA"):
				writeSMTP("354 end data\r\n")
				var data strings.Builder
				for {
					part, readErr := reader.ReadString('\n')
					if readErr != nil {
						return
					}
					if strings.TrimRight(part, "\r\n") == "." {
						break
					}
					data.WriteString(part)
				}
				received <- data.String()
				writeSMTP("250 queued\r\n")
			case strings.EqualFold(line, "QUIT"):
				writeSMTP("221 bye\r\n")
				return
			}
		}
	}()
	return listener, config, received
}
