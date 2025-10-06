package mail

import (
	"bufio"
	"context"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"
)

// fakeSMTP is a minimal SMTP server that records one message.
func fakeSMTP(t *testing.T) (addr string, got <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	out := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		say := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
		say("220 fake ready")
		var data strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250 fake")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				data.WriteString(strings.TrimSpace(line) + "\n")
				say("250 ok")
			case cmd == "DATA":
				say("354 go on")
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					data.WriteString(l)
				}
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				out <- data.String()
				return
			default:
				say("250 ok")
			}
		}
	}()
	return ln.Addr().String(), out
}

func TestSendDeliversAnAmharicMessage(t *testing.T) {
	addr, got := fakeSMTP(t)
	s, err := NewSMTP(addr, "Addis Hiwot <no-reply@addishiwot.test>")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), Message{To: "kidist@example.com", Subject: "የማረጋገጫ ኮድዎ", Text: "ሰላም ክዲስት፣\nኮድዎ 04217 ነው።"}); err != nil {
		t.Fatal(err)
	}
	raw := <-got
	if !strings.Contains(raw, "MAIL FROM:<no-reply@addishiwot.test>") || !strings.Contains(raw, "RCPT TO:<kidist@example.com>") {
		t.Errorf("envelope:\n%s", raw)
	}
	// Decode what arrived the way a mail client would.
	msg, err := mail.ReadMessage(strings.NewReader(raw[strings.Index(raw, "From:"):]))
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subject != "የማረጋገጫ ኮድዎ" {
		t.Errorf("subject decodes to %q", subject)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if !strings.Contains(string(body), "ኮድዎ 04217 ነው።") {
		t.Errorf("body decodes to %q", body)
	}
	if msg.Header.Get("Message-ID") == "" || msg.Header.Get("Date") == "" {
		t.Error("Message-ID and Date are required headers")
	}
}

func TestBuildRejectsHeaderInjection(t *testing.T) {
	from := mail.Address{Name: "Addis Hiwot", Address: "no-reply@addishiwot.test"}
	to := mail.Address{Address: "kidist@example.com"}
	if _, err := build(from, to, "Hi\r\nBcc: victim@example.com", "x", time.Now()); !errors.Is(err, errHeaderInjection) {
		t.Errorf("err = %v, want errHeaderInjection", err)
	}
}

func TestSendRejectsABadRecipient(t *testing.T) {
	s, _ := NewSMTP("127.0.0.1:1", "no-reply@addishiwot.test")
	if err := s.Send(context.Background(), Message{To: "not an address", Subject: "x", Text: "y"}); err == nil {
		t.Error("a bad recipient was accepted")
	}
}

func TestNewSMTPValidatesSettings(t *testing.T) {
	if _, err := NewSMTP("mailpit", "no-reply@addishiwot.test"); err == nil {
		t.Error("an address without a port was accepted")
	}
	if _, err := NewSMTP("mailpit:1025", "nobody"); err == nil {
		t.Error("a sender that is not an email address was accepted")
	}
}

func TestSendGivesUpWhenTheServerIsDown(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	s, _ := NewSMTP(addr, "no-reply@addishiwot.test")
	if err := s.Send(context.Background(), Message{To: "k@example.com", Subject: "x", Text: "y"}); err == nil {
		t.Error("sending to a closed port succeeded")
	}
}
