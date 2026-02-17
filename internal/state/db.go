// internal/state/db.go
// FR-5: State persistence / execution history
// This is a stub — implementation will be added during the implementation phase.
package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ExecutionRecord represents a single rule execution in the history.
type ExecutionRecord struct {
	ID                     int64
	RuleName               string
	TriggerType            string
	State                  string // success, failure, timeout, cancelled
	StartedAt              time.Time
	FinishedAt             time.Time
	DurationMs             int64
	RetryAttempt           int
	TriggeredByExecutionID int64
	EventData              string // JSON-serialized, max 1KB
	Error                  string
	Output                 string // truncated to 10KB, scrubbed of secrets
	DryRun                 bool
}

// DB wraps the SQLite database connection for execution history.
type DB struct {
	db *sql.DB
}

const stateSchema = `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS execution_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_name TEXT NOT NULL,
    trigger_type TEXT NOT NULL,
    state TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    finished_at DATETIME NOT NULL,
    duration_ms INTEGER NOT NULL,
    retry_attempt INTEGER DEFAULT 0,
    triggered_by_execution_id INTEGER REFERENCES execution_history(id),
    event_data TEXT,
    error TEXT,
    output TEXT,
    dry_run BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_execution_history_rule ON execution_history(rule_name);
CREATE INDEX IF NOT EXISTS idx_execution_history_state ON execution_history(state);
CREATE INDEX IF NOT EXISTS idx_execution_history_started ON execution_history(started_at);
`

// Open opens or creates a state database at the given path.
func Open(path string) (*DB, error) {
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

	if _, err := db.Exec(stateSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing schema: %w", err)
	}

	// Insert schema version if not present
	var count int
	db.QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&count)
	if count == 0 {
		db.Exec("INSERT INTO schema_version (version) VALUES (1)")
	}

	return &DB{db: db}, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// RecordExecution stores an execution record and returns its ID.
func (d *DB) RecordExecution(rec ExecutionRecord) (int64, error) {
	var triggeredBy *int64
	if rec.TriggeredByExecutionID > 0 {
		triggeredBy = &rec.TriggeredByExecutionID
	}

	result, err := d.db.Exec(`
		INSERT INTO execution_history
		(rule_name, trigger_type, state, started_at, finished_at, duration_ms,
		 retry_attempt, triggered_by_execution_id, event_data, error, output, dry_run)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.RuleName, rec.TriggerType, rec.State, rec.StartedAt, rec.FinishedAt,
		rec.DurationMs, rec.RetryAttempt, triggeredBy, rec.EventData,
		rec.Error, rec.Output, rec.DryRun,
	)
	if err != nil {
		return 0, fmt.Errorf("recording execution: %w", err)
	}
	return result.LastInsertId()
}

// GetHistory retrieves execution history filtered by rule name and/or state.
func (d *DB) GetHistory(ruleName, state string, limit int) ([]ExecutionRecord, error) {
	query := "SELECT id, rule_name, trigger_type, state, started_at, finished_at, duration_ms, retry_attempt, error, output, dry_run FROM execution_history WHERE 1=1"
	var args []any

	if ruleName != "" {
		query += " AND rule_name = ?"
		args = append(args, ruleName)
	}
	if state != "" {
		query += " AND state = ?"
		args = append(args, state)
	}

	query += " ORDER BY started_at DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying history: %w", err)
	}
	defer rows.Close()

	var records []ExecutionRecord
	for rows.Next() {
		var r ExecutionRecord
		var errStr, output sql.NullString
		if err := rows.Scan(&r.ID, &r.RuleName, &r.TriggerType, &r.State,
			&r.StartedAt, &r.FinishedAt, &r.DurationMs, &r.RetryAttempt,
			&errStr, &output, &r.DryRun); err != nil {
			return nil, fmt.Errorf("scanning record: %w", err)
		}
		r.Error = errStr.String
		r.Output = output.String
		records = append(records, r)
	}
	return records, rows.Err()
}

// GetLastState returns the most recent execution state for a rule.
func (d *DB) GetLastState(ruleName string) (string, error) {
	var state sql.NullString
	err := d.db.QueryRow(
		"SELECT state FROM execution_history WHERE rule_name = ? ORDER BY started_at DESC LIMIT 1",
		ruleName,
	).Scan(&state)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("getting last state: %w", err)
	}
	return state.String, nil
}

// Cleanup removes execution records older than the specified number of days.
func (d *DB) Cleanup(retentionDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result, err := d.db.Exec(
		"DELETE FROM execution_history WHERE started_at < ?", cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("cleaning up history: %w", err)
	}
	return result.RowsAffected()
}
