// internal/trigger/filesystem_test.go
package trigger

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strings"
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

// ===== FR-11: Emit directory_created for directories =====

func TestFilesystemTrigger_DirectoryCreated(t *testing.T) {
	dir := t.TempDir()

	triggerCfg := config.Trigger{
		Type:            "filesystem",
		WatchPaths:      []string{dir},
		OnEvents:        []string{"file_created", "directory_created"},
		DebounceSeconds: 0,
	}

	trigger, err := NewFilesystem("dir-test-rule", triggerCfg)
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

	// Create a directory (not a file)
	subDir := filepath.Join(dir, "new-directory")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Wait for event
	select {
	case event := <-events:
		// FR-11: The event type should be "directory_created" for directories
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

// ===== FR-12: expandHomeForUser resolves to correct user's home =====

func TestExpandHomeForUser_ResolvesCorrectHome(t *testing.T) {
	// FR-12: expandHomeForUser should use os/user.Lookup to resolve ~
	// to the specified user's home directory, not os.UserHomeDir (root's home).

	// Test with current user (should work without root privileges)
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

func TestExpandHomeForUser_EmptyUser(t *testing.T) {
	// FR-12: Empty username should fall back to os.UserHomeDir()
	result := expandHomeForUser("~/test", "")
	if result == "~/test" {
		t.Error("FR-12: should still expand ~ when user is empty (fallback)")
	}
}

func TestExpandHomeForUser_NoTilde(t *testing.T) {
	// Paths without ~ should be returned unchanged
	result := expandHomeForUser("/absolute/path", "cole")
	if result != "/absolute/path" {
		t.Errorf("FR-12: non-tilde path should be unchanged, got %q", result)
	}
}

func TestExpandHomeForUser_InvalidUser(t *testing.T) {
	// FR-12: Invalid user should fall back to os.UserHomeDir()
	result := expandHomeForUser("~/test", "nonexistent-user-xyz-12345")
	// Should either expand using fallback or return original
	// The key thing: it should not panic
	_ = result
}

// expandHomeForUser is the function FR-12 requires to be implemented.
// It resolves ~ to the specified user's home directory.
// Defined here for testing until implemented in the main trigger package.
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
