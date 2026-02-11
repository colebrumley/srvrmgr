// internal/trigger/lifecycle.go
package trigger

import (
	"context"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
)

// Lifecycle fires on daemon start/stop events
type Lifecycle struct {
	ruleName string
	onEvents map[string]bool
}

// NewLifecycle creates a new lifecycle trigger
func NewLifecycle(ruleName string, cfg config.Trigger) (*Lifecycle, error) {
	onEvents := make(map[string]bool)
	for _, e := range cfg.OnEvents {
		onEvents[e] = true
	}

	return &Lifecycle{
		ruleName: ruleName,
		onEvents: onEvents,
	}, nil
}

func (l *Lifecycle) RuleName() string {
	return l.ruleName
}

func (l *Lifecycle) Start(ctx context.Context, events chan<- Event) error {
	<-ctx.Done()
	return ctx.Err()
}

func (l *Lifecycle) Stop() error {
	return nil
}

// ShouldFireOn returns true if this trigger should fire for the given event
func (l *Lifecycle) ShouldFireOn(eventType string) bool {
	return l.onEvents[eventType]
}

// Fire sends a lifecycle event. Returns false if the channel is full.
func (l *Lifecycle) Fire(eventType string, events chan<- Event) bool {
	if !l.ShouldFireOn(eventType) {
		return false
	}
	select {
	case events <- Event{
		RuleName:  l.ruleName,
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      map[string]any{},
	}:
		return true
	default:
		return false // channel full, avoid blocking
	}
}
