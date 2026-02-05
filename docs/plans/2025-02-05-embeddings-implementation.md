# Embeddings Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add local semantic search to the memory layer using embedded all-MiniLM-L6-v2 model via hugot library.

**Architecture:** Use knights-analytics/hugot for ONNX inference with pure Go backend. Embed model files via go:embed, extract to temp dir at runtime. Store embeddings as BLOBs, compute cosine similarity in Go.

**Tech Stack:** Go, hugot (github.com/knights-analytics/hugot), SQLite, go:embed

---

## Task 1: Add hugot Dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add hugot dependency**

Run:
```bash
go get github.com/knights-analytics/hugot
```

**Step 2: Verify dependency installed**

Run: `go mod tidy && grep hugot go.mod`

Expected: `github.com/knights-analytics/hugot` in require block

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add hugot dependency for embeddings"
```

---

## Task 2: Download and Embed Model Files

**Files:**
- Create: `internal/embedder/models/` directory
- Create: `internal/embedder/embed.go`

**Step 1: Download model files from HuggingFace**

Run:
```bash
mkdir -p internal/embedder/models

# Download quantized ONNX model (pick one based on platform, avx2 is most compatible)
curl -L "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model_quint8_avx2.onnx" -o internal/embedder/models/model.onnx

# Download tokenizer and config files
curl -L "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/tokenizer.json" -o internal/embedder/models/tokenizer.json
curl -L "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/config.json" -o internal/embedder/models/config.json
curl -L "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/special_tokens_map.json" -o internal/embedder/models/special_tokens_map.json
curl -L "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/tokenizer_config.json" -o internal/embedder/models/tokenizer_config.json

# Download pooling config
mkdir -p internal/embedder/models/1_Pooling
curl -L "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/1_Pooling/config.json" -o internal/embedder/models/1_Pooling/config.json
```

**Step 2: Create embed.go with go:embed directive**

```go
// internal/embedder/embed.go
package embedder

import "embed"

//go:embed models/*
//go:embed models/1_Pooling/*
var modelFS embed.FS
```

**Step 3: Verify files exist**

Run: `ls -la internal/embedder/models/`

Expected: model.onnx (~23MB), tokenizer.json, config.json, etc.

**Step 4: Commit**

```bash
git add internal/embedder/
git commit -m "feat(embedder): add embedded model files"
```

---

## Task 3: Create Embedder Package

**Files:**
- Create: `internal/embedder/embedder.go`
- Create: `internal/embedder/embedder_test.go`

**Step 1: Write failing test**

```go
// internal/embedder/embedder_test.go
package embedder

import (
	"testing"
)

func TestNewEmbedder(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer e.Close()

	if e == nil {
		t.Error("New() returned nil")
	}
}

func TestEmbed(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer e.Close()

	embedding, err := e.Embed("Hello world")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	// all-MiniLM-L6-v2 produces 384-dimensional embeddings
	if len(embedding) != 384 {
		t.Errorf("Embed() returned %d dimensions, want 384", len(embedding))
	}
}

func TestEmbedBatch(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer e.Close()

	texts := []string{"Hello world", "How are you?"}
	embeddings, err := e.EmbedBatch(texts)
	if err != nil {
		t.Fatalf("EmbedBatch() error = %v", err)
	}

	if len(embeddings) != 2 {
		t.Errorf("EmbedBatch() returned %d embeddings, want 2", len(embeddings))
	}

	for i, emb := range embeddings {
		if len(emb) != 384 {
			t.Errorf("embeddings[%d] has %d dimensions, want 384", i, len(emb))
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/embedder/... -v`

Expected: FAIL (functions not defined)

**Step 3: Write implementation**

```go
// internal/embedder/embedder.go
package embedder

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

// Embedder wraps the hugot feature extraction pipeline
type Embedder struct {
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
	modelDir string
	mu       sync.Mutex
}

// New creates a new Embedder, extracting embedded model files to a temp directory
func New() (*Embedder, error) {
	// Create temp directory for model files
	modelDir, err := os.MkdirTemp("", "srvrmgr-embedder-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}

	// Extract embedded model files
	if err := extractModelFiles(modelDir); err != nil {
		os.RemoveAll(modelDir)
		return nil, fmt.Errorf("extracting model files: %w", err)
	}

	// Create hugot session with pure Go backend
	session, err := hugot.NewGoSession()
	if err != nil {
		os.RemoveAll(modelDir)
		return nil, fmt.Errorf("creating hugot session: %w", err)
	}

	// Create feature extraction pipeline
	config := hugot.FeatureExtractionConfig{
		ModelPath: modelDir,
		Name:      "embeddings",
	}
	pipeline, err := hugot.NewPipeline(session, config)
	if err != nil {
		session.Destroy()
		os.RemoveAll(modelDir)
		return nil, fmt.Errorf("creating pipeline: %w", err)
	}

	return &Embedder{
		session:  session,
		pipeline: pipeline,
		modelDir: modelDir,
	}, nil
}

// extractModelFiles extracts embedded model files to the target directory
func extractModelFiles(targetDir string) error {
	return fs.WalkDir(modelFS, "models", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Calculate target path (strip "models/" prefix)
		relPath, err := filepath.Rel("models", path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		// Read and write file
		data, err := modelFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		return os.WriteFile(targetPath, data, 0644)
	})
}

// Embed generates an embedding for a single text
func (e *Embedder) Embed(text string) ([]float32, error) {
	embeddings, err := e.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts
func (e *Embedder) EmbedBatch(texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	result, err := e.pipeline.RunPipeline(texts)
	if err != nil {
		return nil, fmt.Errorf("running pipeline: %w", err)
	}

	return result.Embeddings, nil
}

// Close releases resources
func (e *Embedder) Close() error {
	if e.session != nil {
		e.session.Destroy()
	}
	if e.modelDir != "" {
		os.RemoveAll(e.modelDir)
	}
	return nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/embedder/... -v`

Expected: PASS (may take a moment on first run as model loads)

**Step 5: Commit**

```bash
git add internal/embedder/
git commit -m "feat(embedder): implement embedder with hugot"
```

---

## Task 4: Update Memory Schema

**Files:**
- Modify: `internal/memory/db.go`
- Modify: `internal/memory/db_test.go`

**Step 1: Update schema constant**

In `internal/memory/db.go`, update the schema to add embedding column:

```go
const schema = `
CREATE TABLE IF NOT EXISTS memories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    content TEXT NOT NULL,
    category TEXT,
    rule_name TEXT,
    embedding BLOB,
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
```

**Step 2: Add test for embedding column**

Add to `internal/memory/db_test.go`:

```go
func TestOpenDBCreatesEmbeddingColumn(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// Verify embedding column exists by inserting with it
	_, err := db.db.Exec("INSERT INTO memories (content, embedding) VALUES (?, ?)", "test", []byte{1, 2, 3, 4})
	if err != nil {
		t.Errorf("embedding column not created: %v", err)
	}
}
```

**Step 3: Run tests**

Run: `go test ./internal/memory/... -v`

Expected: PASS

**Step 4: Commit**

```bash
git add internal/memory/
git commit -m "feat(memory): add embedding column to schema"
```

---

## Task 5: Update Remember to Store Embeddings

**Files:**
- Modify: `internal/memory/db.go`
- Modify: `internal/memory/db_test.go`

**Step 1: Update Remember signature and implementation**

```go
// RememberWithEmbedding stores a new memory with its embedding and returns its ID
func (d *DB) RememberWithEmbedding(content, category, ruleName string, embedding []float32) (int64, error) {
	var embeddingBytes []byte
	if embedding != nil {
		embeddingBytes = float32SliceToBytes(embedding)
	}

	result, err := d.db.Exec(
		"INSERT INTO memories (content, category, rule_name, embedding) VALUES (?, ?, ?, ?)",
		content, category, ruleName, embeddingBytes,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting memory: %w", err)
	}
	return result.LastInsertId()
}

// float32SliceToBytes converts a float32 slice to bytes
func float32SliceToBytes(floats []float32) []byte {
	bytes := make([]byte, len(floats)*4)
	for i, f := range floats {
		bits := math.Float32bits(f)
		bytes[i*4] = byte(bits)
		bytes[i*4+1] = byte(bits >> 8)
		bytes[i*4+2] = byte(bits >> 16)
		bytes[i*4+3] = byte(bits >> 24)
	}
	return bytes
}

// bytesToFloat32Slice converts bytes back to float32 slice
func bytesToFloat32Slice(bytes []byte) []float32 {
	floats := make([]float32, len(bytes)/4)
	for i := range floats {
		bits := uint32(bytes[i*4]) |
			uint32(bytes[i*4+1])<<8 |
			uint32(bytes[i*4+2])<<16 |
			uint32(bytes[i*4+3])<<24
		floats[i] = math.Float32frombits(bits)
	}
	return floats
}
```

Add import: `"math"`

**Step 2: Add test**

```go
func TestRememberWithEmbedding(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	embedding := make([]float32, 384)
	for i := range embedding {
		embedding[i] = float32(i) / 384.0
	}

	id, err := db.RememberWithEmbedding("test content", "test-category", "test-rule", embedding)
	if err != nil {
		t.Fatalf("RememberWithEmbedding() error = %v", err)
	}
	if id == 0 {
		t.Error("RememberWithEmbedding() returned id = 0, want > 0")
	}
}
```

**Step 3: Run tests**

Run: `go test ./internal/memory/... -v`

Expected: PASS

**Step 4: Commit**

```bash
git add internal/memory/
git commit -m "feat(memory): add RememberWithEmbedding for storing embeddings"
```

---

## Task 6: Add Semantic Recall

**Files:**
- Modify: `internal/memory/db.go`
- Modify: `internal/memory/db_test.go`

**Step 1: Add RecallSemantic method**

```go
// MemoryWithScore represents a memory with its similarity score
type MemoryWithScore struct {
	Memory
	Score float32
}

// RecallSemantic searches memories using cosine similarity
func (d *DB) RecallSemantic(queryEmbedding []float32, category string, limit int) ([]MemoryWithScore, error) {
	if limit <= 0 {
		limit = 10
	}

	var rows *sql.Rows
	var err error

	if category != "" {
		rows, err = d.db.Query(`
			SELECT id, content, category, rule_name, embedding, created_at, updated_at
			FROM memories
			WHERE embedding IS NOT NULL AND category = ?
		`, category)
	} else {
		rows, err = d.db.Query(`
			SELECT id, content, category, rule_name, embedding, created_at, updated_at
			FROM memories
			WHERE embedding IS NOT NULL
		`)
	}
	if err != nil {
		return nil, fmt.Errorf("querying memories: %w", err)
	}
	defer rows.Close()

	var results []MemoryWithScore
	for rows.Next() {
		var m Memory
		var cat, ruleName sql.NullString
		var embeddingBytes []byte

		if err := rows.Scan(&m.ID, &m.Content, &cat, &ruleName, &embeddingBytes, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning memory: %w", err)
		}
		m.Category = cat.String
		m.RuleName = ruleName.String

		if len(embeddingBytes) == 0 {
			continue
		}

		embedding := bytesToFloat32Slice(embeddingBytes)
		score := cosineSimilarity(queryEmbedding, embedding)

		results = append(results, MemoryWithScore{Memory: m, Score: score})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Limit results
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// cosineSimilarity computes cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
```

Add import: `"sort"`

**Step 2: Add test**

```go
func TestRecallSemantic(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// Create embeddings that are similar and different
	embedding1 := make([]float32, 384)
	embedding2 := make([]float32, 384)
	embedding3 := make([]float32, 384)

	for i := range embedding1 {
		embedding1[i] = float32(i) / 384.0       // Similar to query
		embedding2[i] = float32(i) / 384.0 + 0.1 // Also similar
		embedding3[i] = float32(384-i) / 384.0   // Different (reversed)
	}

	db.RememberWithEmbedding("similar content 1", "test", "", embedding1)
	db.RememberWithEmbedding("similar content 2", "test", "", embedding2)
	db.RememberWithEmbedding("different content", "test", "", embedding3)

	// Query with embedding similar to embedding1
	queryEmbedding := make([]float32, 384)
	for i := range queryEmbedding {
		queryEmbedding[i] = float32(i) / 384.0
	}

	results, err := db.RecallSemantic(queryEmbedding, "", 10)
	if err != nil {
		t.Fatalf("RecallSemantic() error = %v", err)
	}

	if len(results) != 3 {
		t.Errorf("RecallSemantic() returned %d results, want 3", len(results))
	}

	// First result should be most similar (embedding1)
	if results[0].Content != "similar content 1" {
		t.Errorf("First result should be 'similar content 1', got '%s'", results[0].Content)
	}

	// Scores should be descending
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("Results not sorted by score: %f > %f", results[i].Score, results[i-1].Score)
		}
	}
}
```

**Step 3: Run tests**

Run: `go test ./internal/memory/... -v`

Expected: PASS

**Step 4: Commit**

```bash
git add internal/memory/
git commit -m "feat(memory): add RecallSemantic for similarity search"
```

---

## Task 7: Update MCP Server

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/server_test.go`

**Step 1: Update Server struct to include embedder**

```go
import (
	"github.com/colebrumley/srvrmgr/internal/embedder"
)

type Server struct {
	db       *memory.DB
	embedder *embedder.Embedder
	server   *mcp.Server
}
```

**Step 2: Update NewServer to initialize embedder**

```go
func NewServer(dbPath string) (*Server, error) {
	db, err := memory.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening memory database: %w", err)
	}

	emb, err := embedder.New()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("creating embedder: %w", err)
	}

	s := &Server{db: db, embedder: emb}
	// ... rest of initialization
}
```

**Step 3: Update Close to close embedder**

```go
func (s *Server) Close() error {
	if s.embedder != nil {
		s.embedder.Close()
	}
	return s.db.Close()
}
```

**Step 4: Update RecallInput to include Mode and Limit**

```go
type RecallInput struct {
	Query    string `json:"query" jsonschema:"Search terms"`
	Category string `json:"category,omitempty" jsonschema:"Optional category filter"`
	Mode     string `json:"mode,omitempty" jsonschema:"Search mode: semantic (default) or keyword"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max results (default 10)"`
}
```

**Step 5: Update MemoryResult to include Score**

```go
type MemoryResult struct {
	ID       int64   `json:"id"`
	Content  string  `json:"content"`
	Category string  `json:"category,omitempty"`
	Score    float32 `json:"score,omitempty"`
}
```

**Step 6: Update handleRemember to generate and store embedding**

```go
func (s *Server) handleRemember(ctx context.Context, req *mcp.CallToolRequest, input RememberInput) (*mcp.CallToolResult, RememberOutput, error) {
	// Generate embedding
	embedding, err := s.embedder.Embed(input.Content)
	if err != nil {
		// Log warning but continue without embedding
		embedding = nil
	}

	id, err := s.db.RememberWithEmbedding(input.Content, input.Category, "", embedding)
	if err != nil {
		return nil, RememberOutput{}, fmt.Errorf("failed to store memory: %w", err)
	}
	return nil, RememberOutput{
		ID:      id,
		Message: fmt.Sprintf("Stored memory with ID %d", id),
	}, nil
}
```

**Step 7: Update handleRecall to support both modes**

```go
func (s *Server) handleRecall(ctx context.Context, req *mcp.CallToolRequest, input RecallInput) (*mcp.CallToolResult, RecallOutput, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}

	mode := input.Mode
	if mode == "" {
		mode = "semantic"
	}

	var results []MemoryResult

	if mode == "keyword" {
		// Use FTS5 keyword search
		memories, err := s.db.Recall(input.Query, input.Category)
		if err != nil {
			return nil, RecallOutput{}, fmt.Errorf("failed to search memories: %w", err)
		}
		for _, m := range memories {
			results = append(results, MemoryResult{
				ID:       m.ID,
				Content:  m.Content,
				Category: m.Category,
			})
		}
	} else {
		// Use semantic search
		queryEmbedding, err := s.embedder.Embed(input.Query)
		if err != nil {
			return nil, RecallOutput{}, fmt.Errorf("failed to embed query: %w", err)
		}

		memories, err := s.db.RecallSemantic(queryEmbedding, input.Category, limit)
		if err != nil {
			return nil, RecallOutput{}, fmt.Errorf("failed to search memories: %w", err)
		}
		for _, m := range memories {
			results = append(results, MemoryResult{
				ID:       m.ID,
				Content:  m.Content,
				Category: m.Category,
				Score:    m.Score,
			})
		}
	}

	return nil, RecallOutput{
		Memories: results,
		Count:    len(results),
	}, nil
}
```

**Step 8: Run tests**

Run: `go test ./internal/mcp/... -v`

Expected: PASS

**Step 9: Commit**

```bash
git add internal/mcp/
git commit -m "feat(mcp): integrate embedder for semantic search"
```

---

## Task 8: Update Tests and Final Verification

**Files:**
- Modify: `internal/mcp/server_test.go`

**Step 1: Update server tests to account for embedder**

The existing tests should still pass, but may take longer due to model loading. Add a test for semantic recall:

```go
func TestSemanticRecall(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	server, err := NewServer(dbPath)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer server.Close()

	ctx := context.Background()

	// Remember some content
	server.handleRemember(ctx, nil, RememberInput{
		Content:  "The API returns timestamps in UTC format",
		Category: "api-behaviors",
	})
	server.handleRemember(ctx, nil, RememberInput{
		Content:  "Database queries should use prepared statements",
		Category: "best-practices",
	})

	// Semantic search for time-related content
	_, output, err := server.handleRecall(ctx, nil, RecallInput{
		Query: "time format",
		Mode:  "semantic",
	})
	if err != nil {
		t.Fatalf("handleRecall() error = %v", err)
	}

	if output.Count == 0 {
		t.Error("Expected to find memories with semantic search")
	}

	// The timestamp memory should rank higher for "time format" query
	if output.Count > 0 && output.Memories[0].Score == 0 {
		t.Error("Expected non-zero similarity score")
	}
}
```

**Step 2: Run all tests**

Run: `go test ./... -v`

Expected: All PASS

**Step 3: Build and verify binary size**

Run: `go build -o /tmp/srvrmgrd ./cmd/srvrmgrd && ls -lh /tmp/srvrmgrd`

Expected: Binary ~40-50MB (includes embedded model)

**Step 4: Test MCP server manually**

Run: `claude mcp list`

Expected: srvrmgr-memory shows as connected

**Step 5: Final commit**

```bash
git add -A
git commit -m "test(mcp): add semantic recall test"
```

---

## Summary

This plan implements:

1. **hugot dependency** - Pure Go ONNX inference
2. **Embedded model files** - all-MiniLM-L6-v2 quantized (~23MB)
3. **Embedder package** - Wraps hugot with temp file extraction
4. **Schema update** - Add embedding BLOB column
5. **Remember update** - Generate and store embeddings
6. **Semantic recall** - Cosine similarity search
7. **MCP integration** - Mode parameter for semantic/keyword search

Total tasks: 8
Testing approach: TDD throughout
