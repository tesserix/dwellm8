package mail

import (
	"context"
	"strings"
	"testing"
)

func TestMessageIsAWellFormedRFC5322(t *testing.T) {
	t.Parallel()
	raw := string(compose(Message{
		From: "Dwellm8 <no-reply@dwellm8.com>", To: "manager@example.test",
		Subject: "Your Dwellm8 code", Body: "123456 is your code.\n",
	}))

	for _, want := range []string{
		"From: Dwellm8 <no-reply@dwellm8.com>\r\n",
		"To: manager@example.test\r\n",
		"Subject: Your Dwellm8 code\r\n",
		"MIME-Version: 1.0\r\n",
		"\r\n\r\n",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("message is missing %q\n%s", want, raw)
		}
	}
}

func TestHeadersCannotBeInjectedThroughASubject(t *testing.T) {
	t.Parallel()
	// The subject is composed from data. A newline in it would let anything
	// reaching that field add a Bcc.
	raw := string(compose(Message{
		From: "a@b.test", To: "c@d.test",
		Subject: "hello\r\nBcc: attacker@evil.test", Body: "x",
	}))
	for _, line := range strings.Split(raw, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") {
			t.Fatalf("a header was injected:\n%s", raw)
		}
	}
	if !strings.Contains(raw, "Subject: hello  Bcc: attacker@evil.test\r\n") {
		t.Fatalf("the newline must be flattened into the subject, not dropped:\n%s", raw)
	}
}

func TestAnUnconfiguredSenderSaysSoRatherThanSwallowingTheMail(t *testing.T) {
	t.Parallel()
	// Silently dropping is the failure that looks like a working sign-up until
	// somebody waits ten minutes for a code that was never sent.
	s := New(SMTP{})
	err := s.Send(context.Background(), Message{To: "a@b.test", Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("an unconfigured mailer must refuse, not pretend")
	}
	if !strings.Contains(err.Error(), "no mail") {
		t.Fatalf("the refusal must name the cause, got %v", err)
	}
}

func TestConfiguredReadsTheCredentialsThemselves(t *testing.T) {
	t.Parallel()
	if (SMTP{}).Configured() {
		t.Error("nothing configured is not configured")
	}
	if (SMTP{Host: "smtp.example.test"}).Configured() {
		t.Error("a host with no sender address cannot send")
	}
	full := SMTP{Host: "smtp.example.test", Port: 587, Username: "u", Password: "p", From: "no-reply@dwellm8.com"}
	if !full.Configured() {
		t.Error("a complete set is configured")
	}
}
