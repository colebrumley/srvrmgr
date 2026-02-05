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
