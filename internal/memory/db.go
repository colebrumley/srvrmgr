// internal/memory/db.go
package memory

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
