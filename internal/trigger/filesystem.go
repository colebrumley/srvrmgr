// internal/trigger/filesystem.go
package trigger

import (
	"context"
	"os"
	"os/user"
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
// FR-12: runAsUser is used to resolve ~ in watch_paths to the correct user's home.
// Sourced from convention — 3-param signature avoids dual-function pattern.
func NewFilesystem(ruleName string, cfg config.Trigger, runAsUser string) (*Filesystem, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	onEvents := make(map[string]bool)
	for _, e := range cfg.OnEvents {
		onEvents[e] = true
	}

	// FR-12: Expand ~ in paths using run_as_user's home directory
	var watchPaths []string
	for _, p := range cfg.WatchPaths {
		watchPaths = append(watchPaths, expandHomeForUser(p, runAsUser))
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
		// FR-11: Distinguish directory_created from file_created.
		// Sourced from convention.
		if info, err := os.Stat(fsEvent.Name); err == nil && info.IsDir() {
			eventType = "directory_created"
		} else {
			eventType = "file_created"
		}
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

// FR-12: expandHomeForUser resolves ~ to the specified user's home directory.
// Sourced from convention — single function with username fallback.
func expandHomeForUser(path, username string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}

	if username != "" {
		u, err := user.Lookup(username)
		if err == nil {
			return filepath.Join(u.HomeDir, path[2:])
		}
	}

	// Fallback to current user's home
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

func expandHome(path string) string {
	return expandHomeForUser(path, "")
}
