//go:build darwin

package trigger

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
)

const fsEventsInitDelay = 500 * time.Millisecond

func TestFilesystemTrigger(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	triggerCfg := config.Trigger{
		Type:            "filesystem",
		WatchPaths:      []string{dir},
		OnEvents:        []string{"file_created"},
		IgnorePatterns:  []string{"*.tmp"},
		DebounceSeconds: 0,
	}

	trigger, err := NewFilesystem("test-rule", triggerCfg, "")
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

	time.Sleep(fsEventsInitDelay)

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
	case <-time.After(500 * time.Millisecond):
		// Expected - no event
	}
}

// ===== FR-11: Emit directory_created for directories =====

func TestFilesystemTrigger_DirectoryCreated(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	triggerCfg := config.Trigger{
		Type:            "filesystem",
		WatchPaths:      []string{dir},
		OnEvents:        []string{"file_created", "directory_created"},
		DebounceSeconds: 0,
	}

	trigger, err := NewFilesystem("dir-test-rule", triggerCfg, "")
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

	time.Sleep(fsEventsInitDelay)

	// Create a directory (not a file)
	subDir := filepath.Join(dir, "new-directory")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Wait for event
	select {
	case event := <-events:
		if event.Type != "directory_created" {
			t.Errorf("FR-11: expected event type 'directory_created' for directory creation, got %q", event.Type)
		}
		if event.RuleName != "dir-test-rule" {
			t.Errorf("expected rule name dir-test-rule, got %s", event.RuleName)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for directory creation event")
	}
}

func TestFilesystemTrigger_DoubleStartReturnsError(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	triggerCfg := config.Trigger{
		Type:       "filesystem",
		WatchPaths: []string{dir},
		OnEvents:   []string{"file_created"},
	}

	trigger, err := NewFilesystem("double-start", triggerCfg, "")
	if err != nil {
		t.Fatalf("NewFilesystem failed: %v", err)
	}

	events := make(chan Event, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = trigger.Start(ctx, events)
	}()
	time.Sleep(fsEventsInitDelay)

	// Second Start should return an error
	err = trigger.Start(ctx, events)
	if err == nil {
		t.Error("expected error on double Start(), got nil")
	}
}

func TestFilesystemTrigger_StopIsIdempotent(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	triggerCfg := config.Trigger{
		Type:       "filesystem",
		WatchPaths: []string{dir},
		OnEvents:   []string{"file_created"},
	}

	trigger, err := NewFilesystem("stop-test", triggerCfg, "")
	if err != nil {
		t.Fatalf("NewFilesystem failed: %v", err)
	}

	events := make(chan Event, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = trigger.Start(ctx, events)
	}()
	time.Sleep(fsEventsInitDelay)

	// Multiple Stop calls should not panic
	if err := trigger.Stop(); err != nil {
		t.Errorf("first Stop failed: %v", err)
	}
	if err := trigger.Stop(); err != nil {
		t.Errorf("second Stop failed: %v", err)
	}
}

// ===== FR-12: expandHomeForUser resolves to correct user's home =====

func TestExpandHomeForUser_ResolvesCorrectHome(t *testing.T) {
	currentUser := os.Getenv("USER")
	if currentUser == "" {
		t.Skip("USER env var not set")
	}

	result := expandHomeForUser("~/Downloads", currentUser)
	if result == "~/Downloads" {
		t.Error("FR-12: expandHomeForUser should expand ~ to user's home directory")
	}
	if !filepath.IsAbs(result) {
		t.Errorf("FR-12: result should be absolute path, got %q", result)
	}
	if !strings.HasSuffix(result, "/Downloads") {
		t.Errorf("FR-12: expected path ending in /Downloads, got %q", result)
	}
}

func TestExpandHomeForUser_BareTilde(t *testing.T) {
	result := expandHomeForUser("~", "")
	if result == "~" {
		t.Error("bare ~ should be expanded to home directory")
	}
	if !filepath.IsAbs(result) {
		t.Errorf("result should be absolute path, got %q", result)
	}
}

func TestExpandHomeForUser_EmptyUser(t *testing.T) {
	result := expandHomeForUser("~/test", "")
	if result == "~/test" {
		t.Error("FR-12: should still expand ~ when user is empty (fallback)")
	}
}

func TestExpandHomeForUser_NoTilde(t *testing.T) {
	result := expandHomeForUser("/absolute/path", "cole")
	if result != "/absolute/path" {
		t.Errorf("FR-12: non-tilde path should be unchanged, got %q", result)
	}
}

func TestExpandHomeForUser_InvalidUser(t *testing.T) {
	// Invalid user should fall back to current user's home directory
	result := expandHomeForUser("~/test", "nonexistent-user-xyz-12345")
	if result == "~/test" {
		t.Error("FR-12: should fall back to current user's home for invalid user")
	}
	if !filepath.IsAbs(result) {
		t.Errorf("FR-12: result should be absolute path, got %q", result)
	}
	if !strings.HasSuffix(result, "/test") {
		t.Errorf("FR-12: expected path ending in /test, got %q", result)
	}
}
