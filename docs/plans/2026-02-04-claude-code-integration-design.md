# srvrmgr v2: Claude Code Integration

A rewrite of srvrmgr from TypeScript to Go, replacing direct Anthropic API calls with Claude Code CLI invocation.

## Overview

srvrmgr becomes a thin orchestration layer:
1. **Triggers** - Watch for events (filesystem, cron, webhooks, lifecycle)
2. **Execution** - Spawn `claude --print` with configured flags

Claude Code handles the agent loop, tooling, permissions, and user authentication.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         srvrmgrd                            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  Triggers   │  │    Rules    │  │      Executor       │  │
│  │             │  │             │  │                     │  │
│  │ filesystem  │──│  YAML +     │──│  sudo -u $user      │  │
│  │ scheduled   │  │  validation │  │  claude --print ... │  │
│  │ webhook     │  │             │  │                     │  │
│  │ lifecycle   │  │             │  │                     │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │   Claude Code   │
                    │   (user auth)   │
                    └─────────────────┘
```

## Project Structure

```
srvrmgr/
├── go.mod
├── go.sum
├── cmd/
│   ├── srvrmgrd/              # Daemon binary
│   │   └── main.go
│   └── srvrmgr/               # CLI binary
│       └── main.go
├── internal/
│   ├── config/
│   │   ├── global.go          # Global config parsing
│   │   └── rule.go            # Rule config + validation
│   ├── trigger/
│   │   ├── trigger.go         # Trigger interface
│   │   ├── filesystem.go      # fsnotify-based
│   │   ├── scheduled.go       # robfig/cron
│   │   ├── webhook.go         # HTTP listener
│   │   ├── lifecycle.go       # Daemon start/stop
│   │   └── manual.go          # CLI-triggered
│   ├── executor/
│   │   └── claude.go          # Spawn claude subprocess
│   ├── daemon/
│   │   └── daemon.go          # Main loop, rule loading
│   └── logging/
│       └── logger.go          # Structured JSON logging
├── install/
│   └── com.srvrmgr.daemon.plist
└── examples/
    └── organize-downloads.yaml
```

## Dependencies

- `github.com/fsnotify/fsnotify` - File watching
- `github.com/robfig/cron/v3` - Cron scheduling
- `gopkg.in/yaml.v3` - YAML parsing
- Standard library for HTTP, exec, logging (log/slog)

## Rule Schema

```yaml
name: organize-downloads
description: Keep Downloads folder organized by file type
enabled: true
run_as_user: cole

trigger:
  type: filesystem
  watch_paths: [~/Downloads]
  on_events: [file_created, file_modified]
  ignore_patterns: ["*.tmp", ".DS_Store"]
  debounce_seconds: 5

action:
  prompt: |
    A new file appeared: {{file_path}}
    Move it to the appropriate subfolder based on type:
    - Images → ~/Pictures/Downloads
    - Documents → ~/Documents/Downloads
    - Videos → ~/Movies/Downloads

claude:
  model: sonnet
  allowed_tools: [Bash, Read, Write, Glob, Grep]
  disallowed_tools: [WebFetch, WebSearch]
  add_dirs: [~/Downloads, ~/Pictures, ~/Documents, ~/Movies]
  permission_mode: default
  max_budget_usd: 0.50
  system_prompt: "You are organizing files for the user."
  append_system_prompt: "Always log what you moved and why."
  mcp_config:
    - ~/.config/mcp/servers.json

dry_run: false

depends_on_rules: []
triggers_rules: []

on_failure:
  retry: true
  retry_attempts: 3
  retry_delay_seconds: 30
```

### Claude Config Options

| Field | Maps to | Description |
|-------|---------|-------------|
| `model` | `--model` | Model alias (sonnet, opus) or full name |
| `allowed_tools` | `--allowedTools` | Tools Claude can use |
| `disallowed_tools` | `--disallowedTools` | Tools to block |
| `add_dirs` | `--add-dir` | Directories Claude can access |
| `permission_mode` | `--permission-mode` | default, plan, bypassPermissions |
| `max_budget_usd` | `--max-budget-usd` | Cost cap per execution |
| `system_prompt` | `--system-prompt` | Override system prompt |
| `append_system_prompt` | `--append-system-prompt` | Add to system prompt |
| `mcp_config` | `--mcp-config` | MCP server configs |

### Dry Run

When `dry_run: true`, executor uses `--permission-mode plan` so Claude proposes actions without executing.

## Trigger Types

### Filesystem

```yaml
trigger:
  type: filesystem
  watch_paths: [~/Downloads, /tmp/incoming]
  on_events: [file_created, file_modified, file_deleted]
  ignore_patterns: ["*.tmp", ".DS_Store", "*.part"]
  debounce_seconds: 5
```

Template variables: `{{file_path}}`, `{{file_name}}`, `{{event_type}}`

### Scheduled

```yaml
trigger:
  type: scheduled
  cron_expression: "0 */6 * * *"  # Every 6 hours
  # OR simpler:
  run_every: 6h
  run_at: "03:00"
```

Template variables: `{{timestamp}}`

### Webhook

```yaml
trigger:
  type: webhook
  listen_path: /hooks/deploy
  allowed_methods: [POST]
  require_secret: true
  secret_header: X-Webhook-Secret
  secret_env_var: DEPLOY_SECRET
```

Template variables: `{{http_body}}`, `{{http_headers}}`

### Lifecycle

```yaml
trigger:
  type: lifecycle
  on_events: [daemon_started, daemon_stopped]
```

### Manual

```yaml
trigger:
  type: manual  # Only via: srvrmgr run <rule>
```

## Executor Implementation

```go
func Execute(ctx context.Context, prompt string, cfg ClaudeConfig, user string, debug bool) (*ExecutionResult, error) {
    args := []string{"--print"}

    if debug {
        args = append(args, "--output-format", "stream-json")
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
        args = append(args, "--add-dir", expandPath(dir))
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

    var cmd *exec.Cmd
    if user != "" {
        cmd = exec.CommandContext(ctx, "sudo", append([]string{"-u", user, "claude"}, args...)...)
    } else {
        cmd = exec.CommandContext(ctx, "claude", args...)
    }

    start := time.Now()
    output, err := cmd.CombinedOutput()
    duration := time.Since(start)

    if err != nil {
        return &ExecutionResult{
            State:    "failure",
            Error:    err.Error(),
            Output:   string(output),
            Duration: duration,
        }, nil
    }

    return &ExecutionResult{
        State:    "success",
        Output:   string(output),
        Duration: duration,
    }, nil
}
```

## Daemon Main Loop

```go
func (d *Daemon) Run(ctx context.Context) error {
    // Load config and rules
    if err := d.loadConfig(); err != nil {
        return err
    }
    if err := d.loadRules(); err != nil {
        return err
    }

    // Start hot-reload watcher on rules directory
    go d.watchRulesDir(ctx)

    // Initialize triggers for enabled rules
    for _, rule := range d.rules {
        if !rule.Enabled {
            continue
        }
        t, err := trigger.New(rule.Trigger)
        if err != nil {
            d.logger.Error("failed to create trigger", "rule", rule.Name, "error", err)
            continue
        }
        d.triggers[rule.Name] = t
        go t.Start(ctx, d.events)
    }

    // Fire lifecycle:daemon_started
    d.fireLifecycleEvent("daemon_started")

    // Main event loop
    for {
        select {
        case event := <-d.events:
            go d.handleEvent(event)
        case <-ctx.Done():
            d.fireLifecycleEvent("daemon_stopped")
            return d.shutdown()
        }
    }
}
```

## Global Configuration

```yaml
# /Library/Application Support/srvrmgr/config.yaml

daemon:
  log_level: info
  webhook_listen_port: 9876
  webhook_listen_address: 127.0.0.1

claude_defaults:
  model: sonnet
  permission_mode: default
  max_budget_usd: 1.00

logging:
  format: json           # json or text
  debug: false           # enables stream-json output from claude

rule_execution:
  max_concurrent: 10
```

## CLI Commands

```bash
# Daemon control
srvrmgr start              # Start daemon via launchctl
srvrmgr stop               # Stop daemon
srvrmgr restart            # Restart daemon
srvrmgr status             # Show daemon status

# Rule management
srvrmgr list               # List all rules
srvrmgr validate           # Validate all rules
srvrmgr validate <rule>    # Validate specific rule
srvrmgr run <rule>         # Manually run a rule

# Setup
srvrmgr init               # Create config directories and default config

# Logs
srvrmgr logs               # Tail daemon logs
srvrmgr logs <rule>        # Tail specific rule logs
srvrmgr logs --follow      # Stream logs
```

## Installation

```bash
# Build
go build -o bin/srvrmgr ./cmd/srvrmgr
go build -o bin/srvrmgrd ./cmd/srvrmgrd

# Install binaries
sudo cp bin/srvrmgrd /usr/local/bin/
sudo cp bin/srvrmgr /usr/local/bin/

# Create directories
sudo mkdir -p "/Library/Application Support/srvrmgr/rules"
sudo mkdir -p "/Library/Logs/srvrmgr/rules"

# Initialize config
sudo srvrmgr init

# Grant Full Disk Access to claude binary (System Preferences)

# Start daemon
sudo srvrmgr start
```

## launchd Configuration

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.srvrmgr.daemon</string>

    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/srvrmgrd</string>
    </array>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>

    <key>ThrottleInterval</key>
    <integer>10</integer>

    <key>StandardOutPath</key>
    <string>/Library/Logs/srvrmgr/srvrmgrd.stdout.log</string>

    <key>StandardErrorPath</key>
    <string>/Library/Logs/srvrmgr/srvrmgrd.stderr.log</string>
</dict>
</plist>
```

## Migration from v1

### Removed
- TypeScript codebase (`src/`, `package.json`, `tsconfig.json`)
- `@anthropic-ai/sdk` dependency
- Custom sandbox system
- API key configuration
- Persistent agent mode

### Schema Changes

```yaml
# v1
agent_mode: isolated
sandbox:
  allowed_read_paths: [~/Downloads]
  allowed_write_paths: [~/Downloads, ~/Pictures]
  allowed_commands: [mv, cp, mkdir]
  allow_network: false
  max_timeout_seconds: 60

# v2
claude:
  add_dirs: [~/Downloads, ~/Pictures]
  allowed_tools: [Bash, Read, Write, Glob, Grep]
  disallowed_tools: [WebFetch, WebSearch]
  permission_mode: default
```

## Requirements

- macOS (launchd integration)
- Go 1.21+ (build)
- Claude Code CLI installed and authenticated
- Full Disk Access granted to `claude` binary (for sudo -u execution)
