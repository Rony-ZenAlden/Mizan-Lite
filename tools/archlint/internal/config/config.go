// Package config loads the data-driven architecture rules from arch-rules.yml.
//
// The whole point of this package is that architecture rules are DATA. A rule's
// parameters (which packages, which imports, which calls) live in YAML; only a
// brand-new rule *kind* needs Go code.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the root of arch-rules.yml.
type Config struct {
	Module string `yaml:"module"`
	Rules  Rules  `yaml:"rules"`
}

// Rules holds every supported rule kind. Adding a kind means adding a field here
// and a builder in the rules package — the loader itself does not change.
type Rules struct {
	ImportBoundary ImportBoundaryRule `yaml:"import-boundary"`
	ForbidCall     ForbidCallRule     `yaml:"forbid-call"`
	NoFloat        NoFloatRule        `yaml:"no-float"`
}

// ImportBoundaryRule enforces layer/module import boundaries.
type ImportBoundaryRule struct {
	Enabled    bool       `yaml:"enabled"`
	Boundaries []Boundary `yaml:"boundaries"`
}

// Boundary describes what a set of packages may (allow_only) or may not (forbid) import.
type Boundary struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	AppliesTo   []string `yaml:"applies_to"`
	AllowOnly   []string `yaml:"allow_only"`
	Forbid      []string `yaml:"forbid"`
	Message     string   `yaml:"message"`
}

// ForbidCallRule forbids specific function calls in specific locations.
type ForbidCallRule struct {
	Enabled bool         `yaml:"enabled"`
	Calls   []ForbidCall `yaml:"calls"`
}

// ForbidCall is one forbidden call, scoped by package (applies_to) and file (exclude).
type ForbidCall struct {
	Selector  string   `yaml:"selector"` // "panic", "os.Exit", "fmt.Println", "time.Now"
	AppliesTo []string `yaml:"applies_to"`
	Exclude   []string `yaml:"exclude"`
	Message   string   `yaml:"message"`
}

// NoFloatRule forbids float32/float64 in the given packages.
type NoFloatRule struct {
	Enabled   bool     `yaml:"enabled"`
	AppliesTo []string `yaml:"applies_to"`
	Message   string   `yaml:"message"`
}

// Load reads and validates the config, expanding the `self` token to the module path.
func Load(path string) (*Config, error) {
	if path == "" {
		path = "arch-rules.yml"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("archlint: reading %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("archlint: parsing %s: %w", path, err)
	}
	if c.Module == "" {
		return nil, fmt.Errorf("archlint: %s: `module` is required", path)
	}
	c.expand()
	return &c, nil
}

// expand replaces the literal token "self" with the module path in every pattern,
// so rules can be written module-path-agnostic.
func (c *Config) expand() {
	rep := func(ss []string) {
		for i, s := range ss {
			ss[i] = strings.ReplaceAll(s, "self", c.Module)
		}
	}
	for i := range c.Rules.ImportBoundary.Boundaries {
		b := &c.Rules.ImportBoundary.Boundaries[i]
		rep(b.AppliesTo)
		rep(b.AllowOnly)
		rep(b.Forbid)
	}
	for i := range c.Rules.ForbidCall.Calls {
		call := &c.Rules.ForbidCall.Calls[i]
		rep(call.AppliesTo)
		rep(call.Exclude)
	}
	rep(c.Rules.NoFloat.AppliesTo)
}
