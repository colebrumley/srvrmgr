// internal/trigger/scheduled_test.go
package trigger

import (
	"context"
	"testing"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
)

func TestScheduledTrigger(t *testing.T) {
	// Use a cron that fires every second for testing
	triggerCfg := config.Trigger{
		Type:           "scheduled",
		CronExpression: "* * * * * *", // Every second (with seconds field)
	}

	trigger, err := NewScheduled("test-rule", triggerCfg)
	if err != nil {
		t.Fatalf("NewScheduled failed: %v", err)
	}

	events := make(chan Event, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := trigger.Start(ctx, events); err != nil && err != context.Canceled {
			t.Errorf("Start failed: %v", err)
		}
	}()

	// Wait for at least one event
	select {
	case event := <-events:
		if event.RuleName != "test-rule" {
			t.Errorf("expected rule name test-rule, got %s", event.RuleName)
		}
		if event.Type != "scheduled" {
			t.Errorf("expected event type scheduled, got %s", event.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for scheduled event")
	}

	trigger.Stop()
}
