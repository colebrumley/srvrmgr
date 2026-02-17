// internal/security/permissions.go
// FR-14: Rules directory permission enforcement
// Stub — implementation will be expanded during the implementation phase.
package security

import (
	"fmt"
	"os"
)

// ValidateDirectoryPermissions checks that a directory has safe permissions.
// Returns an error if the directory is world-writable or has other unsafe permissions.
func ValidateDirectoryPermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("checking directory permissions: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}

	mode := info.Mode().Perm()
	// Check for world-writable (others have write permission)
	if mode&0002 != 0 {
		return fmt.Errorf("directory %s is world-writable (mode %04o), expected 0700 or 0750", path, mode)
	}
	// Check for group-writable on overly open permissions
	if mode&0077 > 0050 {
		return fmt.Errorf("directory %s has overly permissive mode %04o, expected 0700 or 0750", path, mode)
	}

	return nil
}

// ValidateFilePermissions checks that a file has safe permissions.
func ValidateFilePermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("checking file permissions: %w", err)
	}

	mode := info.Mode().Perm()
	// Check for world-writable
	if mode&0002 != 0 {
		return fmt.Errorf("file %s is world-writable (mode %04o)", path, mode)
	}

	return nil
}
