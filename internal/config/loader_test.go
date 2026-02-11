// internal/config/loader_test.go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadGlobal(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	content := `
daemon:
  log_level: debug
  webhook_listen_port: 9876
  webhook_listen_address: 127.0.0.1
claude_defaults:
  model: sonnet
  permission_mode: default
logging:
  format: json
  debug: false
rule_execution:
  max_concurrent: 10
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadGlobal(configPath)
	if err != nil {
		t.Fatalf("LoadGlobal failed: %v", err)
	}

	if cfg.Daemon.LogLevel != "debug" {
		t.Errorf("expected log_level debug, got %s", cfg.Daemon.LogLevel)
	}
	if cfg.Daemon.WebhookListenPort != 9876 {
		t.Errorf("expected port 9876, got %d", cfg.Daemon.WebhookListenPort)
	}
	if cfg.ClaudeDefaults.Model != "sonnet" {
		t.Errorf("expected model sonnet, got %s", cfg.ClaudeDefaults.Model)
	}
}

func TestLoadRule(t *testing.T) {
	dir := t.TempDir()
	rulePath := filepath.Join(dir, "test-rule.yaml")

	content := `
name: test-rule
description: A test rule
enabled: true
run_as_user: testuser
trigger:
  type: filesystem
  watch_paths:
    - ~/Downloads
  on_events:
    - file_created
  debounce_seconds: 5
action:
  prompt: "Handle file: {{file_path}}"
claude:
  model: sonnet
  allowed_tools:
    - Bash
    - Read
  add_dirs:
    - ~/Downloads
  permission_mode: default
`
	if err := os.WriteFile(rulePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rule, err := LoadRule(rulePath)
	if err != nil {
		t.Fatalf("LoadRule failed: %v", err)
	}

	if rule.Name != "test-rule" {
		t.Errorf("expected name test-rule, got %s", rule.Name)
	}
	if !rule.Enabled {
		t.Error("expected rule to be enabled")
	}
	if rule.Trigger.Type != "filesystem" {
		t.Errorf("expected trigger type filesystem, got %s", rule.Trigger.Type)
	}
	if len(rule.Claude.AllowedTools) != 2 {
		t.Errorf("expected 2 allowed tools, got %d", len(rule.Claude.AllowedTools))
	}
}

func validRule() Rule {
	return Rule{
		Name:    "test-rule",
		Trigger: Trigger{Type: "filesystem", WatchPaths: []string{"~/Downloads"}},
		Action:  Action{Prompt: "do something"},
	}
}

func TestValidateRule_Valid(t *testing.T) {
	rule := validRule()
	if err := ValidateRule(&rule); err != nil {
		t.Fatalf("expected valid rule, got error: %v", err)
	}
}

func TestValidateRule_MissingName(t *testing.T) {
	rule := validRule()
	rule.Name = ""
	err := ValidateRule(&rule)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "rule name is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateRule_InvalidTriggerType(t *testing.T) {
	rule := validRule()
	rule.Trigger.Type = "unknown"
	err := ValidateRule(&rule)
	if err == nil {
		t.Fatal("expected error for invalid trigger type")
	}
	if !strings.Contains(err.Error(), "invalid trigger type") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateRule_FilesystemNoWatchPaths(t *testing.T) {
	rule := validRule()
	rule.Trigger.Type = "filesystem"
	rule.Trigger.WatchPaths = nil
	err := ValidateRule(&rule)
	if err == nil {
		t.Fatal("expected error for filesystem without watch_paths")
	}
	if !strings.Contains(err.Error(), "watch_paths") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateRule_ScheduledNoExpression(t *testing.T) {
	rule := validRule()
	rule.Trigger.Type = "scheduled"
	rule.Trigger.WatchPaths = nil
	err := ValidateRule(&rule)
	if err == nil {
		t.Fatal("expected error for scheduled without any schedule expression")
	}
	if !strings.Contains(err.Error(), "cron_expression") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateRule_WebhookNoListenPath(t *testing.T) {
	rule := validRule()
	rule.Trigger.Type = "webhook"
	rule.Trigger.WatchPaths = nil
	err := ValidateRule(&rule)
	if err == nil {
		t.Fatal("expected error for webhook without listen_path")
	}
	if !strings.Contains(err.Error(), "listen_path") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateRule_WebhookBadPath(t *testing.T) {
	rule := validRule()
	rule.Trigger.Type = "webhook"
	rule.Trigger.ListenPath = "no-leading-slash"
	err := ValidateRule(&rule)
	if err == nil {
		t.Fatal("expected error for webhook with path not starting with /")
	}
	if !strings.Contains(err.Error(), "must start with") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateRule_RetryDefaultsAttempts(t *testing.T) {
	rule := validRule()
	rule.OnFailure.Retry = true
	rule.OnFailure.RetryAttempts = 0
	if err := ValidateRule(&rule); err != nil {
		t.Fatalf("expected valid rule, got error: %v", err)
	}
	if rule.OnFailure.RetryAttempts != 3 {
		t.Errorf("expected retry_attempts defaulted to 3, got %d", rule.OnFailure.RetryAttempts)
	}
}

func TestValidateRule_LifecycleNoEvents(t *testing.T) {
	rule := validRule()
	rule.Trigger.Type = "lifecycle"
	rule.Trigger.WatchPaths = nil
	rule.Trigger.OnEvents = nil
	err := ValidateRule(&rule)
	if err == nil {
		t.Fatal("expected error for lifecycle without on_events")
	}
	if !strings.Contains(err.Error(), "on_events") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateRule_MissingPrompt(t *testing.T) {
	rule := validRule()
	rule.Action.Prompt = ""
	err := ValidateRule(&rule)
	if err == nil {
		t.Fatal("expected error for missing prompt")
	}
	if !strings.Contains(err.Error(), "action prompt is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateRule_MissingTriggerType(t *testing.T) {
	rule := validRule()
	rule.Trigger.Type = ""
	err := ValidateRule(&rule)
	if err == nil {
		t.Fatal("expected error for missing trigger type")
	}
	if !strings.Contains(err.Error(), "trigger type is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}
