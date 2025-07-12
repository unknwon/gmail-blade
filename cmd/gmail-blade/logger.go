package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"
)

// Logger defines the interface for logging operations.
type Logger interface {
	Debug(msg interface{}, keyvals ...interface{})
	Info(msg interface{}, keyvals ...interface{})
	Warn(msg interface{}, keyvals ...interface{})
	Error(msg interface{}, keyvals ...interface{})
	SetLevel(level log.Level)
	GetLevel() log.Level
}

// slackLogger wraps a Logger and sends messages at or above a specified level to Slack webhook.
type slackLogger struct {
	underlying   Logger
	webhookURL   string
	sendLogLevel log.Level
}

// SlackMessage represents the payload sent to Slack webhook.
type slackMessage struct {
	Attachments []slackAttachment `json:"attachments"`
}

type slackAttachment struct {
	Color string `json:"color"`
	Text  string `json:"text"`
}

// newSlackLogger creates a new slackLogger that wraps the given logger.
func newSlackLogger(underlying Logger, webhookURL string, sendLogLevel log.Level) Logger {
	return &slackLogger{
		underlying:   underlying,
		webhookURL:   webhookURL,
		sendLogLevel: sendLogLevel,
	}
}

func (s *slackLogger) Debug(msg interface{}, keyvals ...interface{}) {
	s.underlying.Debug(msg, keyvals...)
	if s.sendLogLevel <= log.DebugLevel {
		s.sendToSlack(log.DebugLevel, "DEBUG", fmt.Sprintf("%v", msg), keyvals...)
	}
}

func (s *slackLogger) Info(msg interface{}, keyvals ...interface{}) {
	s.underlying.Info(msg, keyvals...)
	if s.sendLogLevel <= log.InfoLevel {
		s.sendToSlack(log.InfoLevel, "INFO", fmt.Sprintf("%v", msg), keyvals...)
	}
}

func (s *slackLogger) Warn(msg interface{}, keyvals ...interface{}) {
	s.underlying.Warn(msg, keyvals...)
	if s.sendLogLevel <= log.WarnLevel {
		s.sendToSlack(log.WarnLevel, "WARN", fmt.Sprintf("%v", msg), keyvals...)
	}
}

func (s *slackLogger) Error(msg interface{}, keyvals ...interface{}) {
	s.underlying.Error(msg, keyvals...)
	if s.sendLogLevel <= log.ErrorLevel {
		s.sendToSlack(log.ErrorLevel, "ERROR", fmt.Sprintf("%v", msg), keyvals...)
	}
}

func (s *slackLogger) SetLevel(level log.Level) {
	s.underlying.SetLevel(level)
}

func (s *slackLogger) GetLevel() log.Level {
	return s.underlying.GetLevel()
}

func (s *slackLogger) sendToSlack(logLevel log.Level, level, msg string, keyvals ...interface{}) {
	// Don't send to Slack if the message level is below the underlying logger's level
	if logLevel < s.underlying.GetLevel() {
		return
	}
	var kvStr string
	for i := 0; i < len(keyvals); i += 2 {
		kvStr += fmt.Sprintf("%v: %v\n", keyvals[i], keyvals[i+1])
	}

	// Map log levels to Slack colors
	colorMap := map[string]string{
		"DEBUG": "#808080", // Gray
		"INFO":  "#36a64f", // Green
		"WARN":  "#ff9500", // Orange
		"ERROR": "#ff0000", // Red
	}

	color, exists := colorMap[level]
	if !exists {
		color = "#808080" // Default to gray
	}

	slackMsg := slackMessage{
		Attachments: []slackAttachment{
			{
				Color: color,
				Text:  fmt.Sprintf("```\ngmail-blade %s: %s\n%s```", level, msg, kvStr),
			},
		},
	}

	jsonData, err := json.Marshal(slackMsg)
	if err != nil {
		s.underlying.Error("Failed to marshal Slack message", "error", err)
		return
	}

	resp, err := http.Post(s.webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		s.underlying.Error("Failed to send to Slack", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.underlying.Error("Slack webhook returned non-200 status", "status", resp.StatusCode)
	}
}
