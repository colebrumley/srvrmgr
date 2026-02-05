// internal/config/loader_test.go
package config

import (
	"os"
	"path/filepath"
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
