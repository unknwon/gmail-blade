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
	Filters     []configFilter    `yaml:"filters"`
}

type configCredentials struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type configServer struct {
	SleepInterval string `yaml:"sleepInterval"`
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

	for i, f := range c.Filters {
		program, err := expr.Compile(f.Condition)
		if err != nil {
			return nil, errors.Wrapf(err, "compile condition for filter %q", f.Name)
		}
		c.Filters[i].CompiledCondition = program
	}

	// Set default server sleep interval if not configured
	if c.Server.SleepInterval == "" {
		c.Server.SleepInterval = "15s"
	}

	// Validate sleep interval duration
	if _, err := time.ParseDuration(c.Server.SleepInterval); err != nil {
		return nil, errors.Wrapf(err, "invalid server sleep interval %q", c.Server.SleepInterval)
	}

	return &c, nil
}
