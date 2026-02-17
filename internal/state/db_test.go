// internal/state/db_test.go
package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ===== FR-5: State persistence / execution history =====

func TestOpen_CreatesDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-state.db")

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

func TestOpen_CreatesSchema(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// Verify execution_history table exists
	var tableName string
	err := db.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='execution_history'",
	).Scan(&tableName)
	if err != nil {
		t.Errorf("execution_history table not created: %v", err)
	}
	if tableName != "execution_history" {
		t.Errorf("expected table name 'execution_history', got %q", tableName)
	}
}

func TestOpen_CreatesSchemaVersion(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	var tableName string
	err := db.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='schema_version'",
	).Scan(&tableName)
	if err != nil {
		t.Errorf("schema_version table not created: %v", err)
	}
}

func TestOpen_CreatesIndexes(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	indexes := []string{
		"idx_execution_history_rule",
		"idx_execution_history_state",
		"idx_execution_history_started",
	}
	for _, name := range indexes {
		var indexName string
		err := db.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='index' AND name=?", name,
		).Scan(&indexName)
		if err != nil {
			t.Errorf("index %s not created: %v", name, err)
		}
	}
}

func TestOpen_CreatesParentDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir", "nested", "state.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created in nested directory")
	}
}

func TestRecordExecution(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	now := time.Now()
	rec := ExecutionRecord{
		RuleName:    "test-rule",
		TriggerType: "scheduled",
		State:       "success",
		StartedAt:   now.Add(-10 * time.Second),
		FinishedAt:  now,
		DurationMs:  10000,
		Output:      "Rule completed successfully",
		DryRun:      false,
	}

	id, err := db.RecordExecution(rec)
	if err != nil {
		t.Fatalf("RecordExecution() error = %v", err)
	}
	if id == 0 {
		t.Error("RecordExecution() returned id = 0, want > 0")
	}
}

func TestRecordExecution_WithError(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	now := time.Now()
	rec := ExecutionRecord{
		RuleName:    "failing-rule",
		TriggerType: "filesystem",
		State:       "failure",
		StartedAt:   now.Add(-5 * time.Second),
		FinishedAt:  now,
		DurationMs:  5000,
		Error:       "command not found: claude",
		Output:      "",
		DryRun:      false,
	}

	id, err := db.RecordExecution(rec)
	if err != nil {
		t.Fatalf("RecordExecution() error = %v", err)
	}
	if id == 0 {
		t.Error("RecordExecution() returned id = 0, want > 0")
	}
}

func TestRecordExecution_WithRetryAndParent(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	now := time.Now()

	// Record parent execution
	parent := ExecutionRecord{
		RuleName:    "parent-rule",
		TriggerType: "scheduled",
		State:       "success",
		StartedAt:   now.Add(-20 * time.Second),
		FinishedAt:  now.Add(-10 * time.Second),
		DurationMs:  10000,
	}
	parentID, err := db.RecordExecution(parent)
	if err != nil {
		t.Fatalf("RecordExecution(parent) error = %v", err)
	}

	// Record child execution triggered by parent
	child := ExecutionRecord{
		RuleName:               "child-rule",
		TriggerType:            "triggered",
		State:                  "success",
		StartedAt:              now.Add(-10 * time.Second),
		FinishedAt:             now,
		DurationMs:             10000,
		RetryAttempt:           2,
		TriggeredByExecutionID: parentID,
	}
	childID, err := db.RecordExecution(child)
	if err != nil {
		t.Fatalf("RecordExecution(child) error = %v", err)
	}
	if childID <= parentID {
		t.Errorf("child id (%d) should be > parent id (%d)", childID, parentID)
	}
}

func TestRecordExecution_DryRun(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	now := time.Now()
	rec := ExecutionRecord{
		RuleName:    "dry-run-rule",
		TriggerType: "scheduled",
		State:       "success",
		StartedAt:   now.Add(-3 * time.Second),
		FinishedAt:  now,
		DurationMs:  3000,
		DryRun:      true,
	}

	id, err := db.RecordExecution(rec)
	if err != nil {
		t.Fatalf("RecordExecution() error = %v", err)
	}
	if id == 0 {
		t.Error("RecordExecution() returned id = 0")
	}
}

func TestGetHistory_FilterByRule(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	now := time.Now()
	insertTestRecords(t, db, now)

	records, err := db.GetHistory("rule-a", "", 100)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}

	for _, r := range records {
		if r.RuleName != "rule-a" {
			t.Errorf("expected all records for rule-a, got rule_name=%q", r.RuleName)
		}
	}
	if len(records) == 0 {
		t.Error("GetHistory() returned no records for rule-a")
	}
}

func TestGetHistory_FilterByState(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	now := time.Now()
	insertTestRecords(t, db, now)

	records, err := db.GetHistory("", "failure", 100)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}

	for _, r := range records {
		if r.State != "failure" {
			t.Errorf("expected all records with state=failure, got state=%q", r.State)
		}
	}
	if len(records) == 0 {
		t.Error("GetHistory() returned no records with state=failure")
	}
}

func TestGetHistory_WithLimit(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	now := time.Now()
	insertTestRecords(t, db, now)

	records, err := db.GetHistory("", "", 2)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}

	if len(records) > 2 {
		t.Errorf("GetHistory() returned %d records, want <= 2", len(records))
	}
}

func TestGetHistory_EmptyResults(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	records, err := db.GetHistory("nonexistent-rule", "", 100)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(records) != 0 {
		t.Errorf("GetHistory() returned %d records for nonexistent rule, want 0", len(records))
	}
}

func TestGetLastState(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	now := time.Now()

	// Insert records in chronological order
	db.RecordExecution(ExecutionRecord{
		RuleName: "test-rule", TriggerType: "scheduled", State: "failure",
		StartedAt: now.Add(-30 * time.Second), FinishedAt: now.Add(-20 * time.Second),
		DurationMs: 10000,
	})
	db.RecordExecution(ExecutionRecord{
		RuleName: "test-rule", TriggerType: "scheduled", State: "success",
		StartedAt: now.Add(-10 * time.Second), FinishedAt: now,
		DurationMs: 10000,
	})

	state, err := db.GetLastState("test-rule")
	if err != nil {
		t.Fatalf("GetLastState() error = %v", err)
	}
	if state != "success" {
		t.Errorf("GetLastState() = %q, want %q", state, "success")
	}
}

func TestGetLastState_NoRecords(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	state, err := db.GetLastState("nonexistent")
	if err != nil {
		t.Fatalf("GetLastState() error = %v", err)
	}
	if state != "" {
		t.Errorf("GetLastState() for nonexistent rule = %q, want empty string", state)
	}
}

func TestCleanup(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	now := time.Now()

	// Insert old record (100 days ago)
	db.RecordExecution(ExecutionRecord{
		RuleName: "old-rule", TriggerType: "scheduled", State: "success",
		StartedAt: now.Add(-100 * 24 * time.Hour), FinishedAt: now.Add(-100 * 24 * time.Hour),
		DurationMs: 1000,
	})

	// Insert recent record (1 day ago)
	db.RecordExecution(ExecutionRecord{
		RuleName: "recent-rule", TriggerType: "scheduled", State: "success",
		StartedAt: now.Add(-24 * time.Hour), FinishedAt: now.Add(-24 * time.Hour),
		DurationMs: 1000,
	})

	// Cleanup records older than 90 days
	deleted, err := db.Cleanup(90)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted != 1 {
		t.Errorf("Cleanup() deleted %d records, want 1", deleted)
	}

	// Verify old record is gone
	records, _ := db.GetHistory("old-rule", "", 100)
	if len(records) != 0 {
		t.Error("Cleanup() did not remove old record")
	}

	// Verify recent record still exists
	records, _ = db.GetHistory("recent-rule", "", 100)
	if len(records) != 1 {
		t.Error("Cleanup() should not remove recent record")
	}
}

// ===== Helpers =====

func openTestDB(t *testing.T) *DB {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := Open(filepath.Join(tmpDir, "test-state.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return db
}

func insertTestRecords(t *testing.T, db *DB, now time.Time) {
	t.Helper()
	records := []ExecutionRecord{
		{
			RuleName: "rule-a", TriggerType: "scheduled", State: "success",
			StartedAt: now.Add(-60 * time.Second), FinishedAt: now.Add(-50 * time.Second),
			DurationMs: 10000,
		},
		{
			RuleName: "rule-a", TriggerType: "scheduled", State: "failure",
			StartedAt: now.Add(-40 * time.Second), FinishedAt: now.Add(-30 * time.Second),
			DurationMs: 10000, Error: "timeout",
		},
		{
			RuleName: "rule-b", TriggerType: "filesystem", State: "success",
			StartedAt: now.Add(-20 * time.Second), FinishedAt: now.Add(-10 * time.Second),
			DurationMs: 10000,
		},
		{
			RuleName: "rule-b", TriggerType: "filesystem", State: "failure",
			StartedAt: now.Add(-10 * time.Second), FinishedAt: now,
			DurationMs: 10000, Error: "file not found",
		},
	}
	for _, r := range records {
		if _, err := db.RecordExecution(r); err != nil {
			t.Fatalf("insertTestRecords: %v", err)
		}
	}
}
