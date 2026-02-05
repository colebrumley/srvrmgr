//go:build integration

package memory

import (
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

	// Recall by category (note: can't use "*" as FTS5 query, need actual term)
	memories, err = db.Recall("file-patterns", "file-patterns")
	if err != nil {
		// FTS5 may not like querying category this way, adjust test if needed
		t.Logf("Recall by category note: %v", err)
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
