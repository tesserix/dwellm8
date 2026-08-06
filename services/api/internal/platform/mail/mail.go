// Package mail sends the few emails this API originates itself — today, the
// one-time code that proves a manager's address before a firm is minted (#282).
//
// Everything transactional and high-volume belongs to the notify module and its
// provider; this is deliberately the smallest thing that can put a code in an
// inbox, over SMTP, with no template engine and no queue.
package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/smtp"
	"strings"
	"time"
)

// SMTP is the relay to hand the message to. Read from the environment, which
// reads it from Secret Manager — never from a file in the image.
type SMTP struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// Configured reads the credentials themselves rather than a flag somebody has
// to remember to set alongside them.
func (c SMTP) Configured() bool {
	return c.Host != "" && c.Port != 0 && c.From != ""
}

// Message is one email.
type Message struct {
	From    string
	To      string
	Subject string
	Body    string
}

// Sender is what callers depend on, so a handler can be tested without a relay.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

type sender struct{ cfg SMTP }

func New(cfg SMTP) Sender { return &sender{cfg: cfg} }

var errUnconfigured = errors.New("mail: no mail relay is configured")

func (s *sender) Send(ctx context.Context, m Message) error {
	if !s.cfg.Configured() {
		// Refusing is the point. A mailer that silently drops looks like a
		// working sign-up until somebody waits ten minutes for a code.
		return errUnconfigured
	}
	if m.From == "" {
		m.From = s.cfg.From
	}

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)

	done := make(chan error, 1)
	go func() {
		done <- smtp.SendMail(addr, auth, s.cfg.From, []string{m.To}, compose(m))
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("mail: sending to %s: %w", m.To, err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// compose builds the RFC 5322 message. Header values are stripped of CR and LF
// first: a newline reaching a header field is how a Bcc gets added by whoever
// controls the data it was built from.
func compose(m Message) []byte {
	var b bytes.Buffer
	h := func(name, value string) {
		fmt.Fprintf(&b, "%s: %s\r\n", name, header(value))
	}
	h("From", m.From)
	h("To", m.To)
	h("Subject", m.Subject)
	h("Date", time.Now().Format(time.RFC1123Z))
	h("MIME-Version", "1.0")
	h("Content-Type", "text/plain; charset=UTF-8")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(m.Body, "\n", "\r\n"))
	return b.Bytes()
}

func header(v string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(v)
}
