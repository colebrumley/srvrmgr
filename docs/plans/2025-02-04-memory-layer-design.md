# Memory Layer Design

A SQLite-backed memory system for srvrmgr that lets Claude store and retrieve domain knowledge across rule executions.

## Overview

Claude running srvrmgr rules often learns things about the systems it's automating - file patterns, API behaviors, system quirks. Currently this knowledge is lost between executions. The memory layer gives Claude a place to store and recall this domain knowledge.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                    srvrmgrd                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  │
│  │   Daemon    │  │ MCP Server  │  │  Memory DB  │  │
│  │  (existing) │  │   (new)     │  │  (SQLite)   │  │
│  └─────────────┘  └─────────────┘  └─────────────┘  │
└─────────────────────────────────────────────────────┘
         │                 ▲
         │ spawns          │ stdio MCP
         ▼                 │
┌─────────────────────────────────────────────────────┐
│              claude --mcp-config ...                │
│         (tools: remember, recall, forget)           │
└─────────────────────────────────────────────────────┘
```

**Key decisions:**

- Single global SQLite database (not per-rule isolation)
- MCP server built into daemon, invoked via `srvrmgrd mcp-server`
- Stdio transport (spawned by Claude, not persistent HTTP)
- Memory auto-injected for all rules by default

## Database Schema

```sql
CREATE TABLE memories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    content TEXT NOT NULL,
    category TEXT,
    rule_name TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE VIRTUAL TABLE memories_fts USING fts5(
    content,
    category,
    content='memories',
    content_rowid='id'
);

CREATE TRIGGER memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, content, category)
    VALUES (new.id, new.content, new.category);
END;

CREATE TRIGGER memories_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, content, category)
    VALUES ('delete', old.id, old.content, old.category);
END;
```

**Fields:**

- `content` - The actual knowledge
- `category` - Optional filtering (file-patterns, api-behaviors, system-quirks, naming-conventions)
- `rule_name` - Which rule created this memory (for context, not isolation)
- Timestamps for housekeeping

**Location:** `~/Library/Application Support/srvrmgr/memory.db`

## MCP Tools

### remember

Store domain knowledge for future rule executions.

```json
{
  "name": "remember",
  "description": "Store domain knowledge you've learned that would help future rule executions. Use for: file patterns/conventions, API behaviors, system quirks, naming conventions. Be specific and factual. Don't store: execution logs, temporary state, or procedural instructions.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "content": { "type": "string", "description": "The knowledge to store" },
      "category": { "type": "string", "description": "Optional category: file-patterns, api-behaviors, system-quirks, naming-conventions" }
    },
    "required": ["content"]
  }
}
```

### recall

Search memories for relevant domain knowledge.

```json
{
  "name": "recall",
  "description": "Search stored memories for relevant domain knowledge. Use before making assumptions about files, APIs, or system behaviors you've encountered before.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "query": { "type": "string", "description": "Search terms (full-text search)" },
      "category": { "type": "string", "description": "Optional category filter" }
    },
    "required": ["query"]
  }
}
```

### forget

Remove outdated or incorrect knowledge.

```json
{
  "name": "forget",
  "description": "Remove a memory that's no longer accurate or relevant. Use when you learn something that contradicts a stored memory.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "id": { "type": "integer", "description": "Memory ID to remove (from recall results)" }
    },
    "required": ["id"]
  }
}
```

## Configuration

### Global config

```yaml
# config.yaml
memory:
  enabled: true                           # default: true
  path: "~/custom/memory.db"              # optional override
```

### Per-rule opt-out

```yaml
# rules/some-rule.yaml
claude:
  memory: false   # disable memory for this rule
```

### MCP config merging

Memory is additive. Rules can use custom MCP configs alongside memory:

```yaml
# rules/organize-downloads.yaml
claude:
  mcp_config:
    - "~/.config/mcp/my-other-server.json"
  # memory: true (implicit)
```

The daemon merges:
1. Memory MCP (auto-injected if enabled)
2. Rule-level `mcp_config` entries
3. Global default `mcp_config` entries

## Implementation

### New packages

- `internal/memory/` - SQLite wrapper (init schema, CRUD, FTS search)
- `internal/mcp/` - MCP server implementation (stdio transport, tool handlers)

### New commands

- `srvrmgrd mcp-server` - Stdio MCP server for Claude to spawn

### Modified files

- `cmd/srvrmgrd/main.go` - Add mcp-server subcommand
- `internal/config/types.go` - Memory config struct
- `internal/executor/claude.go` - Auto-inject memory MCP when spawning Claude
