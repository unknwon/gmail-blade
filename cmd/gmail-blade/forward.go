package main

import (
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"regexp"
	"strings"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/pkg/errors"
)

const (
	gmailSMTPHost = "smtp.gmail.com"
	gmailSMTPPort = "587"
)

var forwardRegexp = regexp.MustCompile(`forward "([^"]*)"`)

func isForwardAction(action string) bool {
	return action == "forward" || strings.HasPrefix(action, "forward ")
}

func parseForwardAction(action string) (string, error) {
	match := forwardRegexp.FindStringSubmatch(action)
	if len(match) < 2 {
		return "", errors.Errorf("invalid forward action format %q", action)
	}

	address, err := mail.ParseAddress(match[1])
	if err != nil {
		return "", errors.Wrapf(err, "invalid forward address %q", match[1])
	}
	return address.Address, nil
}

func processForwardAction(logger Logger, credentials configCredentials, msg *imapclient.FetchMessageBuffer, body, action string) error {
	recipient, err := parseForwardAction(action)
	if err != nil {
		return err
	}

	fromAddress, err := mail.ParseAddress(credentials.Username)
	if err != nil {
		return errors.Wrapf(err, "parse authenticated email address %q", credentials.Username)
	}

	message := buildForwardSMTPMessage(fromAddress, recipient, msg.Envelope.Subject, body)

	logger.Info("Forwarding email", "uid", msg.UID, "to", recipient)

	auth := smtp.PlainAuth("", fromAddress.Address, credentials.Password, gmailSMTPHost)
	err = smtp.SendMail(
		net.JoinHostPort(gmailSMTPHost, gmailSMTPPort),
		auth,
		fromAddress.Address,
		[]string{recipient},
		[]byte(message),
	)
	if err != nil {
		return errors.Wrapf(err, "send forwarded email to %q", recipient)
	}
	return nil
}

func buildForwardSMTPMessage(from *mail.Address, recipient, subject, body string) string {
	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "From: %s\r\n", sanitizeHeaderValue(from.String()))
	_, _ = fmt.Fprintf(&builder, "To: %s\r\n", sanitizeHeaderValue(recipient))
	_, _ = fmt.Fprintf(&builder, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", sanitizeHeaderValue(subject)))
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	builder.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	builder.WriteString("\r\n")
	builder.WriteString(normalizeCRLF(body))
	return builder.String()
}

func sanitizeHeaderValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizeCRLF(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}
