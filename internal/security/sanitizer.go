// internal/security/sanitizer.go
// FR-16: Template variable sanitization
// Stub — to be fully implemented during the implementation phase.
package security

import "strings"

// SanitizeValue sanitizes a template variable value before interpolation.
// - Strips control characters (0x00-0x1F except tab/newline)
// - Strips backtick sequences that could break code fences
// - Truncates to 1024 characters
func SanitizeValue(s string) string {
	// Strip control characters
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' {
			continue // strip control chars
		}
		b.WriteRune(r)
	}
	result := b.String()

	// Strip triple backtick sequences
	result = strings.ReplaceAll(result, "```", "")

	// Truncate to 1024 characters
	if len(result) > 1024 {
		result = result[:1024]
	}

	return result
}
