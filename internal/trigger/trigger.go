// internal/trigger/trigger.go
package trigger

import (
	"context"
	"time"
)

// Event represents a trigger event
type Event struct {
	RuleName  string
	Type      string
	Timestamp time.Time
	Data      map[string]any
}

// Trigger is the interface all triggers must implement
type Trigger interface {
	// Start begins watching for events, sending them to the channel
	Start(ctx context.Context, events chan<- Event) error
	// Stop stops the trigger
	Stop() error
	// RuleName returns the name of the rule this trigger belongs to
	RuleName() string
}
