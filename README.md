<div align="center">
  <h3>Gmail Blade</h3>
  <a href="https://sourcegraph.com/github.com/unknwon/gmail-blade"><img src="https://img.shields.io/badge/view%20on-Sourcegraph-brightgreen.svg?style=for-the-badge&logo=sourcegraph" alt="Sourcegraph"></a>
</div>

## What?

Gmail Blade is a sidecar with advanced and precise filtering for your Gmail account. Utilizing the expressiveness of [`expr-lang/expr`](https://expr-lang.org/) to fully customize your Gmail experience. Make Gmail great again!

## Why?

I am an inbox-zero guy, I rely on emails for all my notifications because the nature of emails is working asynchronously. I absolutely hate red dots and I disabled all of them. Unfortunately, native Gmail filters do not support precise filtering, and works more like a "search engine" over the emails, with fuzz matches, that creates lots of false positives. The speed of the email processing directly impact my productivity.

Why I have waited so long? Every side project needs a kick, and [Sourcegraph Amp](https://ampcode.com/?ref=github-unknwon) did this one for me, I want to try it for a small project from scratch, and here it is.

## How?

### Installation

```zsh
go install unknwon.dev/gmail-blade/cmd/gmail-blade
```

### Configuration

A YAML configuration file is expected (default `gmail-blade.yml` in the working directory), and below is an example:

```yaml
credentials:
  # Your Gmail email address
  username: "joe@acme.com"
  # Generate yours at: https://myaccount.google.com/apppasswords
  # You can also use the name of an environment variable, or leave empty to be prompted at start.
  password: "$GMAIL_PASSWORD"

filters:
  - name: "Delete GitHub backport notifications"
    condition: |
      "notifications@github.com" in message.from and message.subject contains "] [Backport "
    action: delete
    halt-on-match: true
  - name: "Delete GitHub CI notifications"
    condition: |
      "ci_activity@noreply.github.com" in message.cc
    action: move to "[Gmail]/Trash"
    halt-on-match: true

  - name: "Label GitHub"
    condition: |
      "notifications@github.com" in message.from
    action: label "0-GitHub"
  - name: "Label GitHub mentions"
    condition: |
      "notifications@github.com" in message.from and "mention@noreply.github.com" in message.cc
    action: label "Mentioned"
  - name: "Label GitHub review requests"
    condition: |
      "notifications@github.com" in message.from and "review_requested@noreply.github.com" in message.cc
    action: label "1-Review Requested"
  - name: "Label GitHub comments"
    condition: |
      "notifications@github.com" in message.from and "author@noreply.github.com" in message.cc
    action: label "Comment"
  - name: "Label GitHub merged PRs"
    condition: |
      "notifications@github.com" in message.from and message.body contains "Merged #" and message.body contains " into main."
    action: label "2-Merged"
  - name: "Label GitHub approvals"
    condition: |
      "notifications@github.com" in message.from and message.body contains "approved this pull request."
    action: label "Approved"

  - name: "Label Sentry notifications"
    condition: |
      "noreply@md.getsentry.com" in message.from
    action: label "Sentry"
  - name: "Label Opsgenie notifications"
    condition: |
      "opsgenie@opsgenie.net" in message.from
    action: label "Opsgenie"
  - name: "Label Google Docs notifications"
    condition: |
      ("comments-noreply@docs.google.com" in message.from or "drive-shares-dm-noreply@google.com" in message.from) and count(message.fromName, # contains "Google Docs)") > 0
    action: label "Google Docs"
```

#### Condition expression

Please refer to [`expr-lang/expr`](https://expr-lang.org/) for the syntax manual, available variables are as follows:

| Name      | Type      | Description       |
|-----------|-----------|-------------------|
| `message` | `Message` | The email message |

Type `Message`:

| Name       | Type       | Description                                                                                |
|------------|------------|--------------------------------------------------------------------------------------------|
| `from`     | `[]string` | The list of `from` addresses, e.g. `["notifications@github.com"]`                          |
| `fromName` | `[]string` | The list of `from` names, e.g. `["Joe Chen"]`                                              |
| `subject`  | `string`   | The email subject                                                                          |
| `cc`       | `[]string` | The list of `cc` addresses, e.g. `["joe@acme.com", "review_requested@noreply.github.com"]` |
| `to`       | `[]string` | The list of `to` addresses, e.g. `["acme@noreply.github.com"]`                             |
| `body`     | `string`   | The email body                                                                             |

If `halt-on-match` is `true`, then it will be the last action to take upon matching.

#### Actions

>[!note]
> Gmail mailboxes and labels must already exist in your Gmail settings.
> You can use `gmail-blade list-mailboxes` to get all your mailboxes and labels.

| Action        | Description                                                        |
|---------------|--------------------------------------------------------------------|
| `move to "X"` | Move the message to the "X" mailbox, e.g. `move to "[Gmail]/Spam"` |
| `label "X"`   | Add label "X" to the message, e.g. `label "GitHub"`                |
| `delete`      | Delete the message, shortcut for `move to "[Gmail]/Trash"`         |

Actions are executed in the same order as they are defined. You will get marginal performance benefit if you put `half-on-match` ones on the top.

### Execution

The sidecar _only_ looks at unread emails.

To run the sidecar once, do `gmail-blade once`. To test your filters, you can dry run with `gmail-blade once --dry-run --debug`.

To run the sidecar as a long-running service, do `gmail-blade server`, it pauses 30s after each run. It also supports `--dry-run` and `--debug` if you want to.

Use `--help` flag to get helper information on `gmail-blade` and its subcommands.

## License

This project is under the MIT License. See the [LICENSE](LICENSE) file for the full license text.
