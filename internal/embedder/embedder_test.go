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
