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
	if recipient != "ops@example.com" {
		t.Fatalf("parseForwardAction returned %q", recipient)
	}

	if _, err = parseForwardAction(`forward "not-an-email"`); err == nil {
		t.Fatal("parseForwardAction should reject invalid addresses")
	}

	if _, err = parseForwardAction("forward \"ops@example.com\r\nBcc: hidden@example.com\""); err == nil {
		t.Fatal("parseForwardAction should reject header injection in recipient addresses")
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
	subject := "Hello\r\nBcc: hidden@example.com"
	message := buildForwardSMTPMessage(
		&mail.Address{Address: "me@example.com"},
		"ops@example.com",
		subject,
		"line1\nline2",
	)

	for _, want := range []string{
		"From: <me@example.com>\r\n",
		"To: ops@example.com\r\n",
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

func TestForwardSubject(t *testing.T) {
	tests := map[string]string{
		"":                 "Fwd:",
		"Hello":            "Fwd: Hello",
		"Fwd: Hello":       "Fwd: Hello",
		"fWd: Hello":       "fWd: Hello",
		"Hello\r\nWorld":   "Fwd: Hello World",
		"Hello\rWorld\nGo": "Fwd: HelloWorld Go",
	}

	for input, want := range tests {
		if got := forwardSubject(input); got != want {
			t.Fatalf("forwardSubject(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSanitizeHeaderValue(t *testing.T) {
	tests := map[string]string{
		"":             "",
		"hello":        "hello",
		"hello\r":      "hello",
		"hello\n":      "hello ",
		"he\rllo\ngo":  "hello go",
		"\r\nsubject":  " subject",
		"line1\r\nx\r": "line1 x",
	}

	for input, want := range tests {
		if got := sanitizeHeaderValue(input); got != want {
			t.Fatalf("sanitizeHeaderValue(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeCRLF(t *testing.T) {
	input := "line1\r\nline2\rline3\nline4"
	want := "line1\r\nline2\r\nline3\r\nline4"

	if got := normalizeCRLF(input); got != want {
		t.Fatalf("normalizeCRLF(%q) = %q, want %q", input, got, want)
	}
}
