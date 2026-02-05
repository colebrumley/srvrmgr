// internal/daemon/daemon.go
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
	"github.com/colebrumley/srvrmgr/internal/executor"
	"github.com/colebrumley/srvrmgr/internal/logging"
	"github.com/colebrumley/srvrmgr/internal/template"
	"github.com/colebrumley/srvrmgr/internal/trigger"
)

// Daemon is the main server manager daemon
type Daemon struct {
	configPath string
	rulesDir   string
	config     *config.Global
	rules      map[string]*config.Rule
	triggers   map[string]trigger.Trigger
	events     chan trigger.Event
	logger     *slog.Logger
	webhooks   map[string]*trigger.Webhook
	httpServer *http.Server
	mu         sync.RWMutex
}

// New creates a new daemon instance
func New(configPath, rulesDir string) *Daemon {
	return &Daemon{
		configPath: configPath,
		rulesDir:   rulesDir,
		rules:      make(map[string]*config.Rule),
		triggers:   make(map[string]trigger.Trigger),
		events:     make(chan trigger.Event, 100),
		webhooks:   make(map[string]*trigger.Webhook),
	}
}

// Run starts the daemon and blocks until context is cancelled
func (d *Daemon) Run(ctx context.Context) error {
	// Load configuration
	if err := d.loadConfig(); err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Initialize logger
	d.logger = logging.NewLogger(
		d.config.Logging.Format,
		d.config.Daemon.LogLevel,
		os.Stdout,
	)

	d.logger.Info("starting daemon", "config", d.configPath, "rules_dir", d.rulesDir)

	// Load rules
	if err := d.loadRules(); err != nil {
		return fmt.Errorf("loading rules: %w", err)
	}

	// Initialize triggers
	if err := d.initTriggers(ctx); err != nil {
		return fmt.Errorf("initializing triggers: %w", err)
	}

	// Start webhook server if any webhook triggers
	if len(d.webhooks) > 0 {
		go d.startWebhookServer(ctx)
	}

	// Fire lifecycle:daemon_started
	d.fireLifecycleEvent("daemon_started")

	d.logger.Info("daemon started", "rules_loaded", len(d.rules))

	// Main event loop
	for {
		select {
		case event := <-d.events:
			go d.handleEvent(ctx, event)
		case <-ctx.Done():
			d.fireLifecycleEvent("daemon_stopped")
			d.logger.Info("daemon stopping")
			return d.shutdown()
		}
	}
}

func (d *Daemon) loadConfig() error {
	cfg, err := config.LoadGlobal(d.configPath)
	if err != nil {
		return err
	}
	d.config = cfg
	return nil
}

func (d *Daemon) loadRules() error {
	rules, err := config.LoadRulesDir(d.rulesDir)
	if err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, rule := range rules {
		d.rules[rule.Name] = rule
	}

	return nil
}

func (d *Daemon) initTriggers(ctx context.Context) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, rule := range d.rules {
		if !rule.Enabled {
			d.logger.Debug("skipping disabled rule", "rule", rule.Name)
			continue
		}

		t, err := trigger.New(rule.Name, rule.Trigger)
		if err != nil {
			d.logger.Error("failed to create trigger", "rule", rule.Name, "error", err)
			continue
		}

		d.triggers[rule.Name] = t

		// Track webhook triggers separately for HTTP routing
		if wh, ok := t.(*trigger.Webhook); ok {
			d.webhooks[wh.ListenPath()] = wh
		}

		// Start the trigger
		go func(t trigger.Trigger) {
			if err := t.Start(ctx, d.events); err != nil && err != context.Canceled {
				d.logger.Error("trigger error", "rule", t.RuleName(), "error", err)
			}
		}(t)
	}

	return nil
}

func (d *Daemon) startWebhookServer(ctx context.Context) {
	addr := fmt.Sprintf("%s:%d",
		d.config.Daemon.WebhookListenAddress,
		d.config.Daemon.WebhookListenPort,
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		d.mu.RLock()
		wh, ok := d.webhooks[r.URL.Path]
		d.mu.RUnlock()

		if !ok {
			http.NotFound(w, r)
			return
		}

		if wh.HandleRequest(r, d.events) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		} else {
			http.Error(w, "Forbidden", http.StatusForbidden)
		}
	})

	d.httpServer = &http.Server{Addr: addr, Handler: mux}

	d.logger.Info("starting webhook server", "address", addr)

	go func() {
		if err := d.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			d.logger.Error("webhook server error", "error", err)
		}
	}()

	<-ctx.Done()
	d.httpServer.Shutdown(context.Background())
}

func (d *Daemon) fireLifecycleEvent(eventType string) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, t := range d.triggers {
		if lt, ok := t.(*trigger.Lifecycle); ok {
			lt.Fire(eventType, d.events)
		}
	}
}

func (d *Daemon) handleEvent(ctx context.Context, event trigger.Event) {
	d.mu.RLock()
	rule, ok := d.rules[event.RuleName]
	d.mu.RUnlock()

	if !ok {
		d.logger.Error("rule not found for event", "rule", event.RuleName)
		return
	}

	logger := logging.WithRule(d.logger, rule.Name)
	logger.Info("handling event", "type", event.Type)

	// Build prompt from template
	prompt := template.Expand(rule.Action.Prompt, event.Data)

	// Merge rule claude config with defaults
	claudeCfg := d.mergeClaudeConfig(rule.Claude)

	// Handle dry_run
	if rule.DryRun {
		claudeCfg.PermissionMode = "plan"
	}

	// Determine working directory
	workDir := ""
	if len(claudeCfg.AddDirs) > 0 {
		workDir = claudeCfg.AddDirs[0]
	}

	// Execute with timeout
	timeout := 5 * time.Minute // Default timeout
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Get daemon executable path for memory MCP injection
	daemonPath, err := os.Executable()
	if err != nil {
		logger.Warn("could not determine daemon path, memory MCP disabled", "error", err)
		daemonPath = ""
	}

	memoryEnabled := d.isMemoryEnabled(rule)
	result, err := executor.ExecuteWithMemory(execCtx, prompt, claudeCfg, rule.RunAsUser, d.config.Logging.Debug, workDir, memoryEnabled, daemonPath)
	if err != nil {
		logger.Error("execution error", "error", err)
		d.handleFailure(ctx, rule, event, err)
		return
	}

	logger.Info("execution complete",
		"state", result.State,
		"duration", result.Duration,
	)

	if result.State == "success" {
		d.fireTriggeredRules(ctx, rule, event)
	} else {
		d.handleFailure(ctx, rule, event, fmt.Errorf("execution failed: %s", result.Error))
	}
}

func (d *Daemon) mergeClaudeConfig(ruleCfg config.ClaudeConfig) config.ClaudeConfig {
	defaults := d.config.ClaudeDefaults

	result := ruleCfg
	if result.Model == "" {
		result.Model = defaults.Model
	}
	if result.PermissionMode == "" {
		result.PermissionMode = defaults.PermissionMode
	}
	if result.MaxBudgetUSD == 0 {
		result.MaxBudgetUSD = defaults.MaxBudgetUSD
	}

	return result
}

func (d *Daemon) handleFailure(ctx context.Context, rule *config.Rule, event trigger.Event, err error) {
	logger := logging.WithRule(d.logger, rule.Name)

	if !rule.OnFailure.Retry {
		logger.Error("rule failed, no retry configured", "error", err)
		return
	}

	// TODO: Implement retry logic
	logger.Warn("rule failed, retry not yet implemented", "error", err)
}

func (d *Daemon) fireTriggeredRules(ctx context.Context, rule *config.Rule, event trigger.Event) {
	for _, triggerName := range rule.Triggers {
		d.events <- trigger.Event{
			RuleName:  triggerName,
			Type:      "triggered",
			Timestamp: time.Now(),
			Data:      event.Data,
		}
	}
}

func (d *Daemon) shutdown() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, t := range d.triggers {
		t.Stop()
	}

	return nil
}

// isMemoryEnabled determines if memory is enabled for a rule
func (d *Daemon) isMemoryEnabled(rule *config.Rule) bool {
	// Per-rule override takes precedence
	if rule.Claude.Memory != nil {
		return *rule.Claude.Memory
	}
	// Fall back to global config
	return d.config.Memory.Enabled
}

// RunRule manually runs a specific rule (for CLI use)
func (d *Daemon) RunRule(ctx context.Context, ruleName string, data map[string]any) error {
	if err := d.loadConfig(); err != nil {
		return err
	}

	d.logger = logging.NewLogger(d.config.Logging.Format, d.config.Daemon.LogLevel, os.Stdout)

	if err := d.loadRules(); err != nil {
		return err
	}

	_, ok := d.rules[ruleName]
	if !ok {
		return fmt.Errorf("rule not found: %s", ruleName)
	}

	event := trigger.Event{
		RuleName:  ruleName,
		Type:      "manual",
		Timestamp: time.Now(),
		Data:      data,
	}

	d.handleEvent(ctx, event)
	return nil
}
