// internal/config/loader.go
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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

	if err := ValidateRule(&rule); err != nil {
		return nil, fmt.Errorf("validating rule in %s: %w", filepath.Base(path), err)
	}

	return &rule, nil
}

// ValidateRule checks that a rule has all required fields and valid configuration.
func ValidateRule(rule *Rule) error {
	if rule.Name == "" {
		return fmt.Errorf("rule name is required")
	}
	if rule.Trigger.Type == "" {
		return fmt.Errorf("trigger type is required")
	}
	if rule.Action.Prompt == "" {
		return fmt.Errorf("action prompt is required")
	}

	validTypes := map[string]bool{
		"filesystem": true,
		"scheduled":  true,
		"webhook":    true,
		"lifecycle":  true,
		"manual":     true,
	}
	if !validTypes[rule.Trigger.Type] {
		return fmt.Errorf("invalid trigger type %q: must be one of filesystem, scheduled, webhook, lifecycle, manual", rule.Trigger.Type)
	}

	switch rule.Trigger.Type {
	case "filesystem":
		if len(rule.Trigger.WatchPaths) == 0 {
			return fmt.Errorf("filesystem trigger requires at least one watch_paths entry")
		}
	case "scheduled":
		if rule.Trigger.CronExpression == "" && rule.Trigger.RunEvery == "" && rule.Trigger.RunAt == "" {
			return fmt.Errorf("scheduled trigger requires at least one of cron_expression, run_every, or run_at")
		}
	case "webhook":
		if rule.Trigger.ListenPath == "" {
			return fmt.Errorf("webhook trigger requires listen_path")
		}
		if !strings.HasPrefix(rule.Trigger.ListenPath, "/") {
			return fmt.Errorf("webhook listen_path must start with \"/\"")
		}
	case "lifecycle":
		if len(rule.Trigger.OnEvents) == 0 {
			return fmt.Errorf("lifecycle trigger requires at least one on_events entry")
		}
	}

	if rule.OnFailure.Retry && rule.OnFailure.RetryAttempts <= 0 {
		rule.OnFailure.RetryAttempts = 3
	}

	// FR-3: Validate max_timeout_seconds range
	if rule.MaxTimeoutSeconds < 0 {
		return fmt.Errorf("max_timeout_seconds must be >= 0, got %d", rule.MaxTimeoutSeconds)
	}
	if rule.MaxTimeoutSeconds > 3600 {
		return fmt.Errorf("max_timeout_seconds must be <= 3600 (1 hour), got %d", rule.MaxTimeoutSeconds)
	}

	// FR-15: Reject run_as_user: root
	if rule.RunAsUser == "root" {
		return fmt.Errorf("run_as_user cannot be \"root\" — rules must never run as root")
	}

	// FR-15: Reject bypassPermissions mode
	if rule.Claude.PermissionMode == "bypassPermissions" {
		return fmt.Errorf("permission_mode \"bypassPermissions\" is not allowed for daemon rules")
	}

	return nil
}

// ValidateRuleWithGlobal performs additional validation that requires global config context.
// FR-15: Checks run_as_user against the allowed_run_as_users allowlist.
// FR-19: Warns about triggers_rules / depends_on overlap.
// Sourced from architect — clean separation of global-context validation.
func ValidateRuleWithGlobal(rule *Rule, global *Global, allRules map[string]*Rule) []string {
	var warnings []string

	// FR-15: Validate run_as_user against allowlist
	if rule.RunAsUser != "" && len(global.Daemon.AllowedRunAsUsers) > 0 {
		allowed := false
		for _, u := range global.Daemon.AllowedRunAsUsers {
			if u == rule.RunAsUser {
				allowed = true
				break
			}
		}
		if !allowed {
			warnings = append(warnings, fmt.Sprintf("rule %q: run_as_user %q is not in allowed_run_as_users list", rule.Name, rule.RunAsUser))
		}
	}

	// FR-19: Warn about triggers_rules / depends_on overlap
	if len(rule.DependsOn) > 0 && allRules != nil {
		for _, dep := range rule.DependsOn {
			if parentRule, ok := allRules[dep]; ok {
				for _, triggered := range parentRule.Triggers {
					if triggered == rule.Name {
						warnings = append(warnings, fmt.Sprintf(
							"rule %q has depends_on_rules: [%s] which also has triggers_rules: [%s] — this is redundant; triggers_rules already implies 'fire on success'",
							rule.Name, dep, rule.Name,
						))
					}
				}
			}
		}
	}

	return warnings
}

// LoadRulesDir loads all rules from a directory.
// FR-8: Invalid rules are logged via slog and skipped; valid rules are still returned.
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
			// FR-8: Use slog for warnings about invalid rules (not log.Printf or fmt.Fprintf)
			slog.Warn("skipping invalid rule", "file", entry.Name(), "error", err)
			continue
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
