// internal/trigger/manual.go
package trigger

import (
	"context"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
)

// Manual is a trigger that only fires via CLI
type Manual struct {
	ruleName string
}

// NewManual creates a new manual trigger
func NewManual(ruleName string, cfg config.Trigger) (*Manual, error) {
	return &Manual{ruleName: ruleName}, nil
}

func (m *Manual) RuleName() string {
	return m.ruleName
}

// Start for manual trigger just blocks - it never fires automatically
func (m *Manual) Start(ctx context.Context, events chan<- Event) error {
	<-ctx.Done()
	return ctx.Err()
}

func (m *Manual) Stop() error {
	return nil
}

// Fire manually triggers this rule. Returns false if the channel is full.
func (m *Manual) Fire(events chan<- Event, data map[string]any) bool {
	select {
	case events <- Event{
		RuleName:  m.ruleName,
		Type:      "manual",
		Timestamp: time.Now(),
		Data:      data,
	}:
		return true
	default:
		return false
	}
}
