// internal/security/scrubber.go
// FR-18: Output scrubbing for sensitive data
// Stub — to be fully implemented during the implementation phase.
package security

import "regexp"

var (
	// Plex token pattern: X-Plex-Token=<value>
	plexTokenPattern = regexp.MustCompile(`X-Plex-Token=\S+`)
	// Bearer token pattern
	bearerPattern = regexp.MustCompile(`Bearer\s+\S{20,}`)
	// Long hex strings (32+ chars) — likely API keys
	hexKeyPattern = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
)

// ScrubOutput redacts sensitive data from output before storage.
func ScrubOutput(output string) string {
	result := plexTokenPattern.ReplaceAllString(output, "X-Plex-Token=[REDACTED]")
	result = bearerPattern.ReplaceAllString(result, "Bearer [REDACTED]")
	result = hexKeyPattern.ReplaceAllString(result, "[REDACTED]")
	return result
}
