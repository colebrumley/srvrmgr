# srvrmgr Design Document

A macOS server management daemon that uses Claude agents to execute event-driven automation rules.

## Overview

**srvrmgr** is a composable, config-driven rules engine for server automation. Each rule defines a trigger, an action (prompt for a Claude agent), and sandbox constraints. The daemon runs as root via launchd, starting at boot before user login.

### Components

| Component | Description |
|-----------|-------------|
| **srvrmgrd** | Root daemon via launchd, starts at boot |
| **srvrmgr** | CLI for rule management and daemon control |
| **Rules** | YAML configs defining trigger, action, and sandbox |

### Key Features

- **Event-driven**: Filesystem, scheduled, system metrics, webhooks, lifecycle triggers
- **Composable**: Rules can depend on other rules or trigger follow-up rules
- **Sandboxed**: Per-rule restrictions on paths, commands, network, and resources
- **Flexible execution**: Isolated (fresh agent per run) or persistent agents
- **User context**: Rules can run as specific users while daemon runs as root

---

## Directory Structure

```
/Library/Application Support/srvrmgr/
├── config.yaml                    # Global daemon configuration
├── rules/                         # Rule YAML files
│   ├── organize-downloads.yaml
│   └── ...
├── state/                         # Runtime state
│   └── execution-history.json
└── secrets/
    └── api_key                    # chmod 600, root-owned

/Library/Logs/srvrmgr/
├── srvrmgrd.log                   # Daemon logs
└── rules/                         # Per-rule logs
    └── organize-downloads.log

/Library/LaunchDaemons/
└── com.srvrmgr.daemon.plist       # launchd configuration
```

---

## Global Configuration

```yaml
# /Library/Application Support/srvrmgr/config.yaml

daemon:
  log_level: info  # debug, info, warn, error
  webhook_listen_port: 9876
  webhook_listen_address: 127.0.0.1

agent:
  api_key_source: file
  api_key_file: /Library/Application Support/srvrmgr/secrets/api_key

agent_defaults:
  agent_mode: isolated
  model: claude-sonnet-4-20250514
  max_timeout_seconds: 300
  max_memory_mb: 1024
  allow_network: false

sandbox_defaults:
  allowed_commands: [ls, cat, head, tail, file, mkdir, cp, mv, rm]

logging:
  rotate_on_size_mb: 50
  rotate_on_days: 7
  keep_rotated_files: 5
  format: json

persistent_agents:
  max_idle_seconds: 3600
  max_concurrent: 5

rule_execution:
  max_concurrent_rules: 10
  retry_on_failure: false
  retry_attempts: 3
  retry_delay_seconds: 30
```

---

## Rule Configuration

Each rule is a YAML file in the rules directory:

```yaml
# /Library/Application Support/srvrmgr/rules/organize-downloads.yaml
name: organize-downloads
description: Keep Downloads folder organized by file type
enabled: true
agent_mode: isolated  # isolated = fresh agent per run, persistent = reuse agent
run_as_user: cole     # Run as this user (default: root)

trigger:
  type: filesystem
  watch_paths:
    - /Users/cole/Downloads
  on_events: [file_created, file_modified]
  ignore_patterns: ["*.tmp", ".DS_Store"]
  debounce_seconds: 5

action:
  prompt: |
    A new file appeared in Downloads: {{file_path}}
    Move it to the appropriate subfolder based on type:
    - Images → ~/Pictures/Downloads
    - Documents → ~/Documents/Downloads
    - Videos → ~/Movies/Downloads
    - Archives → extract and organize contents
    - Other → leave in place, log what it is
  include_file_metadata: true

sandbox:
  allowed_read_paths:
    - ~/Downloads
  allowed_write_paths:
    - ~/Downloads
    - ~/Pictures/Downloads
    - ~/Documents/Downloads
    - ~/Movies/Downloads
  allowed_commands: [mv, cp, mkdir, unzip, tar, file]
  allow_network: false
  max_timeout_seconds: 60
  max_memory_mb: 512

on_failure:
  retry: true
  retry_attempts: 3
  retry_delay_seconds: 30

depends_on_rules: []
triggers_rules: []
```

---

## Trigger Types

### Filesystem

```yaml
trigger:
  type: filesystem
  watch_paths: [/path/to/watch]
  on_events: [file_created, file_modified, file_deleted, directory_created]
  ignore_patterns: ["*.tmp", ".DS_Store"]
  debounce_seconds: 5
```

### Scheduled

```yaml
trigger:
  type: scheduled
  cron_expression: "0 */6 * * *"  # Every 6 hours
  # OR simpler syntax:
  run_every: 6h
  run_at: "03:00"  # Daily at 3am
```

### System

```yaml
trigger:
  type: system
  on_events: [cpu_above, memory_above, disk_above, process_started, process_stopped]
  cpu_threshold_percent: 80
  memory_threshold_percent: 90
  disk_threshold_percent: 95
  watch_processes: [nginx, postgres]
  sustained_seconds: 30
```

### Webhook

```yaml
trigger:
  type: webhook
  listen_path: /hooks/deploy-notify
  allowed_methods: [POST]
  require_secret: true
  secret_env_var: DEPLOY_WEBHOOK_SECRET
```

### Lifecycle

```yaml
trigger:
  type: lifecycle
  on_events: [daemon_started, daemon_stopped]
```

### Manual

```yaml
trigger:
  type: manual  # Only runs via: srvrmgr run <rule-name>
```

---

## Rule Dependencies

### depends_on_rules

Prerequisites that must complete successfully before this rule runs:

```yaml
name: deploy-application
depends_on_rules:
  - run-tests
  - build-assets
```

### triggers_rules

Follow-up rules to execute after this rule completes:

```yaml
name: backup-database
triggers_rules:
  - notify-backup-complete
  - sync-to-offsite
```

### Conditional triggers

```yaml
triggers_rules:
  - rule: notify-success
    on_status: success
  - rule: notify-failure
    on_status: failure
  - rule: cleanup
    on_status: any
```

---

## Sandbox Enforcement

Each sandbox dimension is enforced by the daemon, not the agent:

### Filesystem Restrictions

- Custom tools wrap file operations
- Paths validated against `allowed_read_paths` and `allowed_write_paths`
- Paths resolved to absolute, checked for directory traversal
- Symlinks resolved before validation

### Command Restrictions

- Agent receives `execute_command` tool, not raw shell
- Commands parsed, binary name checked against `allowed_commands`
- All commands in pipes/chains must be allowed
- Shell escapes and subshells blocked

### Network Restrictions

- When `allow_network: false`: No HTTP/fetch capabilities
- When `allow_network: true`: Can restrict to `allowed_domains`

### Resource Limits

- `max_timeout_seconds`: Execution killed after duration
- `max_memory_mb`: Process memory capped

---

## CLI Interface

```bash
# Daemon control
srvrmgr start                    # Start daemon (installs launchd plist if needed)
srvrmgr stop                     # Stop daemon
srvrmgr restart                  # Restart daemon
srvrmgr status                   # Show daemon status, uptime, active agents

# Rule management
srvrmgr list                     # List all rules with status
srvrmgr show <rule-name>         # Show full rule config and recent executions
srvrmgr add <path-to-yaml>       # Copy rule file into rules directory
srvrmgr remove <rule-name>       # Remove rule (prompts for confirmation)
srvrmgr enable <rule-name>       # Enable a disabled rule
srvrmgr disable <rule-name>      # Disable without removing
srvrmgr edit <rule-name>         # Open rule in $EDITOR

# Testing & validation
srvrmgr validate                 # Validate all rule configs
srvrmgr validate <rule-name>     # Validate specific rule
srvrmgr test <rule-name>         # Dry-run: show what would happen
srvrmgr run <rule-name>          # Manually trigger a rule now

# Logs
srvrmgr logs                     # Tail daemon logs
srvrmgr logs <rule-name>         # Tail specific rule's logs
srvrmgr logs --follow            # Stream logs in real-time
```

---

## launchd Integration

```xml
<!-- /Library/LaunchDaemons/com.srvrmgr.daemon.plist -->
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

    <key>UserName</key>
    <string>root</string>
</dict>
</plist>
```

Starts at system boot, before user login. Automatically restarts on crash with 10-second throttle.

---

## Error Handling

### Execution States

- `triggered` - Trigger fired, execution starting
- `waiting_dependencies` - Waiting for `depends_on_rules`
- `running` - Agent actively executing
- `success` - Completed without error
- `failure` - Agent error or sandbox violation
- `timeout` - Exceeded `max_timeout_seconds`
- `skipped_dependency_failed` - Dependency rule failed
- `skipped_disabled` - Rule is disabled

### Recovery Behavior

- **Sandbox violations**: Logged with details, execution terminated, counts as failure
- **Daemon crashes**: launchd restarts automatically, incomplete runs logged as `interrupted`
- **API rate limits**: Exponential backoff with jitter
- **Network failures**: Retry with backoff
- **Invalid API key**: Log error, disable rule until config reload

---

## Project Structure

```
srvrmgr/
├── package.json
├── tsconfig.json
├── src/
│   ├── daemon/
│   │   ├── index.ts              # Daemon entry point
│   │   ├── rule-loader.ts        # Load & validate YAML rules
│   │   ├── rule-watcher.ts       # Hot-reload on rule changes
│   │   └── executor.ts           # Agent spawning & lifecycle
│   │
│   ├── triggers/
│   │   ├── index.ts              # Trigger registry
│   │   ├── filesystem.ts         # chokidar-based file watcher
│   │   ├── scheduled.ts          # node-cron scheduler
│   │   ├── system.ts             # CPU/memory/process monitoring
│   │   ├── webhook.ts            # HTTP listener
│   │   └── lifecycle.ts          # Daemon start/stop events
│   │
│   ├── sandbox/
│   │   ├── index.ts              # Sandbox factory
│   │   ├── tools/
│   │   │   ├── read-file.ts      # Sandboxed file read
│   │   │   ├── write-file.ts     # Sandboxed file write
│   │   │   ├── execute-command.ts
│   │   │   └── fetch-url.ts
│   │   └── validators.ts         # Path & command validation
│   │
│   ├── agent/
│   │   ├── isolated.ts           # Fresh agent per execution
│   │   ├── persistent.ts         # Long-running agent pool
│   │   └── context.ts            # Build context from trigger
│   │
│   ├── cli/
│   │   ├── index.ts              # CLI entry point
│   │   └── commands/             # One file per command
│   │
│   ├── logging/
│   │   ├── logger.ts             # Structured JSON logger
│   │   └── rotation.ts           # Size/time-based rotation
│   │
│   └── types/
│       ├── config.ts             # Global config types
│       └── rule.ts               # Rule config types (Zod schemas)
│
├── bin/
│   ├── srvrmgrd                  # Daemon executable
│   └── srvrmgr                   # CLI executable
│
└── install/
    └── com.srvrmgr.daemon.plist  # launchd template
```

---

## Installation

```bash
# Install binaries
sudo cp bin/srvrmgrd /usr/local/bin/
sudo cp bin/srvrmgr /usr/local/bin/

# Create directories
sudo mkdir -p "/Library/Application Support/srvrmgr/rules"
sudo mkdir -p "/Library/Application Support/srvrmgr/state"
sudo mkdir -p "/Library/Application Support/srvrmgr/secrets"
sudo mkdir -p "/Library/Logs/srvrmgr/rules"

# Create initial config
sudo srvrmgr init

# Set API key
echo "sk-ant-..." | sudo tee "/Library/Application Support/srvrmgr/secrets/api_key"
sudo chmod 600 "/Library/Application Support/srvrmgr/secrets/api_key"

# Start daemon
sudo srvrmgr start
```

---

## Tech Stack

- **Language**: TypeScript / Node.js
- **Agent SDK**: Claude Agent SDK (official Anthropic TypeScript SDK)
- **Config parsing**: YAML + Zod for validation
- **File watching**: chokidar
- **Scheduling**: node-cron
- **CLI framework**: Commander.js or similar
- **Logging**: Custom structured JSON logger with rotation
