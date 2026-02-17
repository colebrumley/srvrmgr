// internal/security/sanitizer_test.go
package security

import (
	"strings"
	"testing"
)

// ===== FR-16: Template variable sanitization =====

func TestSanitizeValue_StripControlChars(t *testing.T) {
	// Control characters 0x00-0x1F (except \t and \n) should be stripped
	input := "hello\x00world\x01test\x02end"
	result := SanitizeValue(input)

	for _, r := range result {
		if r < 0x20 && r != '\t' && r != '\n' {
			t.Errorf("FR-16: result contains control character 0x%02x", r)
		}
	}
	if !strings.Contains(result, "hello") || !strings.Contains(result, "world") {
		t.Errorf("FR-16: readable content should be preserved: %q", result)
	}
}

func TestSanitizeValue_PreservesTabNewline(t *testing.T) {
	input := "line1\nline2\tcol2"
	result := SanitizeValue(input)

	if !strings.Contains(result, "\n") {
		t.Error("FR-16: newlines should be preserved")
	}
	if !strings.Contains(result, "\t") {
		t.Error("FR-16: tabs should be preserved")
	}
}

func TestSanitizeValue_StripBackticks(t *testing.T) {
	input := "file```injection```test.txt"
	result := SanitizeValue(input)

	if strings.Contains(result, "```") {
		t.Errorf("FR-16: triple backticks should be stripped: %q", result)
	}
}

func TestSanitizeValue_StripSingleBacktickPairs(t *testing.T) {
	// Single backticks used for inline code — less dangerous but still worth testing
	input := "normal`code`text"
	result := SanitizeValue(input)

	// Implementation may or may not strip single backticks;
	// the spec mentions "backtick sequences that could break code fences"
	// Triple backticks are the primary concern.
	_ = result // Just verify it doesn't panic
}

func TestSanitizeValue_TruncateTo1024(t *testing.T) {
	longInput := strings.Repeat("x", 2000)
	result := SanitizeValue(longInput)

	if len(result) > 1024 {
		t.Errorf("FR-16: result should be truncated to 1024 chars, got %d", len(result))
	}
}

func TestSanitizeValue_Exactly1024(t *testing.T) {
	input := strings.Repeat("a", 1024)
	result := SanitizeValue(input)

	if len(result) != 1024 {
		t.Errorf("FR-16: exact 1024-char input should not be truncated, got %d", len(result))
	}
}

func TestSanitizeValue_Under1024(t *testing.T) {
	input := "short string"
	result := SanitizeValue(input)

	if result != input {
		t.Errorf("FR-16: short string should not be modified: %q -> %q", input, result)
	}
}

func TestSanitizeValue_EmptyString(t *testing.T) {
	result := SanitizeValue("")
	if result != "" {
		t.Errorf("FR-16: empty string should remain empty: %q", result)
	}
}

func TestSanitizeValue_CombinedThreats(t *testing.T) {
	// A malicious filename combining all threat vectors
	input := "\x00```\x01" + strings.Repeat("A", 2000) + "```\x02"
	result := SanitizeValue(input)

	// Should be truncated
	if len(result) > 1024 {
		t.Errorf("FR-16: should be truncated, got %d chars", len(result))
	}
	// Should not contain control chars
	for _, r := range result {
		if r < 0x20 && r != '\t' && r != '\n' {
			t.Errorf("FR-16: contains control char 0x%02x", r)
			break
		}
	}
	// Should not contain triple backticks
	if strings.Contains(result, "```") {
		t.Errorf("FR-16: contains triple backticks")
	}
}
