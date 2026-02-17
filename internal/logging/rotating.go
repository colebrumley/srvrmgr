// internal/logging/rotating.go
// FR-6: Log rotation via in-process writer
// Stub — to be fully implemented during the implementation phase.
package logging

import (
	"compress/gzip"
	"fmt"
	"os"
	"sync"
)

// RotatingWriter implements io.Writer with automatic log rotation.
// Rotates when file exceeds maxSize bytes. Keeps up to 5 rotated files.
type RotatingWriter struct {
	path    string
	maxSize int64
	file    *os.File
	size    int64
	mu      sync.Mutex
}

// NewRotatingWriter creates a new rotating writer.
func NewRotatingWriter(path string, maxSize int64) (*RotatingWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening log file: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat log file: %w", err)
	}

	return &RotatingWriter{
		path:    path,
		maxSize: maxSize,
		file:    f,
		size:    info.Size(),
	}, nil
}

// Write implements io.Writer.
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, fmt.Errorf("rotating log: %w", err)
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Close closes the writer.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

func (w *RotatingWriter) rotate() error {
	w.file.Close()

	// Shift existing rotated files: .5 -> delete, .4 -> .5, ... .1 -> .2
	for i := 5; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d.gz", w.path, i)
		if i == 5 {
			os.Remove(old)
			// Also try uncompressed
			os.Remove(fmt.Sprintf("%s.%d", w.path, i))
		} else {
			newName := fmt.Sprintf("%s.%d.gz", w.path, i+1)
			os.Rename(old, newName)
			// Also handle uncompressed
			os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1))
		}
	}

	// Compress current file to .1.gz
	src := w.path
	dst := w.path + ".1.gz"
	if err := compressFile(src, dst); err != nil {
		// If compression fails, just rename
		os.Rename(src, w.path+".1")
	} else {
		os.Remove(src) // remove original after successful compression
	}

	// Open new log file
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	w.file = f
	w.size = 0
	return nil
}

func compressFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	defer gz.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			if _, werr := gz.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			break
		}
	}
	return nil
}
