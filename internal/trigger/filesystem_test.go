// internal/trigger/filesystem_test.go
package trigger

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
)

func TestFilesystemTrigger(t *testing.T) {
	dir := t.TempDir()

	triggerCfg := config.Trigger{
		Type:            "filesystem",
		WatchPaths:      []string{dir},
		OnEvents:        []string{"file_created"},
		IgnorePatterns:  []string{"*.tmp"},
		DebounceSeconds: 0, // No debounce for test
	}

	trigger, err := NewFilesystem("test-rule", triggerCfg)
	if err != nil {
		t.Fatalf("NewFilesystem failed: %v", err)
	}

	events := make(chan Event, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := trigger.Start(ctx, events); err != nil && err != context.Canceled {
			t.Errorf("Start failed: %v", err)
		}
	}()

	// Give watcher time to start
	time.Sleep(100 * time.Millisecond)

	// Create a file
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for event
	select {
	case event := <-events:
		if event.RuleName != "test-rule" {
			t.Errorf("expected rule name test-rule, got %s", event.RuleName)
		}
		if event.Type != "file_created" {
			t.Errorf("expected event type file_created, got %s", event.Type)
		}
		filePath, ok := event.Data["file_path"].(string)
		if !ok || filePath != testFile {
			t.Errorf("expected file_path %s, got %v", testFile, event.Data["file_path"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Create a .tmp file - should be ignored
	tmpFile := filepath.Join(dir, "ignored.tmp")
	if err := os.WriteFile(tmpFile, []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}

	// Should not receive event for .tmp file
	select {
	case event := <-events:
		t.Errorf("unexpected event for ignored file: %+v", event)
	case <-time.After(200 * time.Millisecond):
		// Expected - no event
	}
}
