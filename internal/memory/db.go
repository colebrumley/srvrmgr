// internal/memory/db.go
package memory

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a memory is not found
var ErrNotFound = errors.New("memory not found")

// Memory represents a stored memory
type Memory struct {
	ID        int64
	Content   string
	Category  string
	RuleName  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MemoryWithScore represents a memory with its similarity score
type MemoryWithScore struct {
	Memory
	Score float32
}

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

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting to database: %w", err)
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

// Recall searches memories using full-text search with optional category filter
func (d *DB) Recall(query, category string) ([]Memory, error) {
	var rows *sql.Rows
	var err error

	// Escape FTS5 special syntax by wrapping in double quotes
	escapedQuery := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`

	if category != "" {
		rows, err = d.db.Query(`
			SELECT m.id, m.content, m.category, m.rule_name, m.created_at, m.updated_at
			FROM memories m
			JOIN memories_fts fts ON m.id = fts.rowid
			WHERE memories_fts MATCH ? AND m.category = ?
			ORDER BY rank
		`, escapedQuery, category)
	} else {
		rows, err = d.db.Query(`
			SELECT m.id, m.content, m.category, m.rule_name, m.created_at, m.updated_at
			FROM memories m
			JOIN memories_fts fts ON m.id = fts.rowid
			WHERE memories_fts MATCH ?
			ORDER BY rank
		`, escapedQuery)
	}
	if err != nil {
		return nil, fmt.Errorf("querying memories: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		var cat, ruleName sql.NullString
		if err := rows.Scan(&m.ID, &m.Content, &cat, &ruleName, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning memory: %w", err)
		}
		m.Category = cat.String
		m.RuleName = ruleName.String
		memories = append(memories, m)
	}
	return memories, rows.Err()
}

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

// bytesToFloat32Slice converts bytes back to float32 slice.
// Returns nil if the byte length is not a valid multiple of 4.
func bytesToFloat32Slice(bytes []byte) []float32 {
	if len(bytes)%4 != 0 {
		return nil
	}
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
		if embedding == nil {
			continue // corrupted embedding data
		}
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
