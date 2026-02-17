// internal/trigger/factory_test.go
package trigger

import (
	"testing"

	"github.com/colebrumley/srvrmgr/internal/config"
)

func TestNewTrigger(t *testing.T) {
	tests := []struct {
		name        string
		triggerType string
		wantType    string
	}{
		{"filesystem", "filesystem", "*trigger.Filesystem"},
		{"scheduled", "scheduled", "*trigger.Scheduled"},
		{"webhook", "webhook", "*trigger.Webhook"},
		{"lifecycle", "lifecycle", "*trigger.Lifecycle"},
		{"manual", "manual", "*trigger.Manual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Trigger{
				Type:           tt.triggerType,
				WatchPaths:     []string{"/tmp"},
				OnEvents:       []string{"file_created"},
				CronExpression: "0 0 * * * *",
				ListenPath:     "/hooks/test",
				AllowedMethods: []string{"POST"},
			}

			trigger, err := New("test-rule", cfg, "")
			if err != nil {
				t.Fatalf("New failed: %v", err)
			}

			if trigger.RuleName() != "test-rule" {
				t.Errorf("expected rule name test-rule, got %s", trigger.RuleName())
			}
		})
	}
}

func TestNewTriggerUnknownType(t *testing.T) {
	cfg := config.Trigger{Type: "unknown"}
	_, err := New("test", cfg, "")
	if err == nil {
		t.Error("expected error for unknown trigger type")
	}
}
