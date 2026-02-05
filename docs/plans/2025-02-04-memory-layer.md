# Memory Layer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a SQLite-backed memory system that lets Claude store and retrieve domain knowledge across rule executions via MCP tools (remember, recall, forget).

**Architecture:** MCP server built into `srvrmgrd` using stdio transport. SQLite database with FTS5 for full-text search. Memory auto-injected for all rules by default.

**Tech Stack:** Go, SQLite (via `modernc.org/sqlite` - pure Go, no CGO), Official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk/mcp`)

---

## Task 1: Add Dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add SQLite and MCP SDK dependencies**

Run:
```bash
go get modernc.org/sqlite
go get github.com/modelcontextprotocol/go-sdk/mcp
```

**Step 2: Verify dependencies installed**

Run: `go mod tidy && cat go.mod`

Expected: `modernc.org/sqlite` and `github.com/modelcontextprotocol/go-sdk/mcp` in require block

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add SQLite and MCP SDK dependencies"
```

---

## Task 2: Memory Storage - Schema and Init

**Files:**
- Create: `internal/memory/db.go`
- Test: `internal/memory/db_test.go`

**Step 1: Write failing test for database initialization**

```go
// internal/memory/db_test.go
package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	// Verify file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}

func TestOpenDBCreatesSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	// Verify memories table exists
	var tableName string
	err = db.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='memories'").Scan(&tableName)
	if err != nil {
		t.Errorf("memories table not created: %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/... -v`

Expected: FAIL (package does not exist)

**Step 3: Write minimal implementation**

```go
// internal/memory/db.go
package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite database connection
type DB struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS memories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    content TEXT NOT NULL,
    category TEXT,
    rule_name TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    content,
    category,
    content='memories',
    content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, content, category)
    VALUES (new.id, new.content, new.category);
END;

CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, content, category)
    VALUES ('delete', old.id, old.content, old.category);
END;

CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, content, category)
    VALUES ('delete', old.id, old.content, old.category);
    INSERT INTO memories_fts(rowid, content, category)
    VALUES (new.id, new.content, new.category);
END;
`

// Open opens or creates a memory database at the given path
func Open(path string) (*DB, error) {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Initialize schema
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing schema: %w", err)
	}

	return &DB{db: db}, nil
}

// Close closes the database connection
func (d *DB) Close() error {
	return d.db.Close()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/memory/... -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/memory/
git commit -m "feat(memory): add SQLite database initialization with FTS5"
```

---

## Task 3: Memory Storage - CRUD Operations

**Files:**
- Modify: `internal/memory/db.go`
- Modify: `internal/memory/db_test.go`

**Step 1: Write failing test for Remember**

Add to `internal/memory/db_test.go`:

```go
func TestRemember(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	id, err := db.Remember("downloads folder contains invoices", "file-patterns", "organize-downloads")
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	if id == 0 {
		t.Error("Remember() returned id = 0, want > 0")
	}
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return db
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/... -v -run TestRemember`

Expected: FAIL (method does not exist)

**Step 3: Write Remember implementation**

Add to `internal/memory/db.go`:

```go
// Remember stores a new memory and returns its ID
func (d *DB) Remember(content, category, ruleName string) (int64, error) {
	result, err := d.db.Exec(
		"INSERT INTO memories (content, category, rule_name) VALUES (?, ?, ?)",
		content, category, ruleName,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting memory: %w", err)
	}
	return result.LastInsertId()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/memory/... -v -run TestRemember`

Expected: PASS

**Step 5: Write failing test for Recall**

Add to `internal/memory/db_test.go`:

```go
func TestRecall(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// Insert test data
	db.Remember("downloads folder contains PDF invoices from Acme Corp", "file-patterns", "rule1")
	db.Remember("API returns timestamps in PST not UTC", "api-behaviors", "rule2")
	db.Remember("downloads has monthly reports", "file-patterns", "rule1")

	// Search for invoices
	memories, err := db.Recall("invoices", "")
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(memories) != 1 {
		t.Errorf("Recall() returned %d memories, want 1", len(memories))
	}
	if len(memories) > 0 && memories[0].Content != "downloads folder contains PDF invoices from Acme Corp" {
		t.Errorf("Recall() returned wrong content: %s", memories[0].Content)
	}
}

func TestRecallWithCategory(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.Remember("downloads has invoices", "file-patterns", "rule1")
	db.Remember("downloads has reports", "file-patterns", "rule1")
	db.Remember("API timeout behavior", "api-behaviors", "rule2")

	// Search with category filter
	memories, err := db.Recall("downloads", "file-patterns")
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(memories) != 2 {
		t.Errorf("Recall() with category returned %d memories, want 2", len(memories))
	}
}
```

**Step 6: Run test to verify it fails**

Run: `go test ./internal/memory/... -v -run TestRecall`

Expected: FAIL (method does not exist, Memory type does not exist)

**Step 7: Write Recall implementation**

Add to `internal/memory/db.go`:

```go
import "time"

// Memory represents a stored memory
type Memory struct {
	ID        int64
	Content   string
	Category  string
	RuleName  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Recall searches memories using full-text search with optional category filter
func (d *DB) Recall(query, category string) ([]Memory, error) {
	var rows *sql.Rows
	var err error

	if category != "" {
		rows, err = d.db.Query(`
			SELECT m.id, m.content, m.category, m.rule_name, m.created_at, m.updated_at
			FROM memories m
			JOIN memories_fts fts ON m.id = fts.rowid
			WHERE memories_fts MATCH ? AND m.category = ?
			ORDER BY rank
		`, query, category)
	} else {
		rows, err = d.db.Query(`
			SELECT m.id, m.content, m.category, m.rule_name, m.created_at, m.updated_at
			FROM memories m
			JOIN memories_fts fts ON m.id = fts.rowid
			WHERE memories_fts MATCH ?
			ORDER BY rank
		`, query)
	}
	if err != nil {
		return nil, fmt.Errorf("querying memories: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		var category, ruleName sql.NullString
		if err := rows.Scan(&m.ID, &m.Content, &category, &ruleName, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning memory: %w", err)
		}
		m.Category = category.String
		m.RuleName = ruleName.String
		memories = append(memories, m)
	}
	return memories, rows.Err()
}
```

**Step 8: Run test to verify it passes**

Run: `go test ./internal/memory/... -v -run TestRecall`

Expected: PASS

**Step 9: Write failing test for Forget**

Add to `internal/memory/db_test.go`:

```go
func TestForget(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	id, _ := db.Remember("outdated info", "file-patterns", "rule1")

	err := db.Forget(id)
	if err != nil {
		t.Fatalf("Forget() error = %v", err)
	}

	// Verify it's gone
	memories, _ := db.Recall("outdated", "")
	if len(memories) != 0 {
		t.Errorf("Forget() did not delete memory, found %d", len(memories))
	}
}

func TestForgetNotFound(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := db.Forget(99999)
	if err == nil {
		t.Error("Forget() should return error for non-existent ID")
	}
}
```

**Step 10: Run test to verify it fails**

Run: `go test ./internal/memory/... -v -run TestForget`

Expected: FAIL (method does not exist)

**Step 11: Write Forget implementation**

Add to `internal/memory/db.go`:

```go
import "errors"

// ErrNotFound is returned when a memory is not found
var ErrNotFound = errors.New("memory not found")

// Forget deletes a memory by ID
func (d *DB) Forget(id int64) error {
	result, err := d.db.Exec("DELETE FROM memories WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting memory: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
```

**Step 12: Run all memory tests**

Run: `go test ./internal/memory/... -v`

Expected: All PASS

**Step 13: Commit**

```bash
git add internal/memory/
git commit -m "feat(memory): add Remember, Recall, Forget operations"
```

---

## Task 4: MCP Server - Tool Handlers

**Files:**
- Create: `internal/mcp/server.go`
- Test: `internal/mcp/server_test.go`

**Step 1: Write failing test for MCP server creation**

```go
// internal/mcp/server_test.go
package mcp

import (
	"path/filepath"
	"testing"
)

func TestNewServer(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	server, err := NewServer(dbPath)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer server.Close()

	if server == nil {
		t.Error("NewServer() returned nil")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/... -v`

Expected: FAIL (package does not exist)

**Step 3: Write MCP server implementation**

```go
// internal/mcp/server.go
package mcp

import (
	"context"
	"fmt"

	"github.com/colebrumley/srvrmgr/internal/memory"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wraps the MCP server with memory tools
type Server struct {
	db     *memory.DB
	server *mcp.Server
}

// RememberInput is the input schema for the remember tool
type RememberInput struct {
	Content  string `json:"content" jsonschema:"required,description=The knowledge to store"`
	Category string `json:"category,omitempty" jsonschema:"description=Optional category: file-patterns, api-behaviors, system-quirks, naming-conventions"`
}

// RecallInput is the input schema for the recall tool
type RecallInput struct {
	Query    string `json:"query" jsonschema:"required,description=Search terms (full-text search)"`
	Category string `json:"category,omitempty" jsonschema:"description=Optional category filter"`
}

// ForgetInput is the input schema for the forget tool
type ForgetInput struct {
	ID int64 `json:"id" jsonschema:"required,description=Memory ID to remove (from recall results)"`
}

// NewServer creates a new MCP server with memory tools
func NewServer(dbPath string) (*Server, error) {
	db, err := memory.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening memory database: %w", err)
	}

	s := &Server{db: db}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "srvrmgr-memory",
		Version: "1.0.0",
	}, nil)

	// Register remember tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "remember",
		Description: "Store domain knowledge you've learned that would help future rule executions. Use for: file patterns/conventions, API behaviors, system quirks, naming conventions. Be specific and factual. Don't store: execution logs, temporary state, or procedural instructions.",
	}, s.handleRemember)

	// Register recall tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recall",
		Description: "Search stored memories for relevant domain knowledge. Use before making assumptions about files, APIs, or system behaviors you've encountered before.",
	}, s.handleRecall)

	// Register forget tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "forget",
		Description: "Remove a memory that's no longer accurate or relevant. Use when you learn something that contradicts a stored memory.",
	}, s.handleForget)

	s.server = server
	return s, nil
}

func (s *Server) handleRemember(ctx context.Context, req *mcp.CallToolRequest, input RememberInput) (*mcp.CallToolResult, error) {
	id, err := s.db.Remember(input.Content, input.Category, "")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to store memory: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Stored memory with ID %d", id)), nil
}

func (s *Server) handleRecall(ctx context.Context, req *mcp.CallToolRequest, input RecallInput) (*mcp.CallToolResult, error) {
	memories, err := s.db.Recall(input.Query, input.Category)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to search memories: %v", err)), nil
	}

	if len(memories) == 0 {
		return mcp.NewToolResultText("No memories found matching your query."), nil
	}

	var result string
	for _, m := range memories {
		result += fmt.Sprintf("[ID: %d] %s", m.ID, m.Content)
		if m.Category != "" {
			result += fmt.Sprintf(" (category: %s)", m.Category)
		}
		result += "\n"
	}
	return mcp.NewToolResultText(result), nil
}

func (s *Server) handleForget(ctx context.Context, req *mcp.CallToolRequest, input ForgetInput) (*mcp.CallToolResult, error) {
	err := s.db.Forget(input.ID)
	if err != nil {
		if err == memory.ErrNotFound {
			return mcp.NewToolResultError(fmt.Sprintf("memory with ID %d not found", input.ID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to delete memory: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Deleted memory with ID %d", input.ID)), nil
}

// Run starts the MCP server on stdio
func (s *Server) Run(ctx context.Context) error {
	return s.server.Run(ctx, mcp.NewStdioTransport())
}

// Close closes the database connection
func (s *Server) Close() error {
	return s.db.Close()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/mcp/... -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/mcp/
git commit -m "feat(mcp): add MCP server with remember, recall, forget tools"
```

---

## Task 5: Daemon MCP Subcommand

**Files:**
- Modify: `cmd/srvrmgrd/main.go`

**Step 1: Update daemon to support mcp-server subcommand**

Replace `cmd/srvrmgrd/main.go`:

```go
// cmd/srvrmgrd/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/colebrumley/srvrmgr/internal/daemon"
	"github.com/colebrumley/srvrmgr/internal/mcp"
)

const (
	defaultConfigPath = "/Library/Application Support/srvrmgr/config.yaml"
	defaultRulesDir   = "/Library/Application Support/srvrmgr/rules"
	defaultMemoryDB   = "/Library/Application Support/srvrmgr/memory.db"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp-server" {
		runMCPServer()
		return
	}

	runDaemon()
}

func runMCPServer() {
	dbPath := os.Getenv("SRVRMGR_MEMORY_DB")
	if dbPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error getting home directory: %v\n", err)
			os.Exit(1)
		}
		dbPath = filepath.Join(homeDir, "Library/Application Support/srvrmgr/memory.db")
	}

	server, err := mcp.NewServer(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating MCP server: %v\n", err)
		os.Exit(1)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := server.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}

func runDaemon() {
	configPath := os.Getenv("SRVRMGR_CONFIG")
	if configPath == "" {
		configPath = defaultConfigPath
	}

	rulesDir := os.Getenv("SRVRMGR_RULES_DIR")
	if rulesDir == "" {
		rulesDir = defaultRulesDir
	}

	d := daemon.New(configPath, rulesDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nReceived shutdown signal")
		cancel()
	}()

	if err := d.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "daemon error: %v\n", err)
		os.Exit(1)
	}
}
```

**Step 2: Verify it builds**

Run: `go build ./cmd/srvrmgrd`

Expected: No errors

**Step 3: Commit**

```bash
git add cmd/srvrmgrd/main.go
git commit -m "feat(daemon): add mcp-server subcommand"
```

---

## Task 6: Config Types for Memory

**Files:**
- Modify: `internal/config/types.go`

**Step 1: Add memory config fields**

Add to `internal/config/types.go` after `RuleExecConfig`:

```go
type MemoryConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}
```

Update `Global` struct to include Memory field:

```go
type Global struct {
	Daemon         DaemonConfig   `yaml:"daemon"`
	ClaudeDefaults ClaudeConfig   `yaml:"claude_defaults"`
	Logging        LoggingConfig  `yaml:"logging"`
	RuleExecution  RuleExecConfig `yaml:"rule_execution"`
	Memory         MemoryConfig   `yaml:"memory"`
}
```

Add Memory field to `ClaudeConfig` for per-rule override:

```go
type ClaudeConfig struct {
	Model              string   `yaml:"model"`
	AllowedTools       []string `yaml:"allowed_tools"`
	DisallowedTools    []string `yaml:"disallowed_tools"`
	AddDirs            []string `yaml:"add_dirs"`
	PermissionMode     string   `yaml:"permission_mode"`
	MaxBudgetUSD       float64  `yaml:"max_budget_usd"`
	SystemPrompt       string   `yaml:"system_prompt"`
	AppendSystemPrompt string   `yaml:"append_system_prompt"`
	MCPConfig          []string `yaml:"mcp_config"`
	Memory             *bool    `yaml:"memory"`  // nil = inherit, true = enable, false = disable
}
```

**Step 2: Update defaults in loader.go**

Add to `applyGlobalDefaults` in `internal/config/loader.go`:

```go
// Memory enabled by default
if !cfg.Memory.Enabled && cfg.Memory.Path == "" {
	cfg.Memory.Enabled = true
}
```

**Step 3: Verify it builds**

Run: `go build ./...`

Expected: No errors

**Step 4: Commit**

```bash
git add internal/config/
git commit -m "feat(config): add memory configuration fields"
```

---

## Task 7: Auto-inject Memory MCP

**Files:**
- Modify: `internal/executor/claude.go`
- Modify: `internal/executor/claude_test.go`
- Modify: `internal/daemon/daemon.go`

**Step 1: Write failing test for MCP config generation**

Add to `internal/executor/claude_test.go`:

```go
func TestBuildArgsWithMemory(t *testing.T) {
	cfg := config.ClaudeConfig{
		Model: "sonnet",
	}

	memoryEnabled := true
	daemonPath := "/usr/local/bin/srvrmgrd"

	args := BuildArgsWithMemory(cfg, "Do something", false, memoryEnabled, daemonPath)

	assertContains(t, args, "--mcp-config")
	// Should contain a temp file path for the memory MCP config
	found := false
	for i, arg := range args {
		if arg == "--mcp-config" && i+1 < len(args) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected --mcp-config flag for memory")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/... -v -run TestBuildArgsWithMemory`

Expected: FAIL (function does not exist)

**Step 3: Add memory MCP injection to executor**

Update `internal/executor/claude.go`:

```go
import (
	"encoding/json"
	"os"
)

// MCPServerConfig represents an MCP server configuration
type MCPServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// MCPConfig represents the MCP configuration file format
type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// BuildArgsWithMemory constructs command-line arguments with optional memory MCP injection
func BuildArgsWithMemory(cfg config.ClaudeConfig, prompt string, debug bool, memoryEnabled bool, daemonPath string) ([]string, func(), error) {
	args := BuildArgs(cfg, prompt, debug)
	cleanup := func() {}

	if memoryEnabled && daemonPath != "" {
		// Create temporary MCP config file
		mcpCfg := MCPConfig{
			MCPServers: map[string]MCPServerConfig{
				"srvrmgr-memory": {
					Command: daemonPath,
					Args:    []string{"mcp-server"},
				},
			},
		}

		tmpFile, err := os.CreateTemp("", "srvrmgr-mcp-*.json")
		if err != nil {
			return nil, nil, fmt.Errorf("creating temp MCP config: %w", err)
		}

		if err := json.NewEncoder(tmpFile).Encode(mcpCfg); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			return nil, nil, fmt.Errorf("writing MCP config: %w", err)
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
```

**Step 4: Update test and run**

Update test to match new signature:

```go
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

	// Should not have memory MCP config
	for _, arg := range args {
		if arg == "srvrmgr-mcp" {
			t.Error("memory MCP config should not be present when disabled")
		}
	}
}
```

Run: `go test ./internal/executor/... -v`

Expected: PASS

**Step 5: Update daemon to use memory injection**

Update `handleEvent` in `internal/daemon/daemon.go` to use `BuildArgsWithMemory`. Add helper to determine if memory is enabled for a rule:

```go
func (d *Daemon) isMemoryEnabled(rule *config.Rule) bool {
	// Per-rule override takes precedence
	if rule.Claude.Memory != nil {
		return *rule.Claude.Memory
	}
	// Fall back to global config
	return d.config.Memory.Enabled
}
```

Update the `Execute` call to pass memory state (this requires updating the Execute function signature or creating a new function).

**Step 6: Run all tests**

Run: `go test ./... -v`

Expected: All PASS

**Step 7: Commit**

```bash
git add internal/executor/ internal/daemon/
git commit -m "feat(executor): auto-inject memory MCP config when enabled"
```

---

## Task 8: Integration Test

**Files:**
- Create: `internal/memory/integration_test.go`

**Step 1: Write integration test for full flow**

```go
// internal/memory/integration_test.go
//go:build integration

package memory

import (
	"path/filepath"
	"testing"
)

func TestMemoryFullFlow(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// Remember some things
	id1, err := db.Remember("Acme invoices have prefix ACME-INV-", "file-patterns", "organize-downloads")
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}

	id2, err := db.Remember("Reports are always PDF format", "file-patterns", "organize-downloads")
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}

	// Recall by search
	memories, err := db.Recall("invoices", "")
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(memories) != 1 {
		t.Errorf("expected 1 memory for 'invoices', got %d", len(memories))
	}

	// Recall by category
	memories, err = db.Recall("*", "file-patterns")
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(memories) != 2 {
		t.Errorf("expected 2 memories in file-patterns, got %d", len(memories))
	}

	// Forget one
	err = db.Forget(id1)
	if err != nil {
		t.Fatalf("Forget() error = %v", err)
	}

	// Verify it's gone
	memories, err = db.Recall("Acme", "")
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("expected 0 memories after forget, got %d", len(memories))
	}

	// Other memory still exists
	memories, err = db.Recall("Reports", "")
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(memories) != 1 {
		t.Errorf("expected 1 memory for 'Reports', got %d", len(memories))
	}

	_ = id2 // silence unused warning
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return db
}
```

**Step 2: Run integration test**

Run: `go test ./internal/memory/... -v -tags=integration`

Expected: PASS

**Step 3: Commit**

```bash
git add internal/memory/
git commit -m "test(memory): add integration test for full memory flow"
```

---

## Task 9: Update Example Rule

**Files:**
- Modify: `examples/organize-downloads.yaml`

**Step 1: Add memory usage to example prompt**

Update the prompt in `examples/organize-downloads.yaml` to demonstrate memory usage:

```yaml
# Add to the prompt section
prompt: |
  A new file appeared in Downloads: {{file_path}}

  First, use the recall tool to check if you have any stored knowledge about
  this type of file or the Downloads folder organization patterns.

  Then organize the file appropriately. If you learn something new about the
  file patterns or organization conventions, use the remember tool to store
  that knowledge for future use.

  Available tools: Read, Write, Bash (for mv/mkdir), Glob, Grep
```

**Step 2: Commit**

```bash
git add examples/
git commit -m "docs: update example rule to demonstrate memory usage"
```

---

## Task 10: Final Verification

**Step 1: Run all tests**

Run: `go test ./... -v`

Expected: All PASS

**Step 2: Build both binaries**

Run: `go build ./cmd/srvrmgr && go build ./cmd/srvrmgrd`

Expected: No errors

**Step 3: Test MCP server startup**

Run: `echo '{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' | ./srvrmgrd mcp-server`

Expected: JSON response with server capabilities

**Step 4: Final commit if any changes**

```bash
git status
# If clean, done. Otherwise:
git add -A && git commit -m "chore: final cleanup"
```

---

## Summary

This plan implements:

1. **SQLite memory storage** (`internal/memory/`) with FTS5 full-text search
2. **MCP server** (`internal/mcp/`) with remember/recall/forget tools
3. **Daemon integration** (`cmd/srvrmgrd/`) via `mcp-server` subcommand
4. **Config support** for enabling/disabling memory globally and per-rule
5. **Auto-injection** of memory MCP when executing rules

Total estimated tasks: 10
Testing approach: TDD throughout
