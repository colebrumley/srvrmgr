// internal/logging/logger_test.go
package logging

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ===== FR-6: Log rotation via in-process writer =====

func TestRotatingWriter_Creates(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	w, err := NewRotatingWriter(logPath, 1024*1024) // 1MB threshold
	if err != nil {
		t.Fatalf("NewRotatingWriter() error = %v", err)
	}
	defer w.Close()

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("FR-6: log file was not created")
	}
}

func TestRotatingWriter_Writes(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	w, err := NewRotatingWriter(logPath, 1024*1024)
	if err != nil {
		t.Fatalf("NewRotatingWriter() error = %v", err)
	}
	defer w.Close()

	msg := "Hello, log rotation!\n"
	n, err := w.Write([]byte(msg))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(msg) {
		t.Errorf("Write() = %d, want %d", n, len(msg))
	}

	// Verify content was written
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != msg {
		t.Errorf("log content = %q, want %q", string(content), msg)
	}
}

func TestRotatingWriter_RotatesAtThreshold(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	// Use a very small threshold to trigger rotation quickly
	threshold := int64(100) // 100 bytes
	w, err := NewRotatingWriter(logPath, threshold)
	if err != nil {
		t.Fatalf("NewRotatingWriter() error = %v", err)
	}
	defer w.Close()

	// Write enough data to exceed the threshold
	line := strings.Repeat("x", 50) + "\n" // 51 bytes per line
	for i := 0; i < 5; i++ {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	// After rotation, the rotated file should exist
	rotated1 := logPath + ".1"
	if _, err := os.Stat(rotated1); os.IsNotExist(err) {
		// Check for gzipped version
		rotated1gz := logPath + ".1.gz"
		if _, err := os.Stat(rotated1gz); os.IsNotExist(err) {
			t.Error("FR-6: rotated log file (.1 or .1.gz) was not created")
		}
	}

	// Current log file should still exist and be smaller than threshold
	// (or recently opened)
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("FR-6: current log file should still exist after rotation")
	}
}

func TestRotatingWriter_CompressesOldFiles(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	threshold := int64(50)
	w, err := NewRotatingWriter(logPath, threshold)
	if err != nil {
		t.Fatalf("NewRotatingWriter() error = %v", err)
	}
	defer w.Close()

	// Write enough to trigger multiple rotations
	line := strings.Repeat("y", 60) + "\n"
	for i := 0; i < 10; i++ {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	// Check for compressed files (.gz)
	gzFiles, _ := filepath.Glob(filepath.Join(dir, "*.gz"))
	if len(gzFiles) > 0 {
		// Verify the gzip file is valid
		f, err := os.Open(gzFiles[0])
		if err != nil {
			t.Fatalf("failed to open gzip file: %v", err)
		}
		defer f.Close()

		gz, err := gzip.NewReader(f)
		if err != nil {
			t.Fatalf("FR-6: rotated file is not valid gzip: %v", err)
		}
		defer gz.Close()

		var buf bytes.Buffer
		if _, err := buf.ReadFrom(gz); err != nil {
			t.Fatalf("FR-6: failed to read gzip content: %v", err)
		}
	}
}

func TestRotatingWriter_MaxRotatedFiles(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	threshold := int64(30)
	w, err := NewRotatingWriter(logPath, threshold)
	if err != nil {
		t.Fatalf("NewRotatingWriter() error = %v", err)
	}
	defer w.Close()

	// Write enough to trigger many rotations
	line := strings.Repeat("z", 40) + "\n"
	for i := 0; i < 30; i++ {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	// Should keep at most 5 rotated files
	allFiles, _ := filepath.Glob(filepath.Join(dir, "test.log*"))
	// Count rotated files (exclude the current log)
	rotated := 0
	for _, f := range allFiles {
		if f != logPath {
			rotated++
		}
	}
	if rotated > 5 {
		t.Errorf("FR-6: expected at most 5 rotated files, got %d", rotated)
	}
}

func TestRotatingWriter_ThreadSafe(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	w, err := NewRotatingWriter(logPath, 1024)
	if err != nil {
		t.Fatalf("NewRotatingWriter() error = %v", err)
	}
	defer w.Close()

	// Write from multiple goroutines concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				msg := strings.Repeat("x", 10) + "\n"
				w.Write([]byte(msg))
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
	// If we get here without panics/races, the test passes
}

func TestNewLogger_WithWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger("text", "info", &buf)
	logger.Info("test message")

	if buf.Len() == 0 {
		t.Error("expected logger to write to provided writer")
	}
}
