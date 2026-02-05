# Embeddings for Semantic Memory Search

Add local embedding support to the memory layer for semantic search, fully self-contained in the srvrmgrd binary.

## Overview

**Goal:** Enable semantic similarity search for memories using locally-embedded all-MiniLM-L6-v2 model.

**Approach:**
- Bundle ONNX model in binary via `//go:embed` (~23MB)
- Use onnxruntime-go for inference
- Store embeddings as BLOBs in SQLite
- Compute cosine similarity in Go
- Keep FTS5 for keyword search (hybrid approach)

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                       srvrmgrd                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │ MCP Server   │  │  Embedder    │  │   Memory DB      │   │
│  │              │  │ (ONNX model) │  │   (SQLite)       │   │
│  │ remember ────┼──┼─► embed() ───┼──┼─► store          │   │
│  │ recall ──────┼──┼─► embed() ───┼──┼─► similarity     │   │
│  │ forget ──────┼──┼──────────────┼──┼─► delete         │   │
│  └──────────────┘  └──────────────┘  └──────────────────┘   │
│                           │                                  │
│                    ┌──────┴──────┐                          │
│                    │ Embedded    │                          │
│                    │ ONNX Model  │                          │
│                    │ (~23MB)     │                          │
│                    └─────────────┘                          │
└─────────────────────────────────────────────────────────────┘
```

## Schema

```sql
CREATE TABLE IF NOT EXISTS memories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    content TEXT NOT NULL,
    category TEXT,
    rule_name TEXT,
    embedding BLOB,              -- 384-dimensional float32 vector (1536 bytes)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Keep FTS5 for keyword search
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    content,
    category,
    content='memories',
    content_rowid='id'
);
```

## Embedder Package

**Location:** `internal/embedder/`

```go
//go:embed models/all-MiniLM-L6-v2.onnx
var modelData []byte

type Embedder struct {
    session *ort.Session
}

func New() (*Embedder, error)                      // Load model from embedded bytes
func (e *Embedder) Embed(text string) ([]float32, error)  // Returns 384-dim vector
func (e *Embedder) Close() error
```

**Model:** all-MiniLM-L6-v2
- Size: ~23MB ONNX
- Dimensions: 384
- Tokenizer: WordPiece (vocab bundled)

## API Changes

**RecallInput:**
```go
type RecallInput struct {
    Query    string `json:"query"`
    Category string `json:"category,omitempty"`
    Mode     string `json:"mode,omitempty"`  // "semantic" (default) | "keyword"
    Limit    int    `json:"limit,omitempty"` // max results, default 10
}
```

**MemoryResult:**
```go
type MemoryResult struct {
    ID       int64   `json:"id"`
    Content  string  `json:"content"`
    Category string  `json:"category,omitempty"`
    Score    float32 `json:"score,omitempty"`  // similarity score (0-1)
}
```

**Behavior:**
- `recall("invoices")` → semantic search (default)
- `recall("ACME-INV-", mode: "keyword")` → FTS5 exact match

## Implementation

**New files:**
- `internal/embedder/embedder.go` - ONNX wrapper
- `internal/embedder/tokenizer.go` - WordPiece tokenizer
- `internal/embedder/models/all-MiniLM-L6-v2.onnx` - Embedded model
- `internal/embedder/models/vocab.txt` - Tokenizer vocabulary

**Modified files:**
- `internal/memory/db.go` - Add embedding column, RecallSemantic()
- `internal/mcp/server.go` - Initialize embedder, handle mode parameter

**New dependencies:**
- `github.com/yalue/onnxruntime_go`

## Search Algorithm

```go
func (d *DB) RecallSemantic(queryEmbedding []float32, category string, limit int) ([]Memory, error) {
    // 1. Load all memories with embeddings (filtered by category if provided)
    // 2. Compute cosine similarity for each
    // 3. Sort by similarity descending
    // 4. Return top N
}

func cosineSimilarity(a, b []float32) float32 {
    var dot, normA, normB float32
    for i := range a {
        dot += a[i] * b[i]
        normA += a[i] * a[i]
        normB += b[i] * b[i]
    }
    return dot / (sqrt(normA) * sqrt(normB))
}
```

## Binary Size Impact

- Model: ~23MB
- ONNX runtime: ~5MB
- Total increase: ~28MB
