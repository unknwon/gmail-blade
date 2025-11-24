package main

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/pkg/errors"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

type config struct {
	Credentials configCredentials `yaml:"credentials"`
	Server      configServer      `yaml:"server"`
	GitHub      configGitHub      `yaml:"github"`
	Slack       configSlack       `yaml:"slack"`
	Filters     []configFilter    `yaml:"filters"`
}

type configCredentials struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type configServer struct {
	SleepInterval string `yaml:"sleep_interval"`
}

type configGitHub struct {
	PersonalAccessToken string               `yaml:"personal_access_token"`
	Approval            configGitHubApproval `yaml:"approval"`
}

type configGitHubApproval struct {
	Enabled             bool     `yaml:"enabled"`
	AllowedUsernames    []string `yaml:"allowed_usernames"`
	AllowedRepositories []string `yaml:"allowed_repositories"`
}

type configSlack struct {
	WebhookURL   string `yaml:"webhook_url"`
	SendLogLevel string `yaml:"send_log_level"`
}

type configFilter struct {
	Name              string      `yaml:"name"`
	Prefetches        []string    `yaml:"prefetches"`
	Condition         string      `yaml:"condition"`
	CompiledCondition *vm.Program `yaml:"-"`
	Actions           []string    `yaml:"actions"`
	HaltOnMatch       bool        `yaml:"halt-on-match"`
}

func parseConfig(path string) (*config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "read config file")
	}

	var c config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, errors.Wrap(err, "parse config file")
	}

	c.Credentials.Password = os.ExpandEnv(c.Credentials.Password)
	if c.Credentials.Password == "" {
		fmt.Print("Password: ")
		password, err := term.ReadPassword(syscall.Stdin)
		if err != nil {
			return nil, errors.Wrap(err, "read password")
		}
		fmt.Println()
		c.Credentials.Password = string(password)
	}

	if c.Server.SleepInterval == "" {
		c.Server.SleepInterval = "15s"
	}
	if _, err := time.ParseDuration(c.Server.SleepInterval); err != nil {
		return nil, errors.Wrapf(err, "invalid server sleep interval %q", c.Server.SleepInterval)
	}

	c.GitHub.PersonalAccessToken = os.ExpandEnv(c.GitHub.PersonalAccessToken)
	c.Slack.WebhookURL = os.ExpandEnv(c.Slack.WebhookURL)

	var requireGitHubPAT bool
	if c.GitHub.Approval.Enabled {
		requireGitHubPAT = true
	} else {
	loop:
		for _, f := range c.Filters {
			for _, prefetch := range f.Prefetches {
				if githubPullRequestRegexp.MatchString(prefetch) {
					requireGitHubPAT = true
					break loop
				}
			}
		}
	}

	if requireGitHubPAT && c.GitHub.PersonalAccessToken == "" {
		fmt.Print("GitHub Personal Access Token: ")
		token, err := term.ReadPassword(syscall.Stdin)
		if err != nil {
			return nil, errors.Wrap(err, "read GitHub personal access token")
		}
		fmt.Println()
		c.GitHub.PersonalAccessToken = string(token)
	}

	if c.GitHub.Approval.Enabled {
		if len(c.GitHub.Approval.AllowedUsernames) == 0 {
			return nil, errors.New("github.approval.allowed_usernames cannot be empty")
		}
		if len(c.GitHub.Approval.AllowedRepositories) == 0 {
			return nil, errors.New("github.approval.allowed_repositories cannot be empty")
		}
	}

	if c.Slack.SendLogLevel != "" && c.Slack.WebhookURL == "" {
		fmt.Print("Slack Webhook URL: ")
		webhookURL, err := term.ReadPassword(syscall.Stdin)
		if err != nil {
			return nil, errors.Wrap(err, "read Slack webhook URL")
		}
		fmt.Println()
		c.Slack.WebhookURL = string(webhookURL)
	}

	for i, f := range c.Filters {
		program, err := expr.Compile(f.Condition)
		if err != nil {
			return nil, errors.Wrapf(err, "compile condition for filter %q", f.Name)
		}
		c.Filters[i].CompiledCondition = program

		var hasGitHubReviewAction bool
		for _, action := range f.Actions {
			if githubReviewRegexp.MatchString(action) {
				hasGitHubReviewAction = true
				if !c.GitHub.Approval.Enabled {
					return nil, errors.Errorf("GitHub review action is used in filter %q but GitHub integration is not enabled", f.Name)
				}
			}
		}

		if hasGitHubReviewAction {
			hasGitHubPullRequestPrefetch := false
			for _, prefetch := range f.Prefetches {
				if githubPullRequestRegexp.MatchString(prefetch) {
					hasGitHubPullRequestPrefetch = true
					break
				}
			}
			if !hasGitHubPullRequestPrefetch {
				return nil, errors.Errorf(`"GitHub review" action in filter %q requires "GitHub pull request" prefetch`, f.Name)
			}
		}
	}

	return &c, nil
}
