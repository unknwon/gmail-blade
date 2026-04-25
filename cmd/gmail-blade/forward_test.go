package main

import (
	"net/mail"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestParseForwardAction(t *testing.T) {
	recipient, err := parseForwardAction(`forward "ops@example.com"`)
	if err != nil {
		t.Fatalf("parseForwardAction returned error: %v", err)
	}
	if recipient.Address != "ops@example.com" {
		t.Fatalf("parseForwardAction returned %q", recipient.Address)
	}

	if _, err = parseForwardAction(`forward "not-an-email"`); err == nil {
		t.Fatal("parseForwardAction should reject invalid addresses")
	}
}

func TestBuildForwardBody(t *testing.T) {
	envelope := &imap.Envelope{
		From: []imap.Address{{
			Name:    "Example Sender",
			Mailbox: "sender",
			Host:    "example.com",
		}},
		ReplyTo: []imap.Address{{
			Mailbox: "reply",
			Host:    "example.com",
		}},
		To: []imap.Address{{
			Mailbox: "team",
			Host:    "example.com",
		}},
		Cc: []imap.Address{{
			Mailbox: "cc",
			Host:    "example.com",
		}},
		Subject: "Status update",
	}

	body := buildForwardBody(envelope, "hello\nworld")
	for _, want := range []string{
		"---------- Forwarded message ----------",
		`From: "Example Sender" <sender@example.com>`,
		"Reply-To: reply@example.com",
		"To: team@example.com",
		"Cc: cc@example.com",
		"Subject: Status update",
		"hello\nworld",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("buildForwardBody missing %q in %q", want, body)
		}
	}
}

func TestBuildForwardSMTPMessage(t *testing.T) {
	message, err := buildForwardSMTPMessage(
		&mail.Address{Address: "me@example.com"},
		&mail.Address{Address: "ops@example.com"},
		"Hello\r\nBcc: hidden@example.com",
		"line1\nline2",
	)
	if err != nil {
		t.Fatalf("buildForwardSMTPMessage returned error: %v", err)
	}

	for _, want := range []string{
		"From: <me@example.com>\r\n",
		"To: <ops@example.com>\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"\r\nline1\r\nline2",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("buildForwardSMTPMessage missing %q in %q", want, message)
		}
	}

	if strings.Contains(message, "\r\nBcc: hidden@example.com") {
		t.Fatalf("buildForwardSMTPMessage should sanitize injected headers: %q", message)
	}
}

func TestBuildForwardSMTPMessageRejectsInvalidRecipient(t *testing.T) {
	_, err := buildForwardSMTPMessage(
		&mail.Address{Address: "me@example.com"},
		&mail.Address{Address: "ops@example.com\r\nBcc: hidden@example.com"},
		"Hello",
		"body",
	)
	if err == nil {
		t.Fatal("buildForwardSMTPMessage should reject invalid recipient headers")
	}
}
