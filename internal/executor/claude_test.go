// internal/executor/claude_test.go
package executor

import (
	"testing"

	"github.com/colebrumley/srvrmgr/internal/config"
)

func TestBuildArgs(t *testing.T) {
	cfg := config.ClaudeConfig{
		Model:           "sonnet",
		AllowedTools:    []string{"Bash", "Read"},
		DisallowedTools: []string{"WebFetch"},
		AddDirs:         []string{"/home/user/Downloads"},
		PermissionMode:  "default",
		MaxBudgetUSD:    0.50,
		SystemPrompt:    "You are helpful",
	}

	args := BuildArgs(cfg, "Do something", false)

	// Check required flags
	assertContains(t, args, "--print")
	assertContains(t, args, "--model")
	assertContains(t, args, "sonnet")
	assertContains(t, args, "--allowedTools")
	assertContains(t, args, "Bash,Read")
	assertContains(t, args, "--disallowedTools")
	assertContains(t, args, "WebFetch")
	assertContains(t, args, "--add-dir")
	assertContains(t, args, "/home/user/Downloads")
	assertContains(t, args, "--permission-mode")
	assertContains(t, args, "default")
	assertContains(t, args, "--max-budget-usd")
	assertContains(t, args, "0.50")
	assertContains(t, args, "--system-prompt")
	assertContains(t, args, "You are helpful")

	// Prompt should be last
	if args[len(args)-1] != "Do something" {
		t.Errorf("expected prompt as last arg, got %s", args[len(args)-1])
	}
}

func TestBuildArgsDebugMode(t *testing.T) {
	cfg := config.ClaudeConfig{Model: "sonnet"}
	args := BuildArgs(cfg, "test", true)

	assertContains(t, args, "--output-format")
	assertContains(t, args, "stream-json")
}

func TestBuildArgsWithDryRun(t *testing.T) {
	cfg := config.ClaudeConfig{
		Model:          "sonnet",
		PermissionMode: "plan", // dry_run maps to plan mode
	}
	args := BuildArgs(cfg, "test", false)

	assertContains(t, args, "--permission-mode")
	assertContains(t, args, "plan")
}

func assertContains(t *testing.T, slice []string, val string) {
	t.Helper()
	for _, v := range slice {
		if v == val {
			return
		}
	}
	t.Errorf("expected %v to contain %q", slice, val)
}

func TestBuildArgsWithMemory(t *testing.T) {
	cfg := config.ClaudeConfig{
		Model: "sonnet",
	}

	args, cleanup, err := BuildArgsWithMemory(cfg, "Do something", false, true, "/usr/local/bin/srvrmgrd")
	if err != nil {
		t.Fatalf("BuildArgsWithMemory() error = %v", err)
	}
	defer cleanup()

	assertContains(t, args, "--mcp-config")

	// Verify the prompt is still last
	if args[len(args)-1] != "Do something" {
		t.Errorf("expected prompt as last arg, got %s", args[len(args)-1])
	}
}

func TestBuildArgsWithMemoryDisabled(t *testing.T) {
	cfg := config.ClaudeConfig{
		Model: "sonnet",
	}

	args, cleanup, err := BuildArgsWithMemory(cfg, "Do something", false, false, "/usr/local/bin/srvrmgrd")
	if err != nil {
		t.Fatalf("BuildArgsWithMemory() error = %v", err)
	}
	defer cleanup()

	// Should not have memory MCP config injected
	// Simply verify the number of --mcp-config flags matches cfg.MCPConfig count
	mcpCount := 0
	for _, arg := range args {
		if arg == "--mcp-config" {
			mcpCount++
		}
	}
	if mcpCount != len(cfg.MCPConfig) {
		t.Errorf("expected %d --mcp-config flags, got %d", len(cfg.MCPConfig), mcpCount)
	}
}

func TestBuildArgsWithMemoryEmptyDaemonPath(t *testing.T) {
	cfg := config.ClaudeConfig{
		Model: "sonnet",
	}

	args, cleanup, err := BuildArgsWithMemory(cfg, "Do something", false, true, "")
	if err != nil {
		t.Fatalf("BuildArgsWithMemory() error = %v", err)
	}
	defer cleanup()

	// Memory enabled but no daemon path - should not inject MCP config
	mcpCount := 0
	for _, arg := range args {
		if arg == "--mcp-config" {
			mcpCount++
		}
	}
	if mcpCount != len(cfg.MCPConfig) {
		t.Errorf("expected %d --mcp-config flags (no injection), got %d", len(cfg.MCPConfig), mcpCount)
	}
}
