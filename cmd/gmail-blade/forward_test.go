package main

import (
	"net/mail"
	"strings"
	"testing"
)

func TestParseForwardAction(t *testing.T) {
	const wantRecipient = "ops@example.com"

	recipient, err := parseForwardAction(`forward "ops@example.com"`)
	if err != nil {
		t.Fatalf("parseForwardAction() error = %v", err)
	}
	if recipient != wantRecipient {
		t.Fatalf("parseForwardAction() recipient = %q, want %q", recipient, wantRecipient)
	}

	if _, err = parseForwardAction(`forward "not-an-email"`); err == nil {
		t.Fatal("parseForwardAction() error = nil, want non-nil for invalid address")
	}

	if _, err = parseForwardAction("forward \"ops@example.com\r\nBcc: hidden@example.com\""); err == nil {
		t.Fatal("parseForwardAction() error = nil, want non-nil for injected headers")
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
		"Subject: Hello Bcc: hidden@example.com\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"\r\nline1\r\nline2",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("buildForwardSMTPMessage() = %q, want substring %q", message, want)
		}
	}

	if strings.Contains(message, "\r\nBcc: hidden@example.com") {
		t.Fatalf("buildForwardSMTPMessage() = %q, want injected header removed", message)
	}
	if strings.Contains(message, "Subject: Hello\r\nBcc: hidden@example.com") {
		t.Fatalf("buildForwardSMTPMessage() = %q, want sanitized subject header", message)
	}
}

func TestSanitizeHeaderValue(t *testing.T) {
	tests := map[string]string{
		"":             "",
		"hello":        "hello",
		"hello\r":      "hello",
		"hello\n":      "hello",
		"he\rllo\ngo":  "he llo go",
		"\r\nsubject":  "subject",
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
