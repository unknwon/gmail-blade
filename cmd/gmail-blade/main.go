package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"slices"
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
		Usage: "A GMail sidecar for advanced filtering",
		Commands: []*cli.Command{
			{
				Name:  "once",
				Usage: "Run once to process emails",
				Flags: commonFlags,
				Action: func(c *cli.Context) error {
					if c.Bool("errors-only") && c.Bool("debug") {
						return errors.New("cannot use both --errors-only and --debug flags")
					}
					if c.Bool("debug") {
						log.SetLevel(log.DebugLevel)
					} else if c.Bool("errors-only") {
						log.SetLevel(log.ErrorLevel)
					}

					config, err := parseConfig(c.String("config"))
					if err != nil {
						return errors.Wrap(err, "parse config")
					}

					return runOnce(c.Context, c.Bool("dry-run"), config, make(map[imap.UID]struct{}))
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
					if c.Bool("debug") {
						log.SetLevel(log.DebugLevel)
					} else if c.Bool("errors-only") {
						log.SetLevel(log.ErrorLevel)
					}

					config, err := parseConfig(c.String("config"))
					if err != nil {
						return errors.Wrap(err, "parse config")
					}

					return runServer(c.Bool("dry-run"), config)
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
					if c.Bool("debug") {
						log.SetLevel(log.DebugLevel)
					}

					config, err := parseConfig(c.String("config"))
					if err != nil {
						return errors.Wrap(err, "parse config")
					}
					return runListMailboxes(config)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal("Failed to run application", "error", err)
	}
}

var (
	labelRegexp = regexp.MustCompile(`label "([^"]*)"`)
)

func runOnce(ctx context.Context, dryRun bool, config *config, processedUIDs map[imap.UID]struct{}) error {
	client, closeClient, err := getAuthenticatedClient(config.Credentials, nil)
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
			log.Info("No more unread messages found")
			break
		}

		for _, msg := range messages {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if _, ok := processedUIDs[msg.UID]; ok {
				log.Debug("Skipped processed message", "uid", msg.UID)
				continue
			}

			err = processMessage(dryRun, client, msg, config.Filters)
			if err != nil {
				// We need to continue processing other messages even if one fails
				log.Error("Failed to process message", "uid", msg.UID, "error", err)
			} else {
				processedUIDs[msg.UID] = struct{}{}
			}
		}
	}
	return nil
}

func processMessage(dryRun bool, client *imapclient.Client, msg *imapclient.FetchMessageBuffer, filters []configFilter) error {
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

	log.Debug(
		"Unread message",
		"uid", msg.UID,
		"from", from,
		"fromName", fromName,
		"subject", msg.Envelope.Subject,
		"cc", cc,
		"to", to,
	)

	var body string
	for _, b := range msg.BodySection {
		body += string(b.Bytes)
	}

	var actions []string
	for _, f := range filters {
		result, err := expr.Run(
			f.CompiledCondition,
			map[string]any{
				"message": map[string]any{
					"from":     from,
					"fromName": fromName,
					"subject":  msg.Envelope.Subject,
					"cc":       cc,
					"to":       to,
					"body":     body,
				},
			})
		if err != nil {
			log.Error("Failed to run expression", "error", err)
		}
		if fmt.Sprintf("%v", result) == "true" {
			actions = append(actions, f.Action)
			if f.HaltOnMatch {
				log.Debug("Halt on match", "uid", msg.UID, "filter", f.Name)
				break
			}
		}
	}

	if len(actions) == 0 {
		log.Debug(
			"No actions matched",
			"uid", msg.UID,
			"subject", msg.Envelope.Subject,
		)
		return nil
	}

	log.Info(
		"Actions matched",
		"uid", msg.UID,
		"subject", msg.Envelope.Subject,
		"actions", actions,
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
			label := labelRegexp.FindStringSubmatch(action)
			if len(label) < 2 {
				return errors.Errorf("invalid label action format for action %q", action)
			}
			labelName := label[1]

			uidSet := imap.UIDSetNum()
			uidSet.AddNum(msg.UID)
			_, err := client.Copy(uidSet, labelName).Wait()
			if err != nil {
				return errors.Wrapf(err, "copy email to label %q", labelName)
			}
		} else {
			log.Warn("Unknown action", "action", action)
		}
	}
	return nil
}

func runServer(dryRun bool, config *config) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a channel to listen for interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info("Received SIGTERM, shutting down")
		cancel()
	}()
	log.Info("Server started (press Ctrl+C to stop)")

	processedUIDs := make(map[imap.UID]struct{})

serverRoutine:
	for {
		err := runOnce(ctx, dryRun, config, processedUIDs)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Error("Failed to process messages", "error", err)
		}

		select {
		case <-ctx.Done():
			break serverRoutine
		case <-time.After(30 * time.Second):
		}
	}

	log.Info("Server stopped")
	return nil
}

func runListMailboxes(config *config) error {
	client, closeClient, err := getAuthenticatedClient(config.Credentials, nil)
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
	log.Info("Found mailboxes", "mailboxes", strings.Join(mailboxes, "\n"))
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
