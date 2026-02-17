// internal/template/template_test.go
package template

import (
	"strings"
	"testing"
)

func TestExpand(t *testing.T) {
	tests := []struct {
		name     string
		template string
		data     map[string]any
		want     string
	}{
		{
			name:     "simple replacement",
			template: "File: {{file_path}}",
			data:     map[string]any{"file_path": "/path/to/file.txt"},
			want:     "File: /path/to/file.txt",
		},
		{
			name:     "multiple replacements",
			template: "{{file_name}} in {{file_path}}",
			data:     map[string]any{"file_name": "test.txt", "file_path": "/tmp"},
			want:     "test.txt in /tmp",
		},
		{
			name:     "missing variable",
			template: "File: {{file_path}}",
			data:     map[string]any{},
			want:     "File: {{file_path}}",
		},
		{
			name:     "no variables",
			template: "Just plain text",
			data:     map[string]any{"unused": "value"},
			want:     "Just plain text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Expand(tt.template, tt.data)
			if got != tt.want {
				t.Errorf("Expand() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ===== FR-16: Template variable sanitization =====

func TestExpand_SanitizesControlChars(t *testing.T) {
	// FR-16: Control characters (0x00-0x1F except tab/newline) should be stripped
	data := map[string]any{
		"file_name": "normal\x00evil\x01file\x02name.txt",
	}
	result := Expand("File: {{file_name}}", data)

	// Should not contain control characters
	for _, r := range result {
		if r < 0x20 && r != '\t' && r != '\n' {
			t.Errorf("FR-16: result contains control character 0x%02x: %q", r, result)
		}
	}
}

func TestExpand_SanitizesBackticks(t *testing.T) {
	// FR-16: Backtick sequences that could break code fences should be stripped
	data := map[string]any{
		"file_name": "file```inject```name.txt",
	}
	result := Expand("File: {{file_name}}", data)

	if strings.Contains(result, "```") {
		t.Errorf("FR-16: result should not contain triple backticks: %q", result)
	}
}

func TestExpand_TruncatesLongValues(t *testing.T) {
	// FR-16: Individual values should be truncated to 1024 characters
	longValue := strings.Repeat("a", 2000)
	data := map[string]any{
		"file_name": longValue,
	}
	result := Expand("{{file_name}}", data)

	if len(result) > 1024 {
		t.Errorf("FR-16: result should be truncated to 1024 chars, got %d", len(result))
	}
}

func TestExpand_SanitizesCombined(t *testing.T) {
	// FR-16: Test combined sanitization — control chars + backticks + long value
	evil := strings.Repeat("\x00", 10) + "```" + strings.Repeat("x", 2000)
	data := map[string]any{
		"file_name": evil,
	}
	result := Expand("Name: {{file_name}}", data)

	// Should not contain control chars
	for _, r := range result {
		if r < 0x20 && r != '\t' && r != '\n' {
			t.Errorf("FR-16: result contains control character: %q", result)
			break
		}
	}
	// Should not contain triple backticks
	if strings.Contains(result, "```") {
		t.Errorf("FR-16: result contains triple backticks: %q", result)
	}
	// Value portion should be truncated (account for "Name: " prefix)
	// The value itself should be at most 1024 chars
	prefix := "Name: "
	if len(result) > len(prefix)+1024 {
		t.Errorf("FR-16: value portion exceeds 1024 chars, total result len: %d", len(result))
	}
}

func TestExpand_PreservesTabsAndNewlines(t *testing.T) {
	// FR-16: Tab and newline should NOT be stripped
	data := map[string]any{
		"file_name": "file\twith\ttabs\nand\nnewlines.txt",
	}
	result := Expand("{{file_name}}", data)

	if !strings.Contains(result, "\t") {
		t.Error("FR-16: tabs should be preserved in sanitized output")
	}
	if !strings.Contains(result, "\n") {
		t.Error("FR-16: newlines should be preserved in sanitized output")
	}
}
