// internal/executor/claude.go
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
)

// MCPServerConfig represents an MCP server configuration (stdio)
type MCPServerConfig struct {
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	Type    string   `json:"type,omitempty"`
	URL     string   `json:"url,omitempty"`
}

// MCPConfig represents the MCP configuration file format
type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// Result represents the outcome of a Claude Code execution
type Result struct {
	State    string
	Output   string
	Error    string
	Duration time.Duration
}

// BuildArgs constructs the command-line arguments for claude
func BuildArgs(cfg config.ClaudeConfig, prompt string, debug bool) []string {
	args := []string{"--print"}

	if debug {
		args = append(args, "--verbose", "--output-format", "stream-json")
	}

	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if len(cfg.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(cfg.AllowedTools, ","))
	}
	if len(cfg.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(cfg.DisallowedTools, ","))
	}
	for _, dir := range cfg.AddDirs {
		args = append(args, "--add-dir", dir)
	}
	if cfg.PermissionMode != "" {
		args = append(args, "--permission-mode", cfg.PermissionMode)
	}
	if cfg.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", cfg.MaxBudgetUSD))
	}
	if cfg.SystemPrompt != "" {
		args = append(args, "--system-prompt", cfg.SystemPrompt)
	}
	if cfg.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", cfg.AppendSystemPrompt)
	}
	for _, mcp := range cfg.MCPConfig {
		args = append(args, "--mcp-config", mcp)
	}

	args = append(args, prompt)
	return args
}

// BuildArgsWithMemory constructs command-line arguments with optional memory MCP injection
// If mcpURL is provided, uses HTTP transport; otherwise falls back to stdio with daemonPath
// Returns the args slice, a cleanup function to remove temp files, and any error
func BuildArgsWithMemory(cfg config.ClaudeConfig, prompt string, debug bool, memoryEnabled bool, mcpURL string) ([]string, func(), error) {
	args := BuildArgs(cfg, prompt, debug)
	cleanup := func() {}

	if memoryEnabled && mcpURL != "" {
		// mcpURL is actually the daemon path for stdio transport
		mcpCfg := MCPConfig{
			MCPServers: map[string]MCPServerConfig{
				"srvrmgr-memory": {
					Command: mcpURL,
					Args:    []string{"mcp-server"},
				},
			},
		}

		tmpFile, err := os.CreateTemp("", "srvrmgr-mcp-*.json")
		if err != nil {
			return nil, func() {}, fmt.Errorf("creating temp MCP config: %w", err)
		}

		if err := json.NewEncoder(tmpFile).Encode(mcpCfg); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			return nil, func() {}, fmt.Errorf("writing MCP config: %w", err)
		}
		tmpFile.Close()

		cleanup = func() {
			os.Remove(tmpFile.Name())
		}

		// Insert MCP config before the prompt (last arg)
		promptArg := args[len(args)-1]
		args = args[:len(args)-1]
		args = append(args, "--mcp-config", tmpFile.Name(), promptArg)
	}

	return args, cleanup, nil
}

// Execute runs Claude Code with the given configuration
func Execute(ctx context.Context, prompt string, cfg config.ClaudeConfig, user string, debug bool, workDir string) (*Result, error) {
	return ExecuteWithMemory(ctx, prompt, cfg, user, debug, workDir, false, "")
}

// ExecuteWithMemory runs Claude Code with optional memory MCP injection
// mcpURL should be the HTTP URL of the MCP server (e.g., "http://127.0.0.1:9877")
func ExecuteWithMemory(ctx context.Context, prompt string, cfg config.ClaudeConfig, user string, debug bool, workDir string, memoryEnabled bool, mcpURL string) (*Result, error) {
	args, cleanup, err := BuildArgsWithMemory(cfg, prompt, debug, memoryEnabled, mcpURL)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	var cmd *exec.Cmd
	if user != "" {
		sudoArgs := append([]string{"-u", user, "claude"}, args...)
		cmd = exec.CommandContext(ctx, "sudo", sudoArgs...)
	} else {
		cmd = exec.CommandContext(ctx, "claude", args...)
	}

	if workDir != "" {
		cmd.Dir = workDir
	}

	start := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	if err != nil {
		// Check if it was a context cancellation (timeout)
		if ctx.Err() == context.DeadlineExceeded {
			return &Result{
				State:    "timeout",
				Error:    "execution timed out",
				Output:   string(output),
				Duration: duration,
			}, nil
		}

		return &Result{
			State:    "failure",
			Error:    err.Error(),
			Output:   string(output),
			Duration: duration,
		}, nil
	}

	return &Result{
		State:    "success",
		Output:   string(output),
		Duration: duration,
	}, nil
}
