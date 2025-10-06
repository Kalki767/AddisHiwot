// Package mail sends plain-text emails over SMTP.
//
// In development the SMTP server is Mailpit, which keeps every message in a web
// inbox and never delivers it. Messages are UTF-8 (Amharic included), with the
// subject encoded as a MIME encoded-word and the body as quoted-printable.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Message is one email.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Sender sends emails.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SMTP sends through an SMTP server without authentication or TLS, which is what
// Mailpit expects; production settings are added before deployment.
type SMTP struct {
	addr string // host:port
	from mail.Address
	now  func() time.Time
}

// NewSMTP returns a sender for the server at addr. from is a full address such
// as "Addis Hiwot <no-reply@addishiwot.test>".
func NewSMTP(addr, from string) (*SMTP, error) {
	f, err := mail.ParseAddress(from)
	if err != nil {
		return nil, fmt.Errorf("mail: MAIL_FROM %q is not an email address", from)
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return nil, fmt.Errorf("mail: SMTP_ADDR %q must be host:port", addr)
	}
	return &SMTP{addr: addr, from: *f, now: time.Now}, nil
}

// Send delivers m, giving up after 10 seconds or when ctx ends.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return fmt.Errorf("mail: bad recipient: %w", err)
	}
	body, err := build(s.from, *to, m.Subject, m.Text, s.now())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("mail: connect: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	host, _, _ := net.SplitHostPort(s.addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("mail: hello: %w", err)
	}
	defer c.Close()
	if err := c.Mail(s.from.Address); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("mail: RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("mail: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: send: %w", err)
	}
	return c.Quit()
}

// errHeaderInjection guards against a line break smuggling extra headers (such
// as Bcc) into the message.
var errHeaderInjection = errors.New("mail: line break in a header value")

// build returns the full RFC 5322 message.
func build(from, to mail.Address, subject, text string, now time.Time) ([]byte, error) {
	if strings.ContainsAny(subject+from.Name+to.Name, "\r\n") {
		return nil, errHeaderInjection
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	domain := from.Address[strings.LastIndexByte(from.Address, '@')+1:]

	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+hex.EncodeToString(id)+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", `text/plain; charset="utf-8"`)
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(strings.ReplaceAll(text, "\n", "\r\n"))); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
