package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"time"

	"github.com/nexxserve/nexxnotify/internal/config"
)

type SMTP struct {
	cfg config.SMTPConfig
}

func NewSMTP(cfg config.SMTPConfig) *SMTP {
	return &SMTP{cfg: cfg}
}

func (p *SMTP) Name() string { return "smtp" }

func (p *SMTP) Channels() []ChannelStatus {
	settings := map[string]string{}
	if p.cfg.Host != "" {
		settings = map[string]string{
			"host":     p.cfg.Host,
			"port":     fmt.Sprintf("%d", p.cfg.Port),
			"username": p.cfg.Username,
			"password": Mask(p.cfg.Password),
			"from":     p.cfg.From,
		}
	}
	return []ChannelStatus{
		{Channel: Email, Configured: p.cfg.Host != "", Settings: settings},
	}
}

// SendEmail sends an email via SMTP. Supports both plain text and HTML bodies.
// If htmlBody is non-empty, a multipart/alternative message is sent with
// both text and HTML parts. Otherwise, only the text body is sent.
func (p *SMTP) SendEmail(ctx context.Context, msg EmailMessage) (SendResult, error) {
	if p.cfg.Host == "" {
		return SendResult{}, fmt.Errorf("smtp: host not configured")
	}

	from := p.cfg.From
	if from == "" {
		from = msg.From
	}
	if from == "" {
		from = p.cfg.Username
	}

	// Build MIME message
	var buf bytes.Buffer

	// Headers
	buf.WriteString("From: " + from + "\r\n")
	buf.WriteString("To: " + msg.To + "\r\n")
	buf.WriteString("Subject: " + msg.Subject + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")

	textBody := msg.TextBody
	htmlBody := msg.HTMLBody

	if htmlBody != "" && textBody != "" {
		// Multipart alternative: text + HTML
		boundary := fmt.Sprintf("_boundary_%d", time.Now().UnixNano())
		buf.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n")
		buf.WriteString("\r\n")

		// Text part
		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(textBody)
		buf.WriteString("\r\n")

		// HTML part
		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(htmlBody)
		buf.WriteString("\r\n")

		buf.WriteString("--" + boundary + "--\r\n")
	} else if htmlBody != "" {
		// HTML only
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(htmlBody)
	} else {
		// Plain text only
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(textBody)
	}

	// Connect to SMTP server
	addr := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)

	// Use TLS if port is 465 (implicit TLS) or STARTTLS for 587/25
	var conn net.Conn
	var err error

	 dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	dialer := &net.Dialer{Timeout: 10 * time.Second}

	if p.cfg.Port == 465 {
		// Implicit TLS (port 465)
		conn, err = tlsDial(dialer, dialCtx, "tcp", addr)
	} else {
		// Plain connection, STARTTLS later
		conn, err = dialer.DialContext(dialCtx, "tcp", addr)
	}
	if err != nil {
		return SendResult{}, fmt.Errorf("smtp: connect: %w", err)
	}

	client, err := smtp.NewClient(conn, p.cfg.Host)
	if err != nil {
		conn.Close()
		return SendResult{}, fmt.Errorf("smtp: new client: %w", err)
	}
	defer client.Close()

	// STARTTLS for non-465 ports
	if p.cfg.Port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: p.cfg.Host}); err != nil {
				return SendResult{}, fmt.Errorf("smtp: starttls: %w", err)
			}
		}
	}

	// Authenticate
	if p.cfg.Username != "" && p.cfg.Password != "" {
		auth := smtp.PlainAuth("", p.cfg.Username, p.cfg.Password, p.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return SendResult{}, fmt.Errorf("smtp: auth: %w", err)
		}
	}

	// Set sender
	if err := client.Mail(from); err != nil {
		return SendResult{}, fmt.Errorf("smtp: mail from: %w", err)
	}

	// Set recipient
	if err := client.Rcpt(msg.To); err != nil {
		return SendResult{}, fmt.Errorf("smtp: rcpt to: %w", err)
	}

	// Send body
	w, err := client.Data()
	if err != nil {
		return SendResult{}, fmt.Errorf("smtp: data: %w", err)
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return SendResult{}, fmt.Errorf("smtp: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return SendResult{}, fmt.Errorf("smtp: close data: %w", err)
	}

	return SendResult{OK: true, Message: "sent via smtp"}, nil
}

// tlsDial establishes a TLS connection directly (for port 465).
func tlsDial(dialer *net.Dialer, ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	host, _, _ := net.SplitHostPort(addr)
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: host,
	})

	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}

	return tlsConn, nil
}
