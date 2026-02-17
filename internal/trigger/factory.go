// internal/trigger/factory.go
package trigger

import (
	"fmt"

	"github.com/colebrumley/srvrmgr/internal/config"
)

// New creates a trigger based on the configuration type.
// FR-12: runAsUser is passed to filesystem triggers for ~ expansion.
// Sourced from convention — clean 3-param approach avoids special-casing in daemon.
func New(ruleName string, cfg config.Trigger, runAsUser string) (Trigger, error) {
	switch cfg.Type {
	case "filesystem":
		return NewFilesystem(ruleName, cfg, runAsUser)
	case "scheduled":
		return NewScheduled(ruleName, cfg)
	case "webhook":
		return NewWebhook(ruleName, cfg)
	case "lifecycle":
		return NewLifecycle(ruleName, cfg)
	case "manual":
		return NewManual(ruleName, cfg)
	default:
		return nil, fmt.Errorf("unknown trigger type: %s", cfg.Type)
	}
}
