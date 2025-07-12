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
	SleepInterval string `yaml:"sleepInterval"`
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

	// Allow environment variable override for password
	c.Credentials.Password = os.ExpandEnv(c.Credentials.Password)
	// Prompt for password if empty
	if c.Credentials.Password == "" {
		fmt.Print("Password: ")
		password, err := term.ReadPassword(syscall.Stdin)
		if err != nil {
			return nil, errors.Wrap(err, "read password")
		}
		fmt.Println()
		c.Credentials.Password = string(password)
	}

	// Set default server sleep interval if not configured
	if c.Server.SleepInterval == "" {
		c.Server.SleepInterval = "15s"
	}
	// Validate sleep interval duration
	if _, err := time.ParseDuration(c.Server.SleepInterval); err != nil {
		return nil, errors.Wrapf(err, "invalid server sleep interval %q", c.Server.SleepInterval)
	}

	// Allow environment variable override for GitHub personal access token
	c.GitHub.PersonalAccessToken = os.ExpandEnv(c.GitHub.PersonalAccessToken)

	if c.GitHub.Approval.Enabled {
		// Prompt for GitHub personal access token if empty
		if c.GitHub.PersonalAccessToken == "" {
			fmt.Print("GitHub Personal Access Token: ")
			token, err := term.ReadPassword(syscall.Stdin)
			if err != nil {
				return nil, errors.Wrap(err, "read GitHub personal access token")
			}
			fmt.Println()
			c.GitHub.PersonalAccessToken = string(token)
		}

		// Validate GitHub approval allowlists are not empty
		if len(c.GitHub.Approval.AllowedUsernames) == 0 {
			return nil, errors.New("github.approval.allowed_usernames cannot be empty")
		}
		if len(c.GitHub.Approval.AllowedRepositories) == 0 {
			return nil, errors.New("github.approval.allowed_repositories cannot be empty")
		}
	}

	// Validate Slack configuration
	if c.Slack.SendLogLevel != "" && c.Slack.WebhookURL == "" {
		return nil, errors.New("slack.webhook_url cannot be empty when slack.send_log_level is set")
	}

	for i, f := range c.Filters {
		program, err := expr.Compile(f.Condition)
		if err != nil {
			return nil, errors.Wrapf(err, "compile condition for filter %q", f.Name)
		}
		c.Filters[i].CompiledCondition = program

		// Check if this filter uses GitHub review action
		for _, action := range f.Actions {
			if githubReviewRegexp.MatchString(action) && !c.GitHub.Approval.Enabled {
				return nil, errors.Errorf("GitHub review action is used in filter %q but GitHub integration is not enabled", f.Name)
			}
		}
	}

	return &c, nil
}
