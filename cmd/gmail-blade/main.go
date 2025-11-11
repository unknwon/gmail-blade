package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/expr-lang/expr"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

func main() {
	commonFlags := []cli.Flag{
		&cli.StringFlag{
			Name:    "config",
			Aliases: []string{"c"},
			Value:   "gmail-blade.yml",
			Usage:   "Path to config file",
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Show what would be done without actually doing it",
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "Show debug output",
		},
		&cli.BoolFlag{
			Name:  "errors-only",
			Usage: "Only show errors in output",
		},
	}

	app := &cli.App{
		Name:  "gmail-blade",
		Usage: "A Gmail sidecar for advanced filtering",
		Commands: []*cli.Command{
			{
				Name:  "once",
				Usage: "Run once to process emails",
				Flags: append(
					commonFlags,
					&cli.StringFlag{
						Name:  "uids",
						Usage: "Comma-separated list of UIDs to process (if not specified, processes all unread messages)",
					},
				),
				Action: func(c *cli.Context) error {
					if c.Bool("errors-only") && c.Bool("debug") {
						return errors.New("cannot use both --errors-only and --debug flags")
					}

					var logger Logger = log.New(os.Stderr)
					if c.Bool("debug") {
						logger.SetLevel(log.DebugLevel)
					} else if c.Bool("errors-only") {
						logger.SetLevel(log.ErrorLevel)
					}

					config, err := parseConfig(c.String("config"))
					if err != nil {
						return errors.Wrap(err, "parse config")
					}

					// Wrap logger with Slack integration if configured
					if config.Slack.SendLogLevel != "" {
						sendLevel, err := log.ParseLevel(config.Slack.SendLogLevel)
						if err != nil {
							return errors.Wrapf(err, "invalid slack.send_log_level %q", config.Slack.SendLogLevel)
						}
						logger = newSlackLogger(logger, config.Slack.WebhookURL, sendLevel)
					}

					var targetUIDs map[imap.UID]struct{}
					if uidsStr := c.String("uids"); uidsStr != "" {
						targetUIDs, err = parseUIDs(uidsStr)
						if err != nil {
							return errors.Wrap(err, "parse UIDs")
						}
					}

					return runOnce(logger, c.Context, c.Bool("dry-run"), config, make(map[imap.UID]struct{}), targetUIDs)
				},
			},
			{
				Name:  "server",
				Usage: "Run in server mode",
				Flags: commonFlags,
				Action: func(c *cli.Context) error {
					if c.Bool("errors-only") && c.Bool("debug") {
						return errors.New("cannot use both --errors-only and --debug flags")
					}

					var logger Logger = log.New(os.Stderr)
					if c.Bool("debug") {
						logger.SetLevel(log.DebugLevel)
					} else if c.Bool("errors-only") {
						logger.SetLevel(log.ErrorLevel)
					}

					config, err := parseConfig(c.String("config"))
					if err != nil {
						return errors.Wrap(err, "parse config")
					}

					// Wrap logger with Slack integration if configured
					if config.Slack.SendLogLevel != "" {
						sendLevel, err := log.ParseLevel(config.Slack.SendLogLevel)
						if err != nil {
							return errors.Wrapf(err, "invalid slack.send_log_level %q", config.Slack.SendLogLevel)
						}
						logger = newSlackLogger(logger, config.Slack.WebhookURL, sendLevel)
					}

					return runServer(logger, c.Bool("dry-run"), config)
				},
			},
			{
				Name: "list-mailboxes",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "config",
						Aliases: []string{"c"},
						Value:   "gmail-blade.yml",
						Usage:   "Path to config file",
					},
					&cli.BoolFlag{
						Name:  "debug",
						Usage: "Show debug output",
					},
				},
				Action: func(c *cli.Context) error {
					logger := log.New(os.Stderr)
					if c.Bool("debug") {
						logger.SetLevel(log.DebugLevel)
					}

					config, err := parseConfig(c.String("config"))
					if err != nil {
						return errors.Wrap(err, "parse config")
					}
					return runListMailboxes(logger, config)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal("Failed to run application", "error", err)
	}
}

var (
	labelRegexp             = regexp.MustCompile(`label "([^"]*)"`)
	moveToRegexp            = regexp.MustCompile(`move to "([^"]*)"`)
	githubReviewRegexp      = regexp.MustCompile(`(?i)github\s+review`)
	githubPullRequestRegexp = regexp.MustCompile(`(?i)github\s+pull\s+request`)
)

// transientErrors is a list of error messages that are considered transient
// and should be retried with backoff.
var transientErrors = []string{
	"unexpected EOF",
	"connection reset by peer",
	"i/o timeout",
	"use of closed network connection",
	"dial IMAP server: EOF",
	"imap: NO Lookup failed",
	"imap: NO System Error (Failure)",
}

// isTransientError checks if an error message contains any of the known transient error patterns.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	for _, transientErr := range transientErrors {
		if strings.Contains(errMsg, transientErr) {
			return true
		}
	}
	return false
}

func parseUIDs(uidsStr string) (map[imap.UID]struct{}, error) {
	uids := make(map[imap.UID]struct{})
	parts := strings.Split(uidsStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		uid, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid UID %q", part)
		}
		uids[imap.UID(uid)] = struct{}{}
	}
	return uids, nil
}

func runOnce(logger Logger, ctx context.Context, dryRun bool, config *config, processedUIDs map[imap.UID]struct{}, targetUIDs map[imap.UID]struct{}) error {
	client, closeClient, err := getAuthenticatedClient(config.Credentials, &imapclient.Options{})
	if err != nil {
		return errors.Wrap(err, "get authenticated IMAP client")
	}
	defer closeClient()

	_, err = client.Select(
		"INBOX",
		&imap.SelectOptions{
			ReadOnly: true,
		},
	).Wait()
	if err != nil {
		return errors.Wrap(err, "select INBOX")
	}

	for idx := 1; ; idx += 100 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		seqSet := imap.SeqSetNum()
		seqSet.AddRange(uint32(idx), uint32(idx+100))
		fetchOptions := &imap.FetchOptions{
			Envelope: true,
			Flags:    true,
			UID:      true,
			BodySection: []*imap.FetchItemBodySection{
				{Specifier: imap.PartSpecifierText},
			},
		}

		messages, err := client.Fetch(seqSet, fetchOptions).Collect()
		if err != nil {
			return errors.Wrap(err, "fetch messages")
		}
		if len(messages) == 0 {
			logger.Debug("No more unread messages found")
			break
		}

		for _, msg := range messages {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if _, ok := processedUIDs[msg.UID]; ok {
				logger.Debug("Skipped processed message", "uid", msg.UID)
				continue
			}

			// Skip messages not in target UID list if specified
			if len(targetUIDs) > 0 {
				if _, ok := targetUIDs[msg.UID]; !ok {
					logger.Debug("Skipped message not in target UIDs", "uid", msg.UID)
					continue
				}
			}

			err = processMessage(logger, ctx, dryRun, config, client, msg)
			if err != nil {
				return errors.Wrapf(err, "uid %d", msg.UID)
			}
			processedUIDs[msg.UID] = struct{}{}
		}
	}
	return nil
}

const prefetchGitHubPullRequestKey = "githubPullRequest"

type enver interface {
	Env() map[string]any
}

func processMessage(logger Logger, ctx context.Context, dryRun bool, config *config, client *imapclient.Client, msg *imapclient.FetchMessageBuffer) error {
	// Skip read emails
	if slices.Contains(msg.Flags, imap.FlagSeen) {
		return nil
	}

	from := make([]string, 0, len(msg.Envelope.From))
	fromName := make([]string, 0, len(msg.Envelope.From))
	for _, fromAddr := range msg.Envelope.From {
		from = append(from, fmt.Sprintf("%s@%s", fromAddr.Mailbox, fromAddr.Host))
		fromName = append(fromName, fromAddr.Name)
	}

	cc := make([]string, 0, len(msg.Envelope.Cc))
	for _, ccAddr := range msg.Envelope.Cc {
		cc = append(cc, fmt.Sprintf("%s@%s", ccAddr.Mailbox, ccAddr.Host))
	}

	to := make([]string, 0, len(msg.Envelope.To))
	for _, toAddr := range msg.Envelope.To {
		to = append(to, fmt.Sprintf("%s@%s", toAddr.Mailbox, toAddr.Host))
	}

	replyTo := make([]string, 0, len(msg.Envelope.ReplyTo))
	for _, replyToAddr := range msg.Envelope.ReplyTo {
		replyTo = append(replyTo, fmt.Sprintf("%s@%s", replyToAddr.Mailbox, replyToAddr.Host))
	}

	logger.Debug(
		"Unread message",
		"uid", msg.UID,
		"from", from,
		"fromName", fromName,
		"subject", msg.Envelope.Subject,
		"cc", cc,
		"to", to,
		"replyTo", replyTo,
	)

	var body string
	for _, b := range msg.BodySection {
		body += string(b.Bytes)
	}

	prefetchData := make(map[string]enver)
	var actions []string
	for _, f := range config.Filters {
		// Execute prefetches and collect prefetch data
		for _, prefetch := range f.Prefetches {
			// Only execute prefetch for GitHub pull request notifications.
			if githubPullRequestRegexp.MatchString(prefetch) &&
				githubPullRequestURLRegex.MatchString(body) &&
				prefetchData[prefetchGitHubPullRequestKey] == nil {
				prData, err := executePrefetchGitHubPullRequest(logger, ctx, config.GitHub, body)
				if err != nil {
					logger.Error("Failed to execute GitHub pull request prefetch", "error", err)
					continue
				}
				prefetchData[prefetchGitHubPullRequestKey] = prData
			}
		}

		env := map[string]any{
			"message": map[string]any{
				"from":     from,
				"fromName": fromName,
				"subject":  msg.Envelope.Subject,
				"cc":       cc,
				"to":       to,
				"replyTo":  replyTo,
				"body":     body,
			},
		}
		for key, value := range prefetchData {
			env[key] = value.Env()
		}

		result, err := expr.Run(f.CompiledCondition, env)
		if err != nil {
			logger.Error("Failed to run expression", "error", err)
		}
		if fmt.Sprintf("%v", result) == "true" {
			actions = append(actions, f.Actions...)
			if f.HaltOnMatch {
				logger.Debug("Halt on match", "uid", msg.UID, "filter", f.Name)
				break
			}
		}
	}

	if len(actions) == 0 {
		logger.Debug(
			"No actions matched",
			"uid", msg.UID,
			"subject", msg.Envelope.Subject,
		)
		return nil
	}

	logger.Info(
		"Actions matched",
		"uid", msg.UID,
		"subject", msg.Envelope.Subject,
		"actions", strings.Join(actions, ", "),
		"dryRun", dryRun,
	)
	if dryRun {
		return nil
	}

	for _, action := range actions {
		if action == "delete" {
			uidSet := imap.UIDSetNum()
			uidSet.AddNum(msg.UID)
			_, err := client.Move(uidSet, "[Gmail]/Trash").Wait()
			if err != nil {
				return errors.Wrapf(err, "move email to trash")
			}
		} else if strings.HasPrefix(action, "label ") {
			match := labelRegexp.FindStringSubmatch(action)
			if len(match) < 2 {
				return errors.Errorf("invalid label action format %q", action)
			}
			labelName := match[1]

			uidSet := imap.UIDSetNum()
			uidSet.AddNum(msg.UID)
			_, err := client.Copy(uidSet, labelName).Wait()
			if err != nil {
				return errors.Wrapf(err, "copy email to label %q", labelName)
			}
		} else if strings.HasPrefix(action, "move to ") {
			match := moveToRegexp.FindStringSubmatch(action)
			if len(match) < 2 {
				return errors.Errorf("invalid move to action format %q", action)
			}
			mailboxName := match[1]

			uidSet := imap.UIDSetNum()
			uidSet.AddNum(msg.UID)
			_, err := client.Move(uidSet, mailboxName).Wait()
			if err != nil {
				return errors.Wrapf(err, "move email to mailbox %q", mailboxName)
			}
		} else if config.GitHub.Approval.Enabled && githubReviewRegexp.MatchString(action) {
			err := processGitHubReview(logger, ctx, config.GitHub, msg.UID, prefetchData)
			if err != nil {
				return errors.Wrap(err, "process GitHub review action")
			}
		} else {
			logger.Warn("Unknown action", "action", action)
		}
	}
	return nil
}

func runServer(logger Logger, dryRun bool, config *config) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a channel to listen for interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Debug("Received SIGTERM, shutting down")
		cancel()
	}()
	logger.Info("Server started (press Ctrl+C to stop)")

	processedUIDs := make(map[imap.UID]struct{})
	configuredSleepInternal, _ := time.ParseDuration(config.Server.SleepInterval)
	backoffTimes := 0
serverRoutine:
	for {
		err := runOnce(logger, ctx, dryRun, config, processedUIDs, nil)
		if err != nil && !errors.Is(err, context.Canceled) {
			if isTransientError(err) {
				backoffTimes++
				// Log as warning for backoff errors, but log as error every 5th time
				msg := "Failed to process messages"
				logFields := []any{"error", err, "backoffTimes", backoffTimes}
				if backoffTimes%5 == 0 {
					logger.Error(msg, logFields...)
				} else {
					logger.Warn(msg, logFields...)
				}
			} else {
				logger.Error("Failed to process messages", "error", err)
			}
		} else {
			backoffTimes = 0
		}

		sleepInterval := configuredSleepInternal * time.Duration(backoffTimes+1)
		if sleepInterval > time.Minute {
			sleepInterval = time.Minute
		}
		if sleepInterval > configuredSleepInternal {
			logger.Warn("Backing off", "interval", sleepInterval, "backoffTimes", backoffTimes)
		}
		select {
		case <-ctx.Done():
			break serverRoutine
		case <-time.After(sleepInterval):
		}
	}

	logger.Info("Server stopped")
	return nil
}

func runListMailboxes(logger Logger, config *config) error {
	client, closeClient, err := getAuthenticatedClient(config.Credentials, &imapclient.Options{})
	if err != nil {
		return errors.Wrap(err, "get authenticated IMAP client")
	}
	defer closeClient()

	mailboxList, err := client.List("", "*", nil).Collect()
	if err != nil {
		return errors.Wrap(err, "list mailboxes")
	}
	mailboxes := make([]string, len(mailboxList))
	for i, mailbox := range mailboxList {
		mailboxes[i] = mailbox.Mailbox
	}
	logger.Info("Found mailboxes", "mailboxes", strings.Join(mailboxes, "\n"))
	return nil
}

func getAuthenticatedClient(credentials configCredentials, options *imapclient.Options) (_ *imapclient.Client, close func(), _ error) {
	client, err := imapclient.DialTLS("imap.gmail.com:993", options)
	if err != nil {
		return nil, nil, errors.Wrap(err, "dial IMAP server")
	}

	if err = client.Login(credentials.Username, credentials.Password).Wait(); err != nil {
		_ = client.Close()
		return nil, nil, errors.Wrap(err, "login to IMAP server")
	}

	return client, func() { _ = client.Logout().Wait(); _ = client.Close() }, nil
}
