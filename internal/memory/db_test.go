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

func TestOpenDBCreatesFTSTable(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	var tableName string
	err = db.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='memories_fts'").Scan(&tableName)
	if err != nil {
		t.Errorf("memories_fts table not created: %v", err)
	}
}

func TestOpenDBCreatesTriggers(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	triggers := []string{"memories_ai", "memories_ad", "memories_au"}
	for _, name := range triggers {
		var triggerName string
		err = db.db.QueryRow("SELECT name FROM sqlite_master WHERE type='trigger' AND name=?", name).Scan(&triggerName)
		if err != nil {
			t.Errorf("trigger %s not created: %v", name, err)
		}
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

func TestOpenDBCreatesEmbeddingColumn(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// Verify embedding column exists by inserting with it
	_, err := db.db.Exec("INSERT INTO memories (content, embedding) VALUES (?, ?)", "test", []byte{1, 2, 3, 4})
	if err != nil {
		t.Errorf("embedding column not created: %v", err)
	}
}

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
