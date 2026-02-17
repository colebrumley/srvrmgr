// internal/security/permissions_test.go
package security

import (
	"os"
	"path/filepath"
	"testing"
)

// ===== FR-14: Rules directory permission enforcement =====

func TestValidateDirectoryPermissions_CorrectPerms(t *testing.T) {
	dir := t.TempDir()

	// Set correct permissions: owned by current user, mode 0700
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}

	err := ValidateDirectoryPermissions(dir)
	if err != nil {
		t.Errorf("FR-14: expected no error for dir with 0700 perms, got: %v", err)
	}
}

func TestValidateDirectoryPermissions_Mode0750(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chmod(dir, 0750); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}

	err := ValidateDirectoryPermissions(dir)
	if err != nil {
		t.Errorf("FR-14: expected no error for dir with 0750 perms, got: %v", err)
	}
}

func TestValidateDirectoryPermissions_WorldWritable(t *testing.T) {
	dir := t.TempDir()

	// Set world-writable permissions (insecure)
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}

	err := ValidateDirectoryPermissions(dir)
	if err == nil {
		t.Error("FR-14: expected error for world-writable directory")
	}
}

func TestValidateDirectoryPermissions_WorldReadWrite(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chmod(dir, 0766); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}

	err := ValidateDirectoryPermissions(dir)
	if err == nil {
		t.Error("FR-14: expected error for directory with other-write permission")
	}
}

func TestValidateDirectoryPermissions_NonexistentDir(t *testing.T) {
	err := ValidateDirectoryPermissions("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("FR-14: expected error for nonexistent directory")
	}
}

func TestValidateFilePermissions_CorrectPerms(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test-rule.yaml")

	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	err := ValidateFilePermissions(filePath)
	if err != nil {
		t.Errorf("FR-14: expected no error for file with 0644 perms, got: %v", err)
	}
}

func TestValidateFilePermissions_WorldWritable(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test-rule.yaml")

	if err := os.WriteFile(filePath, []byte("test"), 0666); err != nil {
		t.Fatal(err)
	}
	// Explicitly set world-writable
	if err := os.Chmod(filePath, 0666); err != nil {
		t.Fatal(err)
	}

	err := ValidateFilePermissions(filePath)
	if err == nil {
		t.Error("FR-14: expected error for world-writable file")
	}
}
