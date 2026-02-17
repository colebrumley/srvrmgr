// internal/security/scrubber_test.go
package security

import (
	"strings"
	"testing"
)

// ===== FR-18: Output scrubbing for sensitive data =====

func TestScrubOutput_PlexToken(t *testing.T) {
	input := `Connecting to http://localhost:32400?X-Plex-Token=abc123def456 for library scan`
	result := ScrubOutput(input)

	if strings.Contains(result, "abc123def456") {
		t.Errorf("FR-18: Plex token not scrubbed: %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("FR-18: expected [REDACTED] in output: %q", result)
	}
}

func TestScrubOutput_PlexTokenInURL(t *testing.T) {
	input := `curl http://localhost:32400/library/sections?X-Plex-Token=mySecretPlexToken123`
	result := ScrubOutput(input)

	if strings.Contains(result, "mySecretPlexToken123") {
		t.Errorf("FR-18: Plex token in URL not scrubbed: %q", result)
	}
}

func TestScrubOutput_BearerToken(t *testing.T) {
	input := `Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U`
	result := ScrubOutput(input)

	if strings.Contains(result, "eyJhbGci") {
		t.Errorf("FR-18: Bearer token not scrubbed: %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("FR-18: expected [REDACTED] in output: %q", result)
	}
}

func TestScrubOutput_APIKey_32HexChars(t *testing.T) {
	// 32+ hex character string that looks like an API key
	input := `Using API key: abcdef0123456789abcdef0123456789 for authentication`
	result := ScrubOutput(input)

	if strings.Contains(result, "abcdef0123456789abcdef0123456789") {
		t.Errorf("FR-18: 32-char hex API key not scrubbed: %q", result)
	}
}

func TestScrubOutput_APIKey_64HexChars(t *testing.T) {
	hexKey := strings.Repeat("ab", 32) // 64 hex chars
	input := "key=" + hexKey
	result := ScrubOutput(input)

	if strings.Contains(result, hexKey) {
		t.Errorf("FR-18: 64-char hex API key not scrubbed: %q", result)
	}
}

func TestScrubOutput_NoSecrets(t *testing.T) {
	input := `Normal output: disk usage is 45%, everything looks healthy`
	result := ScrubOutput(input)

	if result != input {
		t.Errorf("FR-18: clean output was modified: %q -> %q", input, result)
	}
}

func TestScrubOutput_MultipleSecrets(t *testing.T) {
	input := `X-Plex-Token=secret1 and Bearer mytoken123456789012345678901234567890`
	result := ScrubOutput(input)

	if strings.Contains(result, "secret1") {
		t.Errorf("FR-18: first secret not scrubbed: %q", result)
	}
}

func TestScrubOutput_PreservesStructure(t *testing.T) {
	input := `Status: OK
Token: Bearer abc123def456ghi789jkl012mno345pqr
Disk: 45% used`
	result := ScrubOutput(input)

	if !strings.Contains(result, "Status: OK") {
		t.Error("FR-18: non-secret content was removed")
	}
	if !strings.Contains(result, "Disk: 45% used") {
		t.Error("FR-18: non-secret content was removed")
	}
}

func TestScrubOutput_ShortHexNotScrubbed(t *testing.T) {
	// Short hex strings (< 32 chars) should NOT be scrubbed — they could be hashes, IDs, etc.
	input := "commit abc123def is deployed"
	result := ScrubOutput(input)

	if !strings.Contains(result, "abc123def") {
		t.Error("FR-18: short hex string should not be scrubbed")
	}
}
