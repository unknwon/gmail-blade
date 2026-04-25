package main

import (
	"net/mail"
	"strings"
	"testing"
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
			t.Fatalf("buildForwardSMTPMessage missing %q in %q", want, message)
		}
	}

	if strings.Contains(message, "\r\nBcc: hidden@example.com") {
		t.Fatalf("buildForwardSMTPMessage should sanitize injected headers: %q", message)
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
