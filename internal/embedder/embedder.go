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

// Embedder wraps the hugot feature extraction pipeline with lazy initialization
type Embedder struct {
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
	modelDir string
	mu       sync.Mutex
	initOnce sync.Once
	initErr  error
}

// New creates a new Embedder with lazy initialization
// The model is not loaded until the first Embed call
func New() (*Embedder, error) {
	return &Embedder{}, nil
}

// NewEager creates a new Embedder and loads the model immediately
func NewEager() (*Embedder, error) {
	e := &Embedder{}
	if err := e.init(); err != nil {
		return nil, err
	}
	return e, nil
}

// init performs the actual model initialization
func (e *Embedder) init() error {
	e.initOnce.Do(func() {
		// Create temp directory for model files
		modelDir, err := os.MkdirTemp("", "srvrmgr-embedder-*")
		if err != nil {
			e.initErr = fmt.Errorf("creating temp directory: %w", err)
			return
		}
		e.modelDir = modelDir

		// Extract embedded model files
		if err := extractModelFiles(modelDir); err != nil {
			os.RemoveAll(modelDir)
			e.initErr = fmt.Errorf("extracting model files: %w", err)
			return
		}

		// Create hugot session with pure Go backend
		session, err := hugot.NewGoSession()
		if err != nil {
			os.RemoveAll(modelDir)
			e.initErr = fmt.Errorf("creating hugot session: %w", err)
			return
		}
		e.session = session

		// Create feature extraction pipeline
		config := hugot.FeatureExtractionConfig{
			ModelPath: modelDir,
			Name:      "embeddings",
		}
		pipeline, err := hugot.NewPipeline(session, config)
		if err != nil {
			session.Destroy()
			os.RemoveAll(modelDir)
			e.initErr = fmt.Errorf("creating pipeline: %w", err)
			return
		}
		e.pipeline = pipeline
	})
	return e.initErr
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
	// Lazy initialization
	if err := e.init(); err != nil {
		return nil, err
	}

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
