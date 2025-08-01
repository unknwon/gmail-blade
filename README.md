<div align="center">
  <img width="256" height="256" src="https://github.com/user-attachments/assets/f1deb9d7-c68d-4b9d-975c-6996ae3656ff">
  <h3>Gmail Blade</h3>
</div>

## What?

Gmail Blade is a sidecar with advanced and precise filtering for your Gmail account. Utilizing the expressiveness of [`expr-lang/expr`](https://expr-lang.org/) to fully customize your Gmail experience. Make Gmail great again!

## Why?

I am an inbox-zero guy, I rely on emails for all my notifications because the nature of emails is working asynchronously. I absolutely hate red dots and I disabled all of them. Unfortunately, native Gmail filters do not support precise filtering, and works more like a "search engine" over the emails, with fuzz matches, that creates lots of false positives. The speed of the email processing directly impacts my productivity.

## How?

> [!warning]
> This project is being actively iterated on, absence of break changes is best effort.

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

server:
  # Sleep interval between processing runs (default: 15s)
  # Uses Go duration format, e.g. "30s", "2m", "1h30m".
  sleep_interval: "15s"

# Optional GitHub integration
github:
  # GitHub Personal Access Token for API access
  # Generate yours at: https://github.com/settings/tokens
  # You can also use the name of an environment variable, or leave empty to be prompted at start.
  personal_access_token: "$GITHUB_TOKEN"
  approval:
    # Once enabled, the "GitHub review" (case insensitive) action is available to the filters.
    enabled: true
    # List of allowed GitHub usernames for the approval workflow
    allowed_usernames: ["unknwon"]
    # List of allowed repository names for the approval workflow
    allowed_repositories: ["unknwon/gmail-blade"]

# Optional Slack integration
slack:
  # Slack webhook URL for incoming webhooks
  # When webhook_url is not defined, the integration is considered disabled.
  webhook_url: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
  # Log level for messages to send to Slack (empty = disabled)
  # Valid levels: debug, info, warn, error (case-insensitive)
  send_log_level: "error"

filters:
  - name: "Delete GitHub backport notifications"
    condition: |
      "notifications@github.com" in message.from and message.subject contains "] [Backport "
    actions:
      - delete
    halt-on-match: true
  - name: "Delete GitHub CI notifications"
    condition: |
      "ci_activity@noreply.github.com" in message.cc
    actions:
      - move to "[Gmail]/Trash"
    halt-on-match: true

  - name: "Label GitHub"
    condition: |
      "notifications@github.com" in message.from
    actions:
      - label "0-GitHub"
  - name: "Label GitHub mentions"
    condition: |
      "notifications@github.com" in message.from and "mention@noreply.github.com" in message.cc
    actions:
      - label "Mentioned"
  - name: "Label GitHub review requests"
    condition: |
      "notifications@github.com" in message.from and "review_requested@noreply.github.com" in message.cc
    actions:
      - label "1-Review Requested"
  - name: "Label GitHub comments"
    condition: |
      "notifications@github.com" in message.from and "author@noreply.github.com" in message.cc
    actions:
      - label "Comment"
  - name: "Label GitHub merged PRs"
    condition: |
      "notifications@github.com" in message.from and message.body contains "Merged #" and message.body contains " into main."
    actions:
      - label "2-Merged"
  - name: "Label GitHub approvals"
    condition: |
      "notifications@github.com" in message.from and message.body contains "approved this pull request."
    actions:
      - label "Approved"

  - name: "Label Sentry notifications"
    condition: |
      "noreply@md.getsentry.com" in message.from
    actions:
      - label "Sentry"
  - name: "Label Opsgenie notifications"
    condition: |
      "opsgenie@opsgenie.net" in message.from
    actions:
      - label "Opsgenie"
  - name: "Label Google Docs notifications"
    condition: |
      ("comments-noreply@docs.google.com" in message.from or "drive-shares-dm-noreply@google.com" in message.from) and count(message.fromName, # contains "Google Docs)") > 0
    actions:
      - label "Google Docs"
```

#### Prefetches

Prefetches allow you to fetch data before evaluating filter conditions and use them as objects in conditions. They are executed in the order they are defined and their data can be used by actions. Failure of prefetches will not halt the execution of the filter but result in empty values for conditions and actions.

```yaml
filters:
  - name: "Auto-approve trusted GitHub PRs"
    prefetches:
      - github pull request
    condition: |
      "notifications@github.com" in message.from and
      message.body contains "joe please stamp"
    actions:
      - github review
      - label "Auto-approved"
```

Available prefetches:

| Name | Description |
|----------|-------------|
| `github pull request` | Fetches GitHub pull request data for the notification (requires GitHub integration, case insensitive) |

#### Condition expression

Please refer to [`expr-lang/expr`](https://expr-lang.org/) for the syntax manual, available variables are as follows:

| Name      | Type      | Description       |
|-----------|-----------|-------------------|
| `message` | `Message` | The email message |
| `githubPullRequest` | `GitHubPullRequest` | Only available when prefetched with "GitHub pull request" |

Type `Message`:

| Name       | Type       | Description                                                                                |
|------------|------------|--------------------------------------------------------------------------------------------|
| `from`     | `[]string` | The list of `from` addresses, e.g. `["notifications@github.com"]`                          |
| `fromName` | `[]string` | The list of `from` names, e.g. `["Joe Chen"]`                                              |
| `subject`  | `string`   | The email subject                                                                          |
| `cc`       | `[]string` | The list of `cc` addresses, e.g. `["joe@acme.com", "review_requested@noreply.github.com"]` |
| `to`       | `[]string` | The list of `to` addresses, e.g. `["acme@noreply.github.com"]`                             |
| `replyTo`  | `[]string` | The list of `replyTo` addresses, e.g. `["joe@acme.com"]` |
| `body`     | `string`   | The email body                                                                             |

Type `GitHubPullRequest`:

| Name       | Type       | Description                                                                                |
|------------|------------|--------------------------------------------------------------------------------------------|
| `owner`     | `string` | GitHub repository owner, e.g. `"unknwon"`                          |
| `name` | `string` | GitHub repository name, e.g. `"gmail-blade"`                                              |
| `number`  | `number`   | Pull request number, e.g. `12`                                                  |
| `author`       | `string` | Pull request author username, e.g. `"unknwon"` |

If `halt-on-match` is `true`, then it will be the last action to take upon matching.

#### Actions

> [!note]
> Gmail mailboxes and labels must already exist in your Gmail settings.
> You can use `gmail-blade list-mailboxes` to get all your mailboxes and labels.

Each filter can have multiple actions that will be executed in sequence.

| Action        | Description                                                        |
|---------------|--------------------------------------------------------------------|
| `move to "X"` | Move the message to the "X" mailbox, e.g. `move to "[Gmail]/Spam"` |
| `label "X"`   | Add label "X" to the message, e.g. `label "GitHub"`                |
| `delete`      | Delete the message, shortcut for `move to "[Gmail]/Trash"`         |
| `github review` | Review GitHub pull requests (requires GitHub integration and "GitHub pull request" prefetch, case insensitive) |

Actions are defined as a list and are executed in the same order as they are defined:

```yaml
actions:
  - label "GitHub"
  - label "Processed"
```

Example of using the GitHub review action:

```yaml
- name: "Auto-approve trusted GitHub PRs"
  prefetches:
    - github pull request
  condition: |
    "notifications@github.com" in message.from and
    message.body contains "joe please stamp"
  actions:
    - github review
    - label "Auto-approved"
```

You will get marginal performance benefit if you put `halt-on-match` ones on the top.

### Execution

The sidecar _only_ looks at unread emails.

To run the sidecar once:
- Do `gmail-blade once`. To test your filters, you can dry run with `gmail-blade once --dry-run --debug`.
- It would be handy for quick testing by specifying a list of UIDs to scope down to with `gmail-blade once --uids 1234567890,1234567891`.

To run the sidecar as a long-running service:
- Do `gmail-blade server`, it pauses between runs (default 15s, configurable via `server.sleep_interval`).
- It also supports `--dry-run` and `--debug` if you want to.

Use `--help` flag to get helper information on `gmail-blade` and its subcommands.

## License

This project is under the MIT License. See the [LICENSE](LICENSE) file for the full license text.
