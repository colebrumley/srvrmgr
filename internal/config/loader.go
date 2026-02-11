// internal/config/loader.go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadGlobal loads the global configuration from a YAML file
func LoadGlobal(path string) (*Global, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Global
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	applyGlobalDefaults(&cfg)
	return &cfg, nil
}

// LoadRule loads a rule configuration from a YAML file
func LoadRule(path string) (*Rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading rule file: %w", err)
	}

	var rule Rule
	if err := yaml.Unmarshal(data, &rule); err != nil {
		return nil, fmt.Errorf("parsing rule file: %w", err)
	}

	return &rule, nil
}

// LoadRulesDir loads all rules from a directory
func LoadRulesDir(dir string) ([]*Rule, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading rules directory: %w", err)
	}

	var rules []*Rule
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		rule, err := LoadRule(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("loading rule %s: %w", entry.Name(), err)
		}
		rules = append(rules, rule)
	}

	return rules, nil
}

func applyGlobalDefaults(cfg *Global) {
	if cfg.Daemon.LogLevel == "" {
		cfg.Daemon.LogLevel = "info"
	}
	if cfg.Daemon.WebhookListenPort == 0 {
		cfg.Daemon.WebhookListenPort = 9876
	}
	if cfg.Daemon.WebhookListenAddress == "" {
		cfg.Daemon.WebhookListenAddress = "127.0.0.1"
	}
	if cfg.ClaudeDefaults.Model == "" {
		cfg.ClaudeDefaults.Model = "sonnet"
	}
	if cfg.ClaudeDefaults.PermissionMode == "" {
		cfg.ClaudeDefaults.PermissionMode = "default"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}
	if cfg.RuleExecution.MaxConcurrent <= 0 {
		cfg.RuleExecution.MaxConcurrent = 10
	}
	// Memory: only set default path if enabled and path not set
	if cfg.Memory.Enabled && cfg.Memory.Path == "" {
		if homeDir, err := os.UserHomeDir(); err == nil {
			cfg.Memory.Path = filepath.Join(homeDir, "Library", "Application Support", "srvrmgr", "memory.db")
		}
	}
}
