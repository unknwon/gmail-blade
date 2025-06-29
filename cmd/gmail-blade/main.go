package main

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/expr-lang/expr"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "gmail-blade",
		Usage: "A GMail sidecar for advanced filtering",
		Commands: []*cli.Command{
			{
				Name:  "once",
				Usage: "Run once to process emails",
				Flags: []cli.Flag{
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
				},
				Action: func(c *cli.Context) error {
					if c.Bool("debug") {
						log.SetLevel(log.DebugLevel)
					}

					config, err := parseConfig(c.String("config"))
					if err != nil {
						return errors.Wrap(err, "parse config")
					}

					dryRun := c.Bool("dry-run")
					return filterEmails(config, dryRun)
				},
			},
			{
				Name:  "server",
				Usage: "Run in server mode",
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
				Action: func(cCtx *cli.Context) error {
					fmt.Println("Server mode not implemented yet")
					return nil
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
					return listMailboxes(config)
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

func filterEmails(config *config, dryRun bool) error {
	client, err := imapclient.DialTLS("imap.gmail.com:993", nil)
	if err != nil {
		return errors.Wrap(err, "dial IMAP server")
	}
	defer func() { _ = client.Close() }()

	if err = client.Login(config.Credentials.Username, config.Credentials.Password).Wait(); err != nil {
		return errors.Wrap(err, "login to IMAP server")
	}

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
			// Skip seen emails
			if slices.Contains(msg.Flags, imap.FlagSeen) {
				continue
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

			log.Debug(
				"Unread message",
				"uid", msg.UID,
				"from", from,
				"fromName", fromName,
				"subject", msg.Envelope.Subject,
				"cc", cc,
			)

			var body string
			for _, b := range msg.BodySection {
				body += string(b.Bytes)
			}

			var actions []string
			for _, f := range config.Filters {
				result, err := expr.Run(
					f.CompiledCondition,
					map[string]any{
						"message": map[string]any{
							"from":     from,
							"fromName": fromName,
							"subject":  msg.Envelope.Subject,
							"cc":       cc,
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

			if len(actions) > 0 {
				log.Info(
					"Actions matched",
					"uid", msg.UID,
					"subject", msg.Envelope.Subject,
					"actions", actions,
				)

				if !dryRun {
					for _, action := range actions {
						if action == "delete" {
							uidSet := imap.UIDSetNum()
							uidSet.AddNum(msg.UID)
							_, err := client.Move(uidSet, "[Gmail]/Trash").Wait()
							if err != nil {
								log.Error("Failed to move email to trash", "uid", msg.UID, "error", err)
							}
						} else if strings.HasPrefix(action, "label ") {
							label := labelRegexp.FindStringSubmatch(action)
							if len(label) < 2 {
								log.Error("Invalid label action", "uid", msg.UID, "action", action)
								continue
							}
							labelName := label[1]

							uidSet := imap.UIDSetNum()
							uidSet.AddNum(msg.UID)
							_, err := client.Copy(uidSet, labelName).Wait()
							if err != nil {
								log.Error("Failed to label email", "uid", msg.UID, "label", labelName, "error", err)
							}
						} else {
							log.Warn("Unknown action", "action", action)
						}
					}
				}
			}
		}
	}

	if err := client.Logout().Wait(); err != nil {
		return errors.Wrap(err, "logout from IMAP server")
	}
	return nil
}

func listMailboxes(config *config) error {
	client, err := imapclient.DialTLS("imap.gmail.com:993", nil)
	if err != nil {
		return errors.Wrap(err, "dial IMAP server")
	}
	defer func() { _ = client.Close() }()

	if err = client.Login(config.Credentials.Username, config.Credentials.Password).Wait(); err != nil {
		return errors.Wrap(err, "login to IMAP server")
	}

	mailboxList, err := client.List("", "*", nil).Collect()
	if err != nil {
		return errors.Wrap(err, "list mailboxes")
	}
	mailboxes := make([]string, len(mailboxList))
	for i, mailbox := range mailboxList {
		mailboxes[i] = mailbox.Mailbox
	}
	log.Info("Found mailboxes", "mailboxes", strings.Join(mailboxes, "\n"))

	if err := client.Logout().Wait(); err != nil {
		return errors.Wrap(err, "logout from IMAP server")
	}
	return nil
}
