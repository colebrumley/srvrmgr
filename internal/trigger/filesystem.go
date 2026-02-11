// internal/trigger/filesystem.go
package trigger

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
	"github.com/fsnotify/fsnotify"
)

// Filesystem watches directories for file events
type Filesystem struct {
	ruleName        string
	watchPaths      []string
	onEvents        map[string]bool
	ignorePatterns  []string
	debounceSeconds int
	watcher         *fsnotify.Watcher
	mu              sync.Mutex
	pending         map[string]*time.Timer
}

// NewFilesystem creates a new filesystem trigger
func NewFilesystem(ruleName string, cfg config.Trigger) (*Filesystem, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	onEvents := make(map[string]bool)
	for _, e := range cfg.OnEvents {
		onEvents[e] = true
	}

	// Expand ~ in paths
	var watchPaths []string
	for _, p := range cfg.WatchPaths {
		watchPaths = append(watchPaths, expandHome(p))
	}

	return &Filesystem{
		ruleName:        ruleName,
		watchPaths:      watchPaths,
		onEvents:        onEvents,
		ignorePatterns:  cfg.IgnorePatterns,
		debounceSeconds: cfg.DebounceSeconds,
		watcher:         watcher,
		pending:         make(map[string]*time.Timer),
	}, nil
}

func (f *Filesystem) RuleName() string {
	return f.ruleName
}

func (f *Filesystem) Start(ctx context.Context, events chan<- Event) error {
	// Add watch paths
	for _, path := range f.watchPaths {
		if err := f.watcher.Add(path); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-f.watcher.Events:
			if !ok {
				return nil
			}
			f.handleEvent(event, events)
		case err, ok := <-f.watcher.Errors:
			if !ok {
				return nil
			}
			// Log error but continue
			_ = err
		}
	}
}

func (f *Filesystem) Stop() error {
	// Cancel all pending debounce timers to prevent goroutine leaks
	f.mu.Lock()
	for path, timer := range f.pending {
		timer.Stop()
		delete(f.pending, path)
	}
	f.mu.Unlock()

	return f.watcher.Close()
}

func (f *Filesystem) handleEvent(fsEvent fsnotify.Event, events chan<- Event) {
	// Determine event type
	var eventType string
	switch {
	case fsEvent.Op&fsnotify.Create != 0:
		eventType = "file_created"
	case fsEvent.Op&fsnotify.Write != 0:
		eventType = "file_modified"
	case fsEvent.Op&fsnotify.Remove != 0:
		eventType = "file_deleted"
	default:
		return
	}

	// Check if we care about this event type
	if !f.onEvents[eventType] {
		return
	}

	// Check ignore patterns
	filename := filepath.Base(fsEvent.Name)
	for _, pattern := range f.ignorePatterns {
		if matched, _ := filepath.Match(pattern, filename); matched {
			return
		}
	}

	// Debounce if configured
	if f.debounceSeconds > 0 {
		f.debounce(fsEvent.Name, eventType, events)
		return
	}

	f.sendEvent(fsEvent.Name, eventType, events)
}

func (f *Filesystem) debounce(path, eventType string, events chan<- Event) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Cancel existing timer for this path
	if timer, exists := f.pending[path]; exists {
		timer.Stop()
	}

	// Create new timer
	f.pending[path] = time.AfterFunc(time.Duration(f.debounceSeconds)*time.Second, func() {
		f.mu.Lock()
		delete(f.pending, path)
		f.mu.Unlock()
		f.sendEvent(path, eventType, events)
	})
}

func (f *Filesystem) sendEvent(path, eventType string, events chan<- Event) {
	select {
	case events <- Event{
		RuleName:  f.ruleName,
		Type:      eventType,
		Timestamp: time.Now(),
		Data: map[string]any{
			"file_path":  path,
			"file_name":  filepath.Base(path),
			"event_type": eventType,
		},
	}:
	default:
		// channel full, drop event
	}
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
