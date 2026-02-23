# srvrmgr Spec Adherence Report

**Date:** 2026-02-10 (original) | **Updated:** 2026-02-17 (post production hardening)
**Method:** 5 independent evaluators assessed the codebase against specs, followed by 8 deep-dive investigators verifying each unique claim
**Specs evaluated:** 5 design documents in `docs/plans/`

---

## Executive Summary

srvrmgr's core architecture is **well-implemented and production-ready**. The 5 trigger types, Claude Code executor, memory system with semantic search, CLI commands, and daemon event loop all work correctly. Following the production hardening effort (see `docs/plans/spec-production-hardening.md`), the previously identified gaps have been addressed: retry logic, rule dependencies, template variables, config merge, hot-reload, log rotation, state persistence, API endpoints, security model, and 5 production rule definitions are all implemented.

**Overall spec adherence: ~95%** -- core architecture plus operational/reliability features are implemented. Only memory-enabled-by-default remains unaddressed (deferred by design).

> **Note:** Items marked with **(FIXED)** below were resolved in commits between 2026-02-10 and 2026-02-17. The original findings are preserved for historical context.

---

## PART 1: What the Application Gets Right

### 1. All 5 Trigger Types Correctly Implemented
**Confidence: HIGH** (5/5 evaluators + deep-dive confirmed)

The trigger factory at `internal/trigger/factory.go` dispatches to all 5 types defined in the v2 spec. Each is implemented with good engineering practices:

- **Filesystem** (`filesystem.go`): Debouncing with per-path timers, mutex protection, ignore pattern matching, home dir expansion, non-blocking event sends. Test coverage for basic events and ignore patterns.
- **Scheduled** (`scheduled.go`): Supports `cron_expression`, `run_every`, and `run_at` via robfig/cron. The `convertSimpleToCron` function correctly parses duration strings and time formats.
- **Webhook** (`webhook.go`): Constant-time secret comparison (`crypto/subtle`), 1MB body limit, method filtering, rich event data. Shared HTTP server in daemon with path-based routing.
- **Lifecycle** (`lifecycle.go`): Correct integration -- `daemon_started` fires after trigger init; `daemon_stopped` uses a clever direct-handle bypass since the event loop has already exited.
- **Manual** (`manual.go`): Blocks in `Start()`, fires on explicit `Fire()` call. Daemon's `RunRule` creates the event directly.

**Notable quality:** Non-blocking sends with `select/default` across all triggers prevent channel deadlocks.

### 2. Memory System Fully Implemented with Semantic Search
**Confidence: HIGH** (5/5 evaluators + deep-dive confirmed, all tests pass)

The memory layer matches both the memory-layer-design and embeddings-design specs:

- **SQLite schema** matches spec exactly: `memories` table with FTS5 virtual table, insert/delete/update triggers for FTS sync, embedding BLOB column for 384-dim vectors.
- **MCP server** (`internal/mcp/server.go`) exposes all 3 tools (`remember`, `recall`, `forget`) with descriptions matching the spec. Recall supports both `semantic` (default) and `keyword` modes.
- **Embedder** (`internal/embedder/`) uses hugot with all-MiniLM-L6-v2, lazy initialization via `sync.Once`, and correct cosine similarity computation.
- **Auto-injection** works: daemon injects memory MCP config into Claude executions with per-rule opt-out support.
- **Bonus beyond spec:** HTTP MCP transport mode, update trigger for FTS sync.

### 3. Executor Correctly Maps All Config Options to CLI Flags
**Confidence: HIGH** (5/5 evaluators + deep-dive confirmed)

`BuildArgs` at `internal/executor/claude.go:36-73` maps all 9 ClaudeConfig fields to their correct CLI flags:

| Config Field | CLI Flag | Verified |
|---|---|---|
| `Model` | `--model` | Correct |
| `AllowedTools` | `--allowedTools` (comma-joined) | Correct |
| `DisallowedTools` | `--disallowedTools` (comma-joined) | Correct |
| `AddDirs` | `--add-dir` (per item) | Correct |
| `PermissionMode` | `--permission-mode` | Correct |
| `MaxBudgetUSD` | `--max-budget-usd` (%.2f) | Correct |
| `SystemPrompt` | `--system-prompt` | Correct |
| `AppendSystemPrompt` | `--append-system-prompt` | Correct |
| `MCPConfig` | `--mcp-config` (per item) | Correct |

Also correct: `sudo -u` for `run_as_user`, context-based timeout/cancellation, dry_run -> `--permission-mode plan`, debug mode with `--verbose` + `--output-format stream-json`.

### 4. CLI Implements All Specified Commands
**Confidence: HIGH** (5/5 evaluators + deep-dive confirmed)

All commands from both the integration design and homebrew installer specs are present and functional:

`init`, `start`, `stop`, `restart`, `status`, `list`, `validate`, `run`, `logs`, `uninstall`

The `uninstall` command matches the homebrew spec exactly: root check, launchctl unload, plist removal, config removal prompt, `--keep-config`/`--remove-config` flags, brew uninstall message.

### 5. Daemon Event Loop with Concurrency Control
**Confidence: HIGH** (5/5 evaluators + deep-dive confirmed)

The daemon at `internal/daemon/daemon.go` correctly implements:
- Config + rule loading at startup
- Trigger initialization for enabled rules
- Lifecycle event firing (daemon_started/daemon_stopped)
- Concurrency limiting via semaphore (buffered channel sized to `MaxConcurrent`)
- Graceful shutdown with `WaitGroup` for in-flight handlers
- Conditional webhook server startup
- Template expansion of prompts with event data
- Memory auto-injection with per-rule override

---

## PART 2: What the Application Gets Wrong

### ~~CRITICAL: Retry Logic Not Implemented~~ **(FIXED in e597b17)**
**Confidence: VERY HIGH** (5/5 evaluators + deep-dive confirmed)
**Severity: HIGH**

~~Both design specs define `on_failure.retry` with `retry_attempts` and `retry_delay_seconds`. The config types parse these fields correctly. However, `daemon.go:334-344` contains only a TODO stub.~~

**Resolution:** Retry logic fully implemented in `daemon.go:handleFailure()` with configurable attempts and delay. Exponential backoff is not used — fixed delay as specified in config.

### ~~CRITICAL: `depends_on_rules` Parsed but Never Enforced~~ **(FIXED in 3646453)**
**Confidence: VERY HIGH** (5/5 evaluators + deep-dive confirmed)
**Severity: HIGH**

~~The `DependsOn []string` field is parsed from YAML but never referenced in `daemon.go`. Rules fire immediately when triggered regardless of dependency status.~~

**Resolution:** Dependency enforcement implemented in `daemon.go:handleEvent()`. Rules check `lastRunState` for all dependencies before executing. Failed dependencies cause the rule to be skipped with a warning log. Additionally, conditional trigger chains via `TRIGGER:` markers in output are now supported (FR-13).

### ~~HIGH: Template Variables Missing Across Triggers~~ **(FIXED in e597b17 + production hardening)**
**Confidence: VERY HIGH** (4/5 evaluators + deep-dive confirmed with exact code paths)
**Severity: HIGH**

~~Several spec-defined variables were set on the Event struct but NOT in the Data map.~~

**Resolution:** Filesystem and scheduled triggers fixed in e597b17. Production hardening (FR-1) added central injection in `daemon.handleEvent()` — `event_type` and `timestamp` are now injected into `event.Data` as defaults before template expansion, covering all trigger types (lifecycle, manual, triggered) that previously emitted empty Data maps. Template values are also sanitized (FR-16) to prevent prompt injection via filenames.

### ~~HIGH: No Structural Rule Validation~~ **(FIXED in 3646453 + production hardening)**
**Confidence: HIGH** (1/5 evaluators + deep-dive confirmed)
**Severity: HIGH**

~~`config.LoadRule()` only does `yaml.Unmarshal` — no semantic validation.~~

**Resolution:** `ValidateRule()` in `config/loader.go` now validates: required fields (name, trigger type, action prompt), trigger type enum, type-specific fields (watch_paths for filesystem, schedule for scheduled), timeout bounds (1-3600s), `run_as_user` allowlist, `bypassPermissions` rejection, and `triggers_rules`/`depends_on` overlap warnings.

### MEDIUM: Memory Disabled by Default (Spec Says Enabled)
**Confidence: HIGH** (2/5 evaluators + deep-dive confirmed)
**Severity: MEDIUM**

The memory spec says `enabled: true # default: true`. But Go's zero value for `bool` is `false`, and `applyGlobalDefaults()` never sets `Enabled = true`. The `srvrmgr init` default config doesn't include a `memory:` section. Fresh installations have memory disabled.

**Impact:** Users must manually add `memory: enabled: true` to config.yaml. The spec's "auto-injected for all rules by default" design is inverted.

### ~~MEDIUM: No Hot-Reload of Rules Directory~~ **(FIXED in production hardening)**
**Confidence: HIGH** (1/5 evaluators + deep-dive confirmed)
**Severity: MEDIUM**

~~Rules are loaded once at startup. Any rule changes require a daemon restart.~~

**Resolution:** Hot-reload implemented (FR-4) via fsnotify watcher on the rules directory with 1-second debounce. Adding, modifying, or deleting `.yaml`/`.yml` files triggers reload. Change detection only restarts triggers for modified rules. Rules directory permissions are validated before each reload (FR-14).

### ~~MEDIUM: No Log Rotation~~ **(FIXED in production hardening)**
**Confidence: HIGH** (1/5 evaluators + deep-dive confirmed)
**Severity: MEDIUM**

~~The logging package is a 53-line slog wrapper with zero rotation logic.~~

**Resolution:** In-process log rotation implemented (FR-6) via `RotatingWriter` in `internal/logging/rotating.go`. Writes to `/Library/Logs/srvrmgr/srvrmgrd.log`, rotates at 50MB, keeps 5 compressed (.gz) rotated files. Thread-safe with mutex. launchd plist updated to remove `StandardOutPath`/`StandardErrorPath` — daemon manages its own log file.

### ~~MEDIUM: `directory_created` Event Type Never Emitted~~ **(FIXED in production hardening)**
**Confidence: HIGH** (4/5 evaluators + deep-dive confirmed)
**Severity: MEDIUM**

~~All `fsnotify.Create` events mapped to `"file_created"` without checking if the path is a directory.~~

**Resolution:** FR-11 implemented in `internal/trigger/filesystem.go` — on `fsnotify.Create` events, `os.Stat()` checks if the path is a directory and emits `"directory_created"` accordingly.

### ~~LOW: `logs --follow` Flag Missing~~ **(FIXED in production hardening)**
**Confidence: HIGH** (1/5 evaluators + deep-dive confirmed)
**Severity: LOW**

~~Only `-f` (short flag) registered. `srvrmgr logs --follow` fails.~~

**Resolution:** FR-10 — both `-f` and `--follow` flags now registered in `cmd/srvrmgr/main.go`.

### ~~LOW: 6-Field Cron Expression Required (Spec Shows 5-Field)~~ **(FIXED in production hardening)**
**Confidence: HIGH** (deep-dive found)
**Severity: LOW**

~~`cron.WithSeconds()` requires 6-field expressions. Standard 5-field cron expressions fail.~~

**Resolution:** FR-9 — `normalizeCronExpression()` in `internal/trigger/scheduled.go` detects 5-field expressions (by counting space-separated fields) and prepends `0` for the seconds field.

### ~~LOW: Config Merge Only Covers 3 of 9 Fields~~ **(FIXED in production hardening)**
**Confidence: HIGH** (deep-dive found)
**Severity: LOW**

~~`mergeClaudeConfig` only merges `Model`, `PermissionMode`, and `MaxBudgetUSD`. The remaining 6 fields are not merged.~~

**Resolution:** FR-2 — `mergeClaudeConfig()` now merges all 9 ClaudeConfig fields. For slice fields (`AllowedTools`, `DisallowedTools`, `AddDirs`, `MCPConfig`), rule values are used if non-empty, otherwise global defaults. For string fields (`SystemPrompt`, `AppendSystemPrompt`), same pattern. New `EnvVars` field also merged.

---

## PART 3: Design Decisions (Not Bugs)

### System Trigger Intentionally Omitted in v2
**Deep-dive verdict: Intentional, correct decision.**

The v1 spec defined a `system` trigger type (CPU/memory/disk/process monitoring). The v2 spec explicitly lists only 5 trigger types and does not include `system`. This aligns with v2's "thin orchestration layer" philosophy -- system monitoring is better handled by:
- Scheduled triggers with prompts asking Claude to check metrics via Bash
- External monitoring tools (Prometheus, etc.) posting to webhook triggers

### Execution States — Partially Tracked **(IMPROVED in production hardening)**
The spec defines 8 execution states. 4 are used (`success`, `failure`, `timeout`, `cancelled`). Execution history is now persisted in a SQLite database (FR-5) at `/Library/Application Support/srvrmgr/state/history.db` with rule name, trigger type, state, timestamps, duration, retry attempt, event data, output (truncated/scrubbed), and triggered-by lineage. Queryable via API endpoints `/api/rules` and `/api/history` (FR-7). 90-day retention with automatic cleanup (NFR-1).

---

## PART 4: Quality Assessment

### What's Done Well
- Clean, modular architecture with clear separation of concerns
- Good concurrency patterns (mutexes, non-blocking sends, semaphores, WaitGroups)
- Security-conscious webhook implementation (constant-time comparison, body limits)
- Comprehensive memory system with both keyword and semantic search
- Correct Claude Code CLI integration with all 9 config options
- Graceful shutdown sequence that handles lifecycle events properly
- Good test coverage on memory system and embedder

### What Needs Work
- ~~Test coverage gaps~~ Improved: daemon, trigger, config, executor, template, state, security, logging all have tests
- `Manual.Fire()` is dead code (daemon bypasses it via `RunRule`)
- `mcpURL` parameter naming is misleading (actually a file path for stdio transport)
- Watcher errors in filesystem trigger silently swallowed (`_ = err`)
- Event channel capacity fixed at 100 with silent drops under load
- ~~`LoadRulesDir` fails on first bad rule~~ **FIXED** — continues past invalid rules, logs warnings

---

## Summary Scorecard

| Area | Status | Notes |
|------|--------|-------|
| Trigger types | **PASS** | All 5 implemented correctly |
| Claude executor | **PASS** | All config mappings correct, env_vars + sudo passthrough |
| Memory system | **PASS** | FTS5 + semantic search working |
| MCP server | **PASS** | All 3 tools with correct schemas |
| CLI commands | **PASS** | All specified commands present, --follow flag works |
| Daemon loop | **PASS** | Correct event loop with concurrency |
| launchd integration | **PASS** | Plist correct with Homebrew support |
| Uninstall command | **PASS** | Matches homebrew spec exactly |
| Retry logic | **PASS** | ~~TODO stub~~ Fully implemented (fixed in e597b17) |
| Rule dependencies | **PASS** | ~~Parsed, never enforced~~ Enforced + conditional triggers (fixed in 3646453 + production hardening) |
| Template variables | **PASS** | ~~event_type and timestamp missing~~ Central injection + sanitization (fixed in e597b17 + production hardening) |
| Rule validation | **PASS** | ~~YAML-only~~ Semantic validation: types, bounds, allowlists (fixed in 3646453 + production hardening) |
| Memory defaults | **FAIL** | Disabled by default (spec says enabled) — deferred by design |
| Hot-reload | **PASS** | ~~Not implemented~~ fsnotify watcher with debounce + permissions check (production hardening) |
| Log rotation | **PASS** | ~~Not implemented~~ In-process RotatingWriter, 50MB/5 files/gzip (production hardening) |
| directory_created | **PASS** | ~~Not distinguished~~ os.Stat check on Create events (production hardening) |
| Cron field count | **PASS** | ~~6-field required~~ Auto-normalizes 5-field to 6-field (production hardening) |
| Config merge | **PASS** | ~~Only 3/9 fields~~ All 9+ fields merged (production hardening) |
| State persistence | **PASS** | **(NEW)** SQLite execution history with API endpoints (production hardening) |
| Security model | **PASS** | **(NEW)** Directory permissions, run_as_user allowlist, output scrubbing, template sanitization (production hardening) |
| Production rules | **PASS** | **(NEW)** 5 core automation rules: media organize, storage monitor/cleanup, service health, startup check (production hardening) |
| API endpoints | **PASS** | **(NEW)** /health, /api/rules, /api/history with rate limiting (production hardening) |

**Final score: 22 PASS / 1 FAIL** -- Core architecture plus operational, reliability, and security features all implemented. Only memory-enabled-by-default remains (deferred).

---

## PART 5: Production Hardening Additions (2026-02-17)

The following features were added as part of the production hardening effort (`docs/plans/spec-production-hardening.md`):

### New Packages
- `internal/state/` — SQLite-backed execution history (FR-5)
- `internal/security/` — Directory permissions validation (FR-14), output scrubbing (FR-18), template sanitization (FR-16)
- `internal/logging/rotating.go` — In-process log rotation (FR-6)

### New Config Fields
- `Rule.MaxTimeoutSeconds` — Per-rule timeout override (FR-3)
- `Rule.MaxActions` — Max tool calls per execution (FR-17)
- `DaemonConfig.AllowedRunAsUsers` — Allowlist for run_as_user (FR-15)
- `ClaudeConfig.EnvVars` — Environment variables passed to subprocess (FR-18)

### New API Surface
- `GET /health` — Daemon health check (60 req/min)
- `GET /api/rules` — Rule status listing (30 req/min)
- `GET /api/history` — Execution history with filters (30 req/min)

### Production Rules (5 core)
- `rules/media-organize-incoming.yaml` — Filesystem-triggered media organization
- `rules/storage-monitor-disk.yaml` — Scheduled disk usage monitoring with conditional triggers
- `rules/storage-cleanup-caches.yaml` — Scheduled + on-demand cache cleanup
- `rules/server-check-services.yaml` — Scheduled Homebrew service health checks
- `rules/server-startup-check.yaml` — Lifecycle-triggered startup health check
