// internal/trigger/webhook_test.go
package trigger

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
)

func TestWebhookTrigger(t *testing.T) {
	triggerCfg := config.Trigger{
		Type:           "webhook",
		ListenPath:     "/hooks/test",
		AllowedMethods: []string{"POST"},
	}

	trigger, err := NewWebhook("test-rule", triggerCfg)
	if err != nil {
		t.Fatalf("NewWebhook failed: %v", err)
	}

	// Create test request
	req := httptest.NewRequest("POST", "/hooks/test", strings.NewReader(`{"key":"value"}`))
	req.Header.Set("Content-Type", "application/json")

	events := make(chan Event, 10)

	// Handle the request
	trigger.HandleRequest(req, events)

	// Check event was sent
	select {
	case event := <-events:
		if event.RuleName != "test-rule" {
			t.Errorf("expected rule name test-rule, got %s", event.RuleName)
		}
		if event.Type != "webhook" {
			t.Errorf("expected event type webhook, got %s", event.Type)
		}
		body, ok := event.Data["http_body"].(string)
		if !ok || body != `{"key":"value"}` {
			t.Errorf("unexpected body: %v", event.Data["http_body"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestWebhookTriggerMethodNotAllowed(t *testing.T) {
	triggerCfg := config.Trigger{
		Type:           "webhook",
		ListenPath:     "/hooks/test",
		AllowedMethods: []string{"POST"},
	}

	trigger, _ := NewWebhook("test-rule", triggerCfg)

	req := httptest.NewRequest("GET", "/hooks/test", nil)
	events := make(chan Event, 10)

	trigger.HandleRequest(req, events)

	// Should not send event for GET
	select {
	case <-events:
		t.Error("unexpected event for disallowed method")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}
