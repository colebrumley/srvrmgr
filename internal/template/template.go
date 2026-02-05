// internal/template/template.go
package template

import (
	"fmt"
	"regexp"
)

var templateVar = regexp.MustCompile(`\{\{(\w+)\}\}`)

// Expand replaces {{variable}} placeholders with values from data
func Expand(tmpl string, data map[string]any) string {
	return templateVar.ReplaceAllStringFunc(tmpl, func(match string) string {
		// Extract variable name (remove {{ and }})
		varName := match[2 : len(match)-2]

		if val, ok := data[varName]; ok {
			return fmt.Sprintf("%v", val)
		}
		return match // Keep original if not found
	})
}
