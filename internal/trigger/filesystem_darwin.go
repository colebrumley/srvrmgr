//go:build darwin

package trigger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
	"github.com/fsnotify/fsevents"
)

// pendingEvent tracks a debounced event, preserving the first event type
// seen during the debounce window so that file_created isn't silently
// replaced by a subsequent file_modified.
type pendingEvent struct {
	timer     *time.Timer
	eventType string
}

// Filesystem watches directories for file events using macOS FSEvents.
// FSEvents watches path strings (not file descriptors), so it handles
// volume mount/unmount and non-existent paths natively.
type Filesystem struct {
	ruleName          string
	watchPaths        []string
	watchPathPrefixes []string // precomputed wp + "/" for recursive prefix matching
	cleanedWatchPaths []string // precomputed filepath.Clean(wp) for non-recursive matching
	recursive         bool
	onEvents          map[string]bool
	ignorePatterns    []string
	debounceDuration  time.Duration
	stream            *fsevents.EventStream
	done              chan struct{}
	mu                sync.Mutex
	pending           map[string]*pendingEvent
	stopped           bool
	running           bool
}

var _ Trigger = (*Filesystem)(nil)

// NewFilesystem creates a new filesystem trigger using macOS FSEvents.
// FR-12: runAsUser is used to resolve ~ in watch_paths to the correct user's home.
func NewFilesystem(ruleName string, cfg config.Trigger, runAsUser string) (*Filesystem, error) {
	onEvents := make(map[string]bool)
	for _, e := range cfg.OnEvents {
		onEvents[e] = true
	}

	var watchPaths, prefixes, cleaned []string
	for _, p := range cfg.WatchPaths {
		expanded := expandHomeForUser(p, runAsUser)
		if resolved, err := filepath.EvalSymlinks(expanded); err == nil {
			expanded = resolved
		}
		watchPaths = append(watchPaths, expanded)
		prefixes = append(prefixes, expanded+"/")
		cleaned = append(cleaned, filepath.Clean(expanded))
	}

	return &Filesystem{
		ruleName:          ruleName,
		watchPaths:        watchPaths,
		watchPathPrefixes: prefixes,
		cleanedWatchPaths: cleaned,
		recursive:         cfg.Recursive,
		onEvents:          onEvents,
		ignorePatterns:    cfg.IgnorePatterns,
		debounceDuration:  time.Duration(cfg.DebounceSeconds) * time.Second,
		pending:           make(map[string]*pendingEvent),
	}, nil
}

func (f *Filesystem) RuleName() string {
	return f.ruleName
}

func (f *Filesystem) Start(ctx context.Context, events chan<- Event) error {
	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return fmt.Errorf("filesystem trigger %q already running", f.ruleName)
	}
	f.running = true
	f.stopped = false
	f.done = make(chan struct{})

	stream := &fsevents.EventStream{
		Paths:   f.watchPaths,
		Latency: 0,
		Flags:   fsevents.FileEvents | fsevents.WatchRoot | fsevents.NoDefer,
	}
	f.stream = stream
	f.mu.Unlock()

	stream.Start()
	slog.Info("fsevents stream started", "rule", f.ruleName, "paths", f.watchPaths)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.done:
			return nil
		case batch := <-stream.Events:
			for _, ev := range batch {
				f.handleFSEvent(ev, events)
			}
		}
	}
}

func (f *Filesystem) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.stopped = true
	f.running = false

	if f.stream != nil {
		f.stream.Stop()
		f.stream = nil
	}

	if f.done != nil {
		select {
		case <-f.done:
		default:
			close(f.done)
		}
	}

	for path, pe := range f.pending {
		pe.timer.Stop()
		delete(f.pending, path)
	}

	return nil
}

// isWatchedPath filters events by depth. FSEvents always watches recursively,
// so when recursive=false (default), only direct children of watched paths pass.
func (f *Filesystem) isWatchedPath(eventPath string) bool {
	if f.recursive {
		for i, wp := range f.watchPaths {
			if strings.HasPrefix(eventPath, f.watchPathPrefixes[i]) || eventPath == wp {
				return true
			}
		}
		return false
	}
	cleanedParent := filepath.Clean(filepath.Dir(eventPath))
	for _, cwp := range f.cleanedWatchPaths {
		if cleanedParent == cwp {
			return true
		}
	}
	return false
}

func (f *Filesystem) handleFSEvent(ev fsevents.Event, events chan<- Event) {
	// Warn on queue overflow — these flags mean the kernel or userspace dropped
	// events and a full rescan would be needed to catch what was missed.
	if ev.Flags&fsevents.MustScanSubDirs != 0 ||
		ev.Flags&fsevents.KernelDropped != 0 ||
		ev.Flags&fsevents.UserDropped != 0 {
		slog.Warn("fsevents queue overflow, events may have been lost",
			"rule", f.ruleName, "path", ev.Path, "flags", ev.Flags)
		return
	}
	if ev.Flags&fsevents.Mount != 0 || ev.Flags&fsevents.Unmount != 0 ||
		ev.Flags&fsevents.RootChanged != 0 {
		return
	}

	eventPath := ev.Path

	// Map flags to event type first (O(1) bitmask checks), then filter by
	// onEvents before doing O(n) path matching — avoids wasted work for
	// unwatched event types.
	var eventType string
	switch {
	case ev.Flags&fsevents.ItemRemoved != 0:
		if ev.Flags&fsevents.ItemIsDir != 0 {
			eventType = "directory_deleted"
		} else {
			eventType = "file_deleted"
		}
	case ev.Flags&fsevents.ItemCreated != 0:
		// Includes rename destinations (typically ItemCreated | ItemRenamed).
		if ev.Flags&fsevents.ItemIsDir != 0 {
			eventType = "directory_created"
		} else {
			eventType = "file_created"
		}
	case ev.Flags&fsevents.ItemModified != 0:
		eventType = "file_modified"
	default:
		// Bare ItemRenamed without ItemCreated or ItemRemoved is the source
		// side of a rename — the path no longer exists at this location. Skip.
		return
	}

	if !f.onEvents[eventType] {
		return
	}

	if !f.isWatchedPath(eventPath) {
		return
	}

	// Ignore patterns match against the basename only (not the full path).
	filename := filepath.Base(eventPath)
	for _, pattern := range f.ignorePatterns {
		if matched, _ := filepath.Match(pattern, filename); matched {
			return
		}
	}

	if f.debounceDuration > 0 {
		f.debounce(eventPath, filename, eventType, events)
		return
	}
	f.sendEvent(eventPath, filename, eventType, events)
}

func (f *Filesystem) debounce(path, filename, eventType string, events chan<- Event) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.stopped {
		return
	}

	// Keep the first event type seen during the debounce window,
	// so file_created isn't silently replaced by file_modified.
	if pe, exists := f.pending[path]; exists {
		pe.timer.Stop()
		eventType = pe.eventType
	}

	f.pending[path] = &pendingEvent{
		eventType: eventType,
		timer: time.AfterFunc(f.debounceDuration, func() {
			f.mu.Lock()
			stopped := f.stopped
			delete(f.pending, path)
			f.mu.Unlock()
			if !stopped {
				f.sendEvent(path, filename, eventType, events)
			}
		}),
	}
}

func (f *Filesystem) sendEvent(path, filename, eventType string, events chan<- Event) {
	select {
	case events <- Event{
		RuleName:  f.ruleName,
		Type:      eventType,
		Timestamp: time.Now(),
		Data: map[string]any{
			"file_path":  path,
			"file_name":  filename,
			"event_type": eventType,
		},
	}:
	default:
		// channel full, drop event
	}
}

// expandHomeForUser resolves ~ or ~/... to the specified user's home directory.
func expandHomeForUser(path, username string) string {
	if path == "~" {
		path = "~/"
	}
	if !strings.HasPrefix(path, "~/") {
		return path
	}

	if username != "" {
		u, err := user.Lookup(username)
		if err == nil {
			return filepath.Join(u.HomeDir, path[2:])
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}
