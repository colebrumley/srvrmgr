// internal/mcp/server_test.go
package mcp

import (
	"context"
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

func TestToolHandlers(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	server, err := NewServer(dbPath)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer server.Close()

	ctx := context.Background()

	// Test remember
	t.Run("remember", func(t *testing.T) {
		_, output, err := server.handleRemember(ctx, nil, RememberInput{
			Content:  "test content",
			Category: "test-category",
		})
		if err != nil {
			t.Fatalf("handleRemember() error = %v", err)
		}
		if output.ID <= 0 {
			t.Errorf("handleRemember() returned invalid ID: %d", output.ID)
		}
	})

	// Test recall
	t.Run("recall", func(t *testing.T) {
		_, output, err := server.handleRecall(ctx, nil, RecallInput{
			Query: "test",
		})
		if err != nil {
			t.Fatalf("handleRecall() error = %v", err)
		}
		if output.Count != 1 {
			t.Errorf("handleRecall() count = %d, want 1", output.Count)
		}
		if len(output.Memories) != 1 {
			t.Errorf("handleRecall() memories len = %d, want 1", len(output.Memories))
		}
		if output.Memories[0].Content != "test content" {
			t.Errorf("handleRecall() content = %q, want %q", output.Memories[0].Content, "test content")
		}
	})

	// Test recall with category filter
	t.Run("recall with category", func(t *testing.T) {
		_, output, err := server.handleRecall(ctx, nil, RecallInput{
			Query:    "test",
			Category: "test-category",
		})
		if err != nil {
			t.Fatalf("handleRecall() error = %v", err)
		}
		if output.Count != 1 {
			t.Errorf("handleRecall() with category count = %d, want 1", output.Count)
		}

		// Test with wrong category
		_, output, err = server.handleRecall(ctx, nil, RecallInput{
			Query:    "test",
			Category: "wrong-category",
		})
		if err != nil {
			t.Fatalf("handleRecall() error = %v", err)
		}
		if output.Count != 0 {
			t.Errorf("handleRecall() with wrong category count = %d, want 0", output.Count)
		}
	})

	// Test forget
	t.Run("forget", func(t *testing.T) {
		// First get the ID
		_, recallOut, _ := server.handleRecall(ctx, nil, RecallInput{Query: "test"})
		if len(recallOut.Memories) == 0 {
			t.Fatal("no memories to forget")
		}
		memID := recallOut.Memories[0].ID

		_, output, err := server.handleForget(ctx, nil, ForgetInput{ID: memID})
		if err != nil {
			t.Fatalf("handleForget() error = %v", err)
		}
		if output.Message == "" {
			t.Error("handleForget() returned empty message")
		}

		// Verify it's gone
		_, recallOut, _ = server.handleRecall(ctx, nil, RecallInput{Query: "test"})
		if recallOut.Count != 0 {
			t.Errorf("memory still exists after forget, count = %d", recallOut.Count)
		}
	})

	// Test forget non-existent
	t.Run("forget non-existent", func(t *testing.T) {
		_, _, err := server.handleForget(ctx, nil, ForgetInput{ID: 99999})
		if err == nil {
			t.Error("handleForget() should return error for non-existent ID")
		}
	})
}

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
