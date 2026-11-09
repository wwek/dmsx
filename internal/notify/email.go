package notify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

const (
	SecuritySTARTTLS = "STARTTLS"
	SecurityTLS      = "TLS"
	SecurityNone     = "NONE"
)

type email struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	// From is empty when the sender is the user, if that is an address.
	From string `json:"from"`
	// To is a comma-separated address list.
	To                 string `json:"to"`
	Security           string `json:"security"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
}

func (e *email) validate() error {
	if e.Host == "" || e.To == "" {
		return fmt.Errorf("host and to are required")
	}
	if _, err := mail.ParseAddressList(e.To); err != nil {
		return fmt.Errorf("to: %w", err)
	}
	if _, err := e.sender(); err != nil {
		return err
	}
	switch e.security() {
	case SecuritySTARTTLS, SecurityTLS, SecurityNone:
	default:
		return fmt.Errorf("security must be STARTTLS, TLS or NONE")
	}
	return nil
}

func (e *email) security() string {
	if e.Security == "" {
		return SecuritySTARTTLS
	}
	return strings.ToUpper(e.Security)
}

func (e *email) port() int {
	if e.Port > 0 {
		return e.Port
	}
	switch e.security() {
	case SecurityTLS:
		return 465
	case SecurityNone:
		return 25
	default:
		return 587
	}
}

func (e *email) sender() (*mail.Address, error) {
	from := e.From
	if from == "" && strings.Contains(e.User, "@") {
		from = e.User
	}
	if from == "" {
		from = "dmsx@" + e.Host
	}
	address, err := mail.ParseAddress(from)
	if err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	return address, nil
}

func (e *email) Send(ctx context.Context, msg Message) error {
	if err := e.send(ctx, msg); err != nil {
		return scrub(fmt.Errorf("email: %w", err), e.Password)
	}
	return nil
}

func (e *email) send(ctx context.Context, msg Message) error {
	from, err := e.sender()
	if err != nil {
		return err
	}
	recipients, err := mail.ParseAddressList(e.To)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(e.Host, strconv.Itoa(e.port()))
	tlsConfig := &tls.Config{ServerName: e.Host, InsecureSkipVerify: e.InsecureSkipVerify, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	_ = conn.SetDeadline(deadline)
	if e.security() == SecurityTLS {
		conn = tls.Client(conn, tlsConfig)
	}
	c, err := smtp.NewClient(conn, e.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if e.security() == SecuritySTARTTLS {
		if err := c.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if e.User != "" {
		if err := c.Auth(smtp.PlainAuth("", e.User, e.Password, e.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	to := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		if err := c.Rcpt(recipient.Address); err != nil {
			return err
		}
		to = append(to, recipient.String())
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(buildMail(from.String(), strings.Join(to, ", "), msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func buildMail(from, to string, msg Message) []byte {
	subject := strings.NewReplacer("\r", " ", "\n", " ").Replace(msg.Heading)
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(msg.body()))
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded + "\r\n")
	return []byte(b.String())
}
