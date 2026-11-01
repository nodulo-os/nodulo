package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

type RegistrationNotifier interface {
	SendDecision(ctx context.Context, recipient, name, status, reason string) error
}

type SMTPRegistrationNotifier struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
	AppURL   string
}

func NewSMTPRegistrationNotifier(host, port, username, password, from, appURL string) *SMTPRegistrationNotifier {
	if port == "" {
		port = "587"
	}
	return &SMTPRegistrationNotifier{Host: host, Port: port, Username: username, Password: password, From: from, AppURL: strings.TrimRight(appURL, "/")}
}

func (n *SMTPRegistrationNotifier) Configured() bool {
	if n == nil || n.Host == "" || n.From == "" {
		return false
	}
	port, err := strconv.Atoi(n.Port)
	if err != nil || port < 1 || port > 65535 {
		return false
	}
	from, err := mail.ParseAddress(n.From)
	return err == nil && from.Address != ""
}

func (n *SMTPRegistrationNotifier) SendDecision(ctx context.Context, recipient, name, status, reason string) error {
	if !n.Configured() {
		return fmt.Errorf("SMTP notifications are not configured")
	}
	to, err := mail.ParseAddress(recipient)
	if err != nil {
		return fmt.Errorf("invalid notification recipient")
	}
	from, err := mail.ParseAddress(n.From)
	if err != nil {
		return fmt.Errorf("invalid SMTP sender address")
	}
	if status != "approved" && status != "rejected" {
		return fmt.Errorf("invalid registration decision")
	}

	subject, plainText, htmlBody, err := renderRegistrationEmail(name, status, reason, n.AppURL)
	if err != nil {
		return fmt.Errorf("render registration decision email: %w", err)
	}
	message, err := buildRegistrationEmail(from, to, subject, plainText, htmlBody)
	if err != nil {
		return fmt.Errorf("build registration decision email: %w", err)
	}

	address := net.JoinHostPort(n.Host, n.Port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer connection.Close()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-finished:
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = connection.SetDeadline(deadline)

	port, _ := strconv.Atoi(n.Port)
	smtpConnection := net.Conn(connection)
	implicitTLS := port == 465 || port == 2465
	if implicitTLS {
		tlsConnection := tls.Client(connection, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: n.Host})
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("start implicit SMTP TLS: %w", err)
		}
		smtpConnection = tlsConnection
	}

	client, err := smtp.NewClient(smtpConnection, n.Host)
	if err != nil {
		return fmt.Errorf("initialize SMTP client: %w", err)
	}
	defer client.Close()
	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP server does not offer STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: n.Host}); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if n.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", n.Username, n.Password, n.Host)); err != nil {
			return fmt.Errorf("authenticate to SMTP server: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("begin SMTP message: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("send SMTP message: %w", err)
	}
	return client.Quit()
}

func buildRegistrationEmail(from, to *mail.Address, subject, plainText, htmlBody string) ([]byte, error) {
	var body bytes.Buffer
	parts := multipart.NewWriter(&body)
	plainHeader := make(textproto.MIMEHeader)
	plainHeader.Set("Content-Type", "text/plain; charset=UTF-8")
	plainHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	plainPart, err := parts.CreatePart(plainHeader)
	if err != nil {
		return nil, err
	}
	plainEncoder := quotedprintable.NewWriter(plainPart)
	if _, err := io.WriteString(plainEncoder, plainText); err != nil {
		return nil, err
	}
	if err := plainEncoder.Close(); err != nil {
		return nil, err
	}

	htmlHeader := make(textproto.MIMEHeader)
	htmlHeader.Set("Content-Type", "text/html; charset=UTF-8")
	htmlHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	htmlPart, err := parts.CreatePart(htmlHeader)
	if err != nil {
		return nil, err
	}
	htmlEncoder := quotedprintable.NewWriter(htmlPart)
	if _, err := io.WriteString(htmlEncoder, htmlBody); err != nil {
		return nil, err
	}
	if err := htmlEncoder.Close(); err != nil {
		return nil, err
	}
	if err := parts.Close(); err != nil {
		return nil, err
	}

	fromHeader := mail.Address{Name: "NÓDULO", Address: from.Address}
	contentType := mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": parts.Boundary()})
	message := []byte("From: " + fromHeader.String() + "\r\n" +
		"To: " + to.String() + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: " + contentType + "\r\n\r\n" + body.String())
	return message, nil
}

func safeMailText(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}
