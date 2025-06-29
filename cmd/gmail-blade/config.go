package main

import (
	"os"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

type config struct {
	Credentials struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"credentials"`
	Filters []struct {
		Name              string      `yaml:"name"`
		Condition         string      `yaml:"condition"`
		CompiledCondition *vm.Program `yaml:"-"`
		Action            string      `yaml:"action"`
		HaltOnMatch       bool        `yaml:"halt-on-match"`
	} `yaml:"filters"`
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

	for i, f := range c.Filters {
		program, err := expr.Compile(f.Condition)
		if err != nil {
			return nil, errors.Wrapf(err, "compile condition for filter %q", f.Name)
		}
		c.Filters[i].CompiledCondition = program
	}

	return &c, nil
}
