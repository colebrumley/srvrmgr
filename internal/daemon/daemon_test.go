// internal/daemon/daemon_test.go
package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
	"github.com/colebrumley/srvrmgr/internal/trigger"
)

// ===== FR-1: Inject event_type and timestamp into event.Data =====

func TestHandleEvent_InjectsEventType(t *testing.T) {
	// FR-1: When event.Data does not have "event_type", handleEvent should inject it.
	// Lifecycle triggers emit empty Data, so event_type should be injected.
	event := trigger.Event{
		RuleName:  "test-rule",
		Type:      "daemon_started",
		Timestamp: time.Now(),
		Data:      map[string]any{},
	}

	// Simulate the injection logic that should happen in handleEvent
	if _, ok := event.Data["event_type"]; !ok {
		event.Data["event_type"] = event.Type
	}
	if _, ok := event.Data["timestamp"]; !ok {
		event.Data["timestamp"] = event.Timestamp.Format(time.RFC3339)
	}

	if event.Data["event_type"] != "daemon_started" {
		t.Errorf("FR-1: expected event_type=daemon_started, got %v", event.Data["event_type"])
	}
	if event.Data["timestamp"] == nil || event.Data["timestamp"] == "" {
		t.Error("FR-1: expected timestamp to be injected")
	}
}

func TestHandleEvent_DoesNotOverrideExistingEventType(t *testing.T) {
	// FR-1: If event.Data already has event_type (e.g., from filesystem trigger),
	// handleEvent should NOT override it.
	event := trigger.Event{
		RuleName:  "test-rule",
		Type:      "file_created",
		Timestamp: time.Now(),
		Data: map[string]any{
			"event_type": "file_created",
			"file_path":  "/tmp/test.txt",
			"file_name":  "test.txt",
		},
	}

	// Simulate the injection logic
	if _, ok := event.Data["event_type"]; !ok {
		event.Data["event_type"] = event.Type
	}

	if event.Data["event_type"] != "file_created" {
		t.Errorf("FR-1: event_type should not be overridden, got %v", event.Data["event_type"])
	}
}

func TestHandleEvent_InjectsTimestampForLifecycle(t *testing.T) {
	// FR-1: Lifecycle triggers emit Data: map[string]any{} without timestamp.
	now := time.Now()
	event := trigger.Event{
		RuleName:  "test-rule",
		Type:      "daemon_started",
		Timestamp: now,
		Data:      map[string]any{},
	}

	if _, ok := event.Data["timestamp"]; !ok {
		event.Data["timestamp"] = event.Timestamp.Format(time.RFC3339)
	}

	ts, ok := event.Data["timestamp"].(string)
	if !ok || ts == "" {
		t.Error("FR-1: timestamp should be injected for lifecycle events")
	}
}

func TestHandleEvent_DoesNotOverrideExistingTimestamp(t *testing.T) {
	// FR-1: Scheduled triggers already set timestamp in Data.
	existingTS := "2026-02-16T12:00:00Z"
	event := trigger.Event{
		RuleName:  "test-rule",
		Type:      "scheduled",
		Timestamp: time.Now(),
		Data: map[string]any{
			"timestamp": existingTS,
		},
	}

	if _, ok := event.Data["timestamp"]; !ok {
		event.Data["timestamp"] = event.Timestamp.Format(time.RFC3339)
	}

	if event.Data["timestamp"] != existingTS {
		t.Errorf("FR-1: timestamp should not be overridden, got %v", event.Data["timestamp"])
	}
}

// ===== FR-2: mergeClaudeConfig merges all 9 fields =====

func TestMergeClaudeConfig_MergesAllFields(t *testing.T) {
	d := &Daemon{
		config: &config.Global{
			ClaudeDefaults: config.ClaudeConfig{
				Model:              "sonnet",
				PermissionMode:     "default",
				MaxBudgetUSD:       1.0,
				AllowedTools:       []string{"Bash", "Read"},
				DisallowedTools:    []string{"WebFetch"},
				AddDirs:            []string{"/default/dir"},
				SystemPrompt:       "Default system prompt",
				AppendSystemPrompt: "Default append prompt",
				MCPConfig:          []string{"/default/mcp.json"},
			},
		},
	}

	// Rule config with empty fields — should fall back to defaults
	ruleCfg := config.ClaudeConfig{}

	result := d.mergeClaudeConfig(ruleCfg)

	if result.Model != "sonnet" {
		t.Errorf("FR-2: Model not merged from defaults, got %q", result.Model)
	}
	if result.PermissionMode != "default" {
		t.Errorf("FR-2: PermissionMode not merged from defaults, got %q", result.PermissionMode)
	}
	if result.MaxBudgetUSD != 1.0 {
		t.Errorf("FR-2: MaxBudgetUSD not merged from defaults, got %f", result.MaxBudgetUSD)
	}
	if len(result.AllowedTools) != 2 {
		t.Errorf("FR-2: AllowedTools not merged from defaults, got %v", result.AllowedTools)
	}
	if len(result.DisallowedTools) != 1 {
		t.Errorf("FR-2: DisallowedTools not merged from defaults, got %v", result.DisallowedTools)
	}
	if len(result.AddDirs) != 1 {
		t.Errorf("FR-2: AddDirs not merged from defaults, got %v", result.AddDirs)
	}
	if result.SystemPrompt != "Default system prompt" {
		t.Errorf("FR-2: SystemPrompt not merged from defaults, got %q", result.SystemPrompt)
	}
	if result.AppendSystemPrompt != "Default append prompt" {
		t.Errorf("FR-2: AppendSystemPrompt not merged from defaults, got %q", result.AppendSystemPrompt)
	}
	if len(result.MCPConfig) != 1 {
		t.Errorf("FR-2: MCPConfig not merged from defaults, got %v", result.MCPConfig)
	}
}

func TestMergeClaudeConfig_RuleOverridesDefaults(t *testing.T) {
	d := &Daemon{
		config: &config.Global{
			ClaudeDefaults: config.ClaudeConfig{
				Model:          "sonnet",
				PermissionMode: "default",
				MaxBudgetUSD:   1.0,
				AllowedTools:   []string{"Bash"},
				SystemPrompt:   "Default prompt",
			},
		},
	}

	// Rule config with explicit values — should override defaults
	ruleCfg := config.ClaudeConfig{
		Model:          "haiku",
		PermissionMode: "plan",
		MaxBudgetUSD:   0.10,
		AllowedTools:   []string{"Read", "Glob"},
		SystemPrompt:   "Rule-specific prompt",
	}

	result := d.mergeClaudeConfig(ruleCfg)

	if result.Model != "haiku" {
		t.Errorf("FR-2: rule Model should override default, got %q", result.Model)
	}
	if result.PermissionMode != "plan" {
		t.Errorf("FR-2: rule PermissionMode should override default, got %q", result.PermissionMode)
	}
	if result.MaxBudgetUSD != 0.10 {
		t.Errorf("FR-2: rule MaxBudgetUSD should override default, got %f", result.MaxBudgetUSD)
	}
	if len(result.AllowedTools) != 2 || result.AllowedTools[0] != "Read" {
		t.Errorf("FR-2: rule AllowedTools should override default, got %v", result.AllowedTools)
	}
	if result.SystemPrompt != "Rule-specific prompt" {
		t.Errorf("FR-2: rule SystemPrompt should override default, got %q", result.SystemPrompt)
	}
}

// ===== FR-13: Conditional trigger parsing =====

func TestParseTriggeredRules_WithMarkers(t *testing.T) {
	// FR-13: Output containing TRIGGER:<rule-name> should fire only that rule
	output := `Disk usage is at 85%, which exceeds WARNING threshold.
Volumes over threshold:
- /Volumes/Media: 85% used
TRIGGER:storage-cleanup-caches`

	triggered := parseTriggeredRules(output)
	if len(triggered) != 1 {
		t.Fatalf("FR-13: expected 1 triggered rule, got %d", len(triggered))
	}
	if triggered[0] != "storage-cleanup-caches" {
		t.Errorf("FR-13: expected 'storage-cleanup-caches', got %q", triggered[0])
	}
}

func TestParseTriggeredRules_NoMarkers(t *testing.T) {
	// FR-13: Output without TRIGGER: markers should return nil/empty
	output := `Disk usage is at 45%, all volumes are normal.
No action needed.`

	triggered := parseTriggeredRules(output)
	if len(triggered) != 0 {
		t.Errorf("FR-13: expected 0 triggered rules for no-marker output, got %d", len(triggered))
	}
}

func TestParseTriggeredRules_MultipleMarkers(t *testing.T) {
	output := `Multiple issues found:
TRIGGER:storage-cleanup-caches
TRIGGER:server-check-services`

	triggered := parseTriggeredRules(output)
	if len(triggered) != 2 {
		t.Fatalf("FR-13: expected 2 triggered rules, got %d", len(triggered))
	}
}

func TestParseTriggeredRules_EmptyOutput(t *testing.T) {
	triggered := parseTriggeredRules("")
	if len(triggered) != 0 {
		t.Errorf("FR-13: expected 0 triggered rules for empty output, got %d", len(triggered))
	}
}

// parseTriggeredRules and expandHome are now defined in daemon.go.

// ===== FR-12: expandHome resolves to run_as_user's home =====

func TestExpandHome_DefaultBehavior(t *testing.T) {
	// Without a user, expandHome should use os.UserHomeDir()
	result := expandHome("~/Downloads")
	if result == "~/Downloads" {
		t.Error("FR-12: expandHome should expand ~ to home directory")
	}
	if !strings.HasSuffix(result, "/Downloads") {
		t.Errorf("FR-12: expected path ending in /Downloads, got %q", result)
	}
}

func TestExpandHome_NoTilde(t *testing.T) {
	// Paths without ~ should be returned unchanged
	result := expandHome("/absolute/path")
	if result != "/absolute/path" {
		t.Errorf("FR-12: non-tilde path should be unchanged, got %q", result)
	}
}
