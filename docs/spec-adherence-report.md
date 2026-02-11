# srvrmgr Spec Adherence Report

**Date:** 2026-02-10
**Method:** 5 independent evaluators assessed the codebase against specs, followed by 8 deep-dive investigators verifying each unique claim
**Specs evaluated:** 5 design documents in `docs/plans/`

---

## Executive Summary

srvrmgr's core architecture is **well-implemented and sound**. The 5 trigger types, Claude Code executor, memory system with semantic search, CLI commands, and daemon event loop all work correctly and match the v2 spec (Claude Code Integration design). However, several features defined in the specs remain **unimplemented or stubbed**, and there are template variable bugs that would affect real-world rule prompts. The application is functional for basic automation workflows but lacks the reliability features (retry, dependencies) and operational polish (hot-reload, log rotation, validation) needed for production use.

**Overall spec adherence: ~70%** -- core architecture is solid, but important features are missing.

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

### CRITICAL: Retry Logic Not Implemented
**Confidence: VERY HIGH** (5/5 evaluators + deep-dive confirmed)
**Severity: HIGH**

Both design specs define `on_failure.retry` with `retry_attempts` and `retry_delay_seconds`. The config types parse these fields correctly. However, `daemon.go:334-344` contains only:

```go
// TODO: Implement retry logic
logger.Warn("rule failed, retry not yet implemented", "error", err)
```

**Impact:** The shipped example config (`organize-downloads.yaml`) includes `retry: true, retry_attempts: 2` -- giving users the impression retry works. Failed rules are silently dropped after a warning log. This is a data loss scenario for transient failures.

### CRITICAL: `depends_on_rules` Parsed but Never Enforced
**Confidence: VERY HIGH** (5/5 evaluators + deep-dive confirmed)
**Severity: HIGH**

The design spec defines prerequisite rules that must complete before a dependent rule runs, with execution states `waiting_dependencies` and `skipped_dependency_failed`. The `DependsOn []string` field is parsed from YAML but **never referenced** in `daemon.go`. Rules fire immediately when triggered regardless of dependency status.

**Impact:** The spec's example of `deploy-application` depending on `run-tests` and `build-assets` would fire the deploy without waiting for tests. No dependency graph, no execution state tracking, no waiting mechanism exists.

**Related:** `triggers_rules` IS partially implemented (`fireTriggeredRules` at `daemon.go:346-359`) but only fires on success. The conditional `on_status: success|failure|any` syntax from the spec cannot be represented in the current `[]string` type.

### HIGH: Template Variables Missing Across Triggers
**Confidence: VERY HIGH** (4/5 evaluators + deep-dive confirmed with exact code paths)
**Severity: HIGH**

The template engine reads from `Event.Data` only. Several spec-defined variables are set on the Event struct but NOT in the Data map:

| Trigger | Variable | Spec Requires | Implementation | Status |
|---------|----------|---------------|----------------|--------|
| Filesystem | `{{file_path}}` | Yes | In Data map | OK |
| Filesystem | `{{file_name}}` | Yes | In Data map | OK |
| Filesystem | `{{event_type}}` | Yes | In `Event.Type` only, NOT in Data | **BUG** |
| Scheduled | `{{timestamp}}` | Yes | `Event.Timestamp` set, Data is empty `{}` | **BUG** |
| Webhook | `{{http_body}}` | Yes | In Data map | OK |
| Webhook | `{{http_headers}}` | Yes | In Data as `map[string]string` (renders as Go map literal) | **PARTIAL** |

**Impact:** Any prompt using `{{event_type}}` or `{{timestamp}}` will render literally -- Claude will see the raw `{{event_type}}` text instead of "file_created". This breaks the primary use case of the example rule.

**Recommended fix:** Inject `event.Type` as `"event_type"` and `event.Timestamp.Format(RFC3339)` as `"timestamp"` into `event.Data` centrally in `daemon.handleEvent` before template expansion.

### HIGH: No Structural Rule Validation
**Confidence: HIGH** (1/5 evaluators + deep-dive confirmed)
**Severity: HIGH**

`config.LoadRule()` only does `yaml.Unmarshal` -- no semantic validation whatsoever. A rule with `trigger.type: "banana"`, empty `name`, or a filesystem trigger with no `watch_paths` will pass validation and only fail at runtime.

The original spec defines comprehensive Zod schemas with required fields, enum validation, and type-specific field requirements. None of this exists in the Go implementation.

**Impact:** `srvrmgr validate` gives false confidence -- it only catches YAML syntax errors, not configuration mistakes.

### MEDIUM: Memory Disabled by Default (Spec Says Enabled)
**Confidence: HIGH** (2/5 evaluators + deep-dive confirmed)
**Severity: MEDIUM**

The memory spec says `enabled: true # default: true`. But Go's zero value for `bool` is `false`, and `applyGlobalDefaults()` never sets `Enabled = true`. The `srvrmgr init` default config doesn't include a `memory:` section. Fresh installations have memory disabled.

**Impact:** Users must manually add `memory: enabled: true` to config.yaml. The spec's "auto-injected for all rules by default" design is inverted.

### MEDIUM: No Hot-Reload of Rules Directory
**Confidence: HIGH** (1/5 evaluators + deep-dive confirmed)
**Severity: MEDIUM**

The v2 spec pseudocode includes `go d.watchRulesDir(ctx)` for hot-reload. This function does not exist. Rules are loaded once at startup. Any rule changes require a daemon restart.

**Impact:** For a long-running system daemon, this is a significant operational burden. Every rule edit requires `srvrmgr restart`.

### MEDIUM: No Log Rotation
**Confidence: HIGH** (1/5 evaluators + deep-dive confirmed)
**Severity: MEDIUM**

The original spec defines `rotate_on_size_mb: 50`, `rotate_on_days: 7`, `keep_rotated_files: 5`. The logging package is a 53-line slog wrapper with zero rotation logic. No rotation config fields exist.

**Impact:** Log files grow unbounded. On a long-running production daemon, this is a disk space risk.

### MEDIUM: `directory_created` Event Type Never Emitted
**Confidence: HIGH** (4/5 evaluators + deep-dive confirmed)
**Severity: MEDIUM**

The original spec lists `directory_created` as a valid filesystem event. The implementation maps all `fsnotify.Create` events to `"file_created"` without checking if the path is a directory. Rules subscribing to only `directory_created` would never fire.

### LOW: `logs --follow` Flag Missing
**Confidence: HIGH** (1/5 evaluators + deep-dive confirmed)
**Severity: LOW**

Both specs document `srvrmgr logs --follow`. The code registers only `-f` (short flag). Go's `flag` package doesn't auto-generate long flags. Running `srvrmgr logs --follow` fails.

### LOW: 6-Field Cron Expression Required (Spec Shows 5-Field)
**Confidence: HIGH** (deep-dive found)
**Severity: LOW**

The cron library is initialized with `cron.WithSeconds()`, requiring a 6-field expression (sec min hour dom month dow). The spec shows standard 5-field cron: `"0 */6 * * *"`. Users following the spec will get parse errors. `convertSimpleToCron` generates correct 6-field expressions, so `run_every`/`run_at` work fine.

### LOW: Config Merge Only Covers 3 of 9 Fields
**Confidence: HIGH** (deep-dive found)
**Severity: LOW**

`mergeClaudeConfig` in `daemon.go:317-332` only merges `Model`, `PermissionMode`, and `MaxBudgetUSD` from global `claude_defaults`. The remaining 6 fields (`AllowedTools`, `DisallowedTools`, `AddDirs`, `SystemPrompt`, `AppendSystemPrompt`, `MCPConfig`) are not merged -- if a rule omits them, they default to empty regardless of global config.

---

## PART 3: Design Decisions (Not Bugs)

### System Trigger Intentionally Omitted in v2
**Deep-dive verdict: Intentional, correct decision.**

The v1 spec defined a `system` trigger type (CPU/memory/disk/process monitoring). The v2 spec explicitly lists only 5 trigger types and does not include `system`. This aligns with v2's "thin orchestration layer" philosophy -- system monitoring is better handled by:
- Scheduled triggers with prompts asking Claude to check metrics via Bash
- External monitoring tools (Prometheus, etc.) posting to webhook triggers

### Execution States Not Tracked
The spec defines 8 execution states (`triggered`, `waiting_dependencies`, `running`, `success`, `failure`, `timeout`, `skipped_dependency_failed`, `skipped_disabled`). Only 3 are used (`success`, `failure`, `timeout`) plus a bonus `cancelled` state. There is no execution history persistence (`state/execution-history.json`). This is a consequence of the missing dependency system.

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
- Test coverage gaps: no tests for lifecycle or manual triggers
- `Manual.Fire()` is dead code (daemon bypasses it via `RunRule`)
- `mcpURL` parameter naming is misleading (actually a file path for stdio transport)
- Watcher errors in filesystem trigger silently swallowed (`_ = err`)
- Event channel capacity fixed at 100 with silent drops under load
- `LoadRulesDir` fails on first bad rule (all-or-nothing loading)

---

## Summary Scorecard

| Area | Status | Notes |
|------|--------|-------|
| Trigger types | **PASS** | All 5 implemented correctly |
| Claude executor | **PASS** | All config mappings correct |
| Memory system | **PASS** | FTS5 + semantic search working |
| MCP server | **PASS** | All 3 tools with correct schemas |
| CLI commands | **PASS** | All specified commands present |
| Daemon loop | **PASS** | Correct event loop with concurrency |
| launchd integration | **PASS** | Plist correct with Homebrew support |
| Uninstall command | **PASS** | Matches homebrew spec exactly |
| Retry logic | **FAIL** | TODO stub, never retries |
| Rule dependencies | **FAIL** | Parsed, never enforced |
| Template variables | **FAIL** | event_type and timestamp missing |
| Rule validation | **FAIL** | YAML-only, no semantic checks |
| Memory defaults | **FAIL** | Disabled by default (spec says enabled) |
| Hot-reload | **FAIL** | Not implemented |
| Log rotation | **FAIL** | Not implemented |
| directory_created | **FAIL** | Not distinguished from file_created |
| --follow flag | **FAIL** | Only -f works |
| Cron field count | **FAIL** | 6-field required, spec shows 5-field |
| Config merge | **FAIL** | Only 3 of 9 fields merged |

**Final score: 9 PASS / 10 FAIL** -- Core architecture passes, operational/reliability features fail.
