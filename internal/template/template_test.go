// internal/template/template_test.go
package template

import (
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
