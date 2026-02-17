// internal/daemon/daemon.go
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
	"github.com/colebrumley/srvrmgr/internal/executor"
	"github.com/colebrumley/srvrmgr/internal/logging"
	"github.com/colebrumley/srvrmgr/internal/security"
	"github.com/colebrumley/srvrmgr/internal/state"
	"github.com/colebrumley/srvrmgr/internal/template"
	"github.com/colebrumley/srvrmgr/internal/trigger"
	"github.com/fsnotify/fsnotify"
)

// Daemon is the main server manager daemon
type Daemon struct {
	configPath   string
	rulesDir     string
	config       *config.Global
	rules        map[string]*config.Rule
	triggers     map[string]trigger.Trigger
	events       chan trigger.Event
	logger       *slog.Logger
	webhooks     map[string]*trigger.Webhook
	httpServer   *http.Server
	daemonPath   string           // Path to daemon executable for MCP stdio transport
	lastRunState map[string]string // tracks last execution state per rule name
	stateDB      *state.DB        // FR-5: execution history persistence
	startTime    time.Time        // FR-7: daemon start time for uptime
	mu           sync.RWMutex
	sem          chan struct{}   // concurrency limiter
	wg           sync.WaitGroup // tracks in-flight event handlers
}

// New creates a new daemon instance
func New(configPath, rulesDir string) *Daemon {
	return &Daemon{
		configPath:   configPath,
		rulesDir:     rulesDir,
		rules:        make(map[string]*config.Rule),
		triggers:     make(map[string]trigger.Trigger),
		events:       make(chan trigger.Event, 100),
		webhooks:     make(map[string]*trigger.Webhook),
		lastRunState: make(map[string]string),
	}
}

// Run starts the daemon and blocks until context is cancelled
func (d *Daemon) Run(ctx context.Context) error {
	// Sourced from architect — startTime set in Run(), not New()
	d.startTime = time.Now()

	// Load configuration
	if err := d.loadConfig(); err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// FR-6: Initialize logger with rotating writer.
	// Sourced from architect — RotatingWriter with stdout fallback.
	logWriter, err := d.initLogWriter()
	if err != nil {
		d.logger = logging.NewLogger(d.config.Logging.Format, d.config.Daemon.LogLevel, os.Stdout)
		d.logger.Warn("failed to initialize rotating log writer, using stdout", "error", err)
	} else {
		d.logger = logging.NewLogger(d.config.Logging.Format, d.config.Daemon.LogLevel, logWriter)
	}

	d.logger.Info("starting daemon", "config", d.configPath, "rules_dir", d.rulesDir)

	// Get daemon path for MCP stdio transport
	if d.config.Memory.Enabled {
		daemonPath, err := os.Executable()
		if err != nil {
			d.logger.Warn("could not determine daemon path, memory disabled", "error", err)
		} else {
			d.daemonPath = daemonPath
		}
	}

	// FR-5: Initialize state database.
	// Sourced from architect — separate initStateDB with NFR-1 cleanup goroutine.
	if err := d.initStateDB(); err != nil {
		d.logger.Warn("failed to initialize state database, history will not be recorded", "error", err)
	}

	// FR-14: Validate rules directory permissions before loading.
	// FIX: Log CRITICAL and continue (not hard-fail like convention, not silent like architect).
	if err := security.ValidateDirectoryPermissions(d.rulesDir); err != nil {
		d.logger.Error("CRITICAL: rules directory has unsafe permissions", "error", err, "path", d.rulesDir)
		// Log critical but continue — the operator should fix permissions
	}

	// Load rules
	if err := d.loadRules(); err != nil {
		return fmt.Errorf("loading rules: %w", err)
	}

	// FR-5: Initialize lastRunState from DB.
	// Sourced from convention — bulk GetHistory is more efficient than per-rule GetLastState.
	d.initLastRunStateFromDB()

	// Initialize triggers
	if err := d.initTriggers(ctx); err != nil {
		return fmt.Errorf("initializing triggers: %w", err)
	}

	// FR-7: Always start HTTP server (not conditional on webhooks)
	go d.startHTTPServer(ctx)

	// FR-4: Start hot-reload watcher.
	// Sourced from convention — debounce channel pattern.
	go d.startHotReload(ctx)

	// Fire lifecycle:daemon_started
	d.fireLifecycleEvent("daemon_started")

	d.logger.Info("daemon started", "rules_loaded", len(d.rules))

	// Initialize concurrency limiter
	d.sem = make(chan struct{}, d.config.RuleExecution.MaxConcurrent)

	// Main event loop
	for {
		select {
		case event := <-d.events:
			d.sem <- struct{}{} // acquire semaphore
			d.wg.Add(1)
			go func() {
				defer func() {
					<-d.sem // release semaphore
					d.wg.Done()
				}()
				d.handleEvent(ctx, event)
			}()
		case <-ctx.Done():
			d.logger.Info("daemon stopping, waiting for in-flight handlers")
			d.wg.Wait() // wait for in-flight handlers to finish
			// Use a fresh context for shutdown lifecycle events since parent is cancelled
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			d.handleLifecycleShutdown(shutdownCtx)
			shutdownCancel()
			return d.shutdown()
		}
	}
}

// initLogWriter creates a rotating log writer (FR-6).
// Sourced from architect — clean separation into helper.
func (d *Daemon) initLogWriter() (*logging.RotatingWriter, error) {
	logDir := "/Library/Logs/srvrmgr"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}
	logPath := filepath.Join(logDir, "srvrmgrd.log")
	return logging.NewRotatingWriter(logPath, 50*1024*1024) // 50MB
}

// initStateDB opens the state database (FR-5).
// Sourced from architect — separate method with NFR-1 cleanup goroutine.
func (d *Daemon) initStateDB() error {
	dbPath := filepath.Join("/Library/Application Support/srvrmgr/state/history.db")
	db, err := state.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening state database: %w", err)
	}
	d.stateDB = db

	// NFR-1: Run cleanup of old records (90-day retention).
	// Sourced from architect.
	go func() {
		if deleted, err := db.Cleanup(90); err != nil {
			d.logger.Warn("state cleanup failed", "error", err)
		} else if deleted > 0 {
			d.logger.Info("cleaned up old execution records", "deleted", deleted)
		}
	}()

	return nil
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
		// FR-15: Validate run_as_user against allowlist.
		// Sourced from convention — enforce by skipping disallowed rules.
		if rule.RunAsUser != "" && len(d.config.Daemon.AllowedRunAsUsers) > 0 {
			allowed := false
			for _, u := range d.config.Daemon.AllowedRunAsUsers {
				if u == rule.RunAsUser {
					allowed = true
					break
				}
			}
			if !allowed {
				if d.logger != nil {
					d.logger.Error("rule run_as_user not in allowlist, skipping",
						"rule", rule.Name,
						"run_as_user", rule.RunAsUser,
						"allowed", d.config.Daemon.AllowedRunAsUsers,
					)
				}
				continue
			}
		}
		d.rules[rule.Name] = rule
	}

	// FR-15/FR-19: Run global-context validation for warnings.
	// Sourced from architect — ValidateRuleWithGlobal returns warnings for overlap detection.
	if d.config != nil {
		for _, rule := range d.rules {
			warnings := config.ValidateRuleWithGlobal(rule, d.config, d.rules)
			for _, w := range warnings {
				if d.logger != nil {
					d.logger.Warn(w)
				}
			}
		}
	}

	return nil
}

func (d *Daemon) initTriggers(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, rule := range d.rules {
		if !rule.Enabled {
			d.logger.Debug("skipping disabled rule", "rule", rule.Name)
			continue
		}

		// FR-12: Pass runAsUser to trigger factory.
		// Sourced from convention — 3-param New() avoids filesystem special-casing.
		t, err := trigger.New(rule.Name, rule.Trigger, rule.RunAsUser)
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

// FR-7: startHTTPServer starts the HTTP server with health, API, and webhook endpoints.
// Combines architect's method guards with convention's typed ruleStatus and inline rate limiter.
func (d *Daemon) startHTTPServer(ctx context.Context) {
	addr := fmt.Sprintf("%s:%d",
		d.config.Daemon.WebhookListenAddress,
		d.config.Daemon.WebhookListenPort,
	)

	mux := http.NewServeMux()

	// FR-7: Health check endpoint
	mux.HandleFunc("/health", rateLimitHandler(60, d.handleHealth))

	// FR-7: API endpoints
	mux.HandleFunc("/api/rules", rateLimitHandler(30, d.handleAPIRules))
	mux.HandleFunc("/api/history", rateLimitHandler(30, d.handleAPIHistory))

	// Webhook handler (catch-all)
	mux.HandleFunc("/", rateLimitHandler(10, func(w http.ResponseWriter, r *http.Request) {
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
	}))

	d.httpServer = &http.Server{Addr: addr, Handler: mux}

	d.logger.Info("starting HTTP server", "address", addr)

	go func() {
		if err := d.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			d.logger.Error("HTTP server error", "error", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d.httpServer.Shutdown(shutdownCtx)
}

// handleHealth returns daemon health status.
// Sourced from architect — includes HTTP method guard.
func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	d.mu.RLock()
	rulesLoaded := len(d.rules)
	rulesEnabled := 0
	for _, rule := range d.rules {
		if rule.Enabled {
			rulesEnabled++
		}
	}
	d.mu.RUnlock()

	uptime := time.Since(d.startTime).Truncate(time.Second).String()
	resp := map[string]any{
		"status":        "ok",
		"uptime":        uptime,
		"rules_loaded":  rulesLoaded,
		"rules_enabled": rulesEnabled,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleAPIRules returns all rules with their current state.
// Combines architect's method guard with convention's typed ruleStatus struct.
func (d *Daemon) handleAPIRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	// Sourced from convention — typed struct with JSON tags for stable API contract.
	type ruleStatus struct {
		Name      string `json:"name"`
		Enabled   bool   `json:"enabled"`
		DryRun    bool   `json:"dry_run"`
		LastState string `json:"last_state,omitempty"`
	}

	var rules []ruleStatus
	for _, rule := range d.rules {
		rs := ruleStatus{
			Name:    rule.Name,
			Enabled: rule.Enabled,
			DryRun:  rule.DryRun,
		}
		if st, ok := d.lastRunState[rule.Name]; ok {
			rs.LastState = st
		}
		rules = append(rules, rs)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rules)
}

// handleAPIHistory returns execution history from the state DB.
// Sourced from architect — includes method guard.
func (d *Daemon) handleAPIHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if d.stateDB == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{})
		return
	}

	ruleName := r.URL.Query().Get("rule")
	stateFilter := r.URL.Query().Get("state")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if limit > 500 {
		limit = 500
	}

	records, err := d.stateDB.GetHistory(ruleName, stateFilter, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("querying history: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(records)
}

// rateLimitHandler wraps an HTTP handler with a simple token-bucket rate limiter (FR-7).
// Sourced from convention — standalone function with closure state avoids sync.Map issues.
func rateLimitHandler(requestsPerMinute int, handler http.HandlerFunc) http.HandlerFunc {
	var mu sync.Mutex
	tokens := requestsPerMinute
	lastRefill := time.Now()

	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		now := time.Now()
		elapsed := now.Sub(lastRefill)
		refill := int(elapsed.Minutes() * float64(requestsPerMinute))
		if refill > 0 {
			tokens += refill
			if tokens > requestsPerMinute {
				tokens = requestsPerMinute
			}
			lastRefill = now
		}

		if tokens <= 0 {
			mu.Unlock()
			w.Header().Set("Retry-After", "60")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		tokens--
		mu.Unlock()

		handler(w, r)
	}
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

// handleLifecycleShutdown directly handles daemon_stopped events with the given context,
// bypassing the event channel which is no longer being read after ctx cancellation.
func (d *Daemon) handleLifecycleShutdown(ctx context.Context) {
	d.mu.RLock()
	var lifecycleRules []string
	for _, t := range d.triggers {
		if lt, ok := t.(*trigger.Lifecycle); ok && lt.ShouldFireOn("daemon_stopped") {
			lifecycleRules = append(lifecycleRules, lt.RuleName())
		}
	}
	d.mu.RUnlock()

	for _, ruleName := range lifecycleRules {
		event := trigger.Event{
			RuleName:  ruleName,
			Type:      "daemon_stopped",
			Timestamp: time.Now(),
			Data:      map[string]any{},
		}
		d.handleEvent(ctx, event)
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

	// FR-1: Inject default event_type and timestamp if not present
	if event.Data == nil {
		event.Data = map[string]any{}
	}
	if _, ok := event.Data["event_type"]; !ok {
		event.Data["event_type"] = event.Type
	}
	if _, ok := event.Data["timestamp"]; !ok {
		event.Data["timestamp"] = event.Timestamp.Format(time.RFC3339)
	}

	// Check dependencies before execution
	if !d.checkDependencies(rule) {
		logger.Warn("skipping rule, dependencies not met", "depends_on", rule.DependsOn)
		return
	}

	// FR-5: Record start time
	startedAt := time.Now()

	// Execute rule
	result, err := d.executeRule(ctx, rule, event)
	if err != nil {
		logger.Error("execution error", "error", err)
		// FR-5: Record failed execution
		d.recordExecution(rule, event, "failure", startedAt, "", err.Error())
		d.handleFailure(ctx, rule, event, err)
		return
	}

	logger.Info("execution complete",
		"state", result.State,
		"duration", result.Duration,
	)

	// FR-18: Scrub output before storage
	scrubbedOutput := security.ScrubOutput(result.Output)

	// FR-5: Record execution
	d.recordExecution(rule, event, result.State, startedAt, scrubbedOutput, result.Error)

	// Track execution state
	d.recordExecutionState(rule.Name, result.State)

	switch result.State {
	case "success":
		// FR-13: Conditional trigger chains
		d.fireTriggeredRules(ctx, rule, event, result.Output)
	case "cancelled":
		logger.Info("execution cancelled (shutdown)")
	default:
		d.handleFailure(ctx, rule, event, fmt.Errorf("execution failed: %s", result.Error))
	}
}

// executeRule performs the actual rule execution (template expand, config merge, Claude call)
func (d *Daemon) executeRule(ctx context.Context, rule *config.Rule, event trigger.Event) (*executor.Result, error) {
	prompt := template.Expand(rule.Action.Prompt, event.Data)
	claudeCfg := d.mergeClaudeConfig(rule.Claude)

	if rule.DryRun {
		claudeCfg.PermissionMode = "plan"
	}

	// FR-12: Expand ~ in add_dirs using run_as_user's home directory.
	// Sourced from architect — expand ALL AddDirs, not just the first.
	workDir := ""
	if len(claudeCfg.AddDirs) > 0 {
		workDir = expandHomeForUser(claudeCfg.AddDirs[0], rule.RunAsUser)
	}
	for i, dir := range claudeCfg.AddDirs {
		claudeCfg.AddDirs[i] = expandHomeForUser(dir, rule.RunAsUser)
	}

	// FR-3: Per-rule timeout configuration
	timeout := 5 * time.Minute
	if rule.MaxTimeoutSeconds > 0 {
		timeout = time.Duration(rule.MaxTimeoutSeconds) * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	memoryEnabled := d.isMemoryEnabled(rule)
	return executor.ExecuteWithMemory(execCtx, prompt, claudeCfg, rule.RunAsUser, d.config.Logging.Debug, workDir, memoryEnabled, d.daemonPath)
}

// FR-2: mergeClaudeConfig merges all 9 ClaudeConfig fields.
// Both implementations have identical logic here.
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
	// FR-2: Merge slice fields
	if len(result.AllowedTools) == 0 {
		result.AllowedTools = defaults.AllowedTools
	}
	if len(result.DisallowedTools) == 0 {
		result.DisallowedTools = defaults.DisallowedTools
	}
	if len(result.AddDirs) == 0 {
		result.AddDirs = defaults.AddDirs
	}
	if len(result.MCPConfig) == 0 {
		result.MCPConfig = defaults.MCPConfig
	}
	// FR-2: Merge string fields
	if result.SystemPrompt == "" {
		result.SystemPrompt = defaults.SystemPrompt
	}
	if result.AppendSystemPrompt == "" {
		result.AppendSystemPrompt = defaults.AppendSystemPrompt
	}

	return result
}

func (d *Daemon) handleFailure(ctx context.Context, rule *config.Rule, event trigger.Event, err error) {
	logger := logging.WithRule(d.logger, rule.Name)

	if !rule.OnFailure.Retry {
		logger.Error("rule failed, no retry configured", "error", err)
		return
	}

	maxAttempts := rule.OnFailure.RetryAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	delay := time.Duration(rule.OnFailure.RetryDelaySeconds) * time.Second
	if delay <= 0 {
		delay = 30 * time.Second
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		logger.Warn("retrying rule execution",
			"attempt", attempt,
			"max_attempts", maxAttempts,
			"delay", delay,
			"previous_error", err,
		)

		// Wait before retry, respecting context cancellation
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			logger.Info("retry cancelled (shutdown)", "attempt", attempt)
			return
		}

		// Re-execute the rule
		result, execErr := d.executeRule(ctx, rule, event)
		if execErr != nil {
			err = execErr
			continue
		}
		if result.State == "success" {
			logger.Info("retry succeeded", "attempt", attempt)
			d.recordExecutionState(rule.Name, "success")
			d.fireTriggeredRules(ctx, rule, event, result.Output)
			return
		}
		if result.State == "cancelled" {
			logger.Info("retry cancelled (shutdown)", "attempt", attempt)
			return
		}
		err = fmt.Errorf("execution failed: %s", result.Error)
	}

	logger.Error("rule failed after all retries",
		"attempts", maxAttempts,
		"last_error", err,
	)
	d.recordExecutionState(rule.Name, "failure")
}

// recordExecutionState tracks the last execution state for a rule.
func (d *Daemon) recordExecutionState(ruleName, state string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastRunState[ruleName] = state
}

// FR-5: recordExecution stores an execution record in the state DB.
// Sourced from convention — cleaner parameter list without separate finishedAt.
func (d *Daemon) recordExecution(rule *config.Rule, event trigger.Event, resultState string, startedAt time.Time, output, errMsg string) {
	if d.stateDB == nil {
		return
	}

	// Truncate output to 10KB
	if len(output) > 10240 {
		output = output[:10240]
	}

	// Serialize event data (truncate to 1KB)
	eventData := ""
	if event.Data != nil {
		if data, err := json.Marshal(event.Data); err == nil {
			eventData = string(data)
			if len(eventData) > 1024 {
				eventData = eventData[:1024]
			}
		}
	}

	rec := state.ExecutionRecord{
		RuleName:    rule.Name,
		TriggerType: event.Type,
		State:       resultState,
		StartedAt:   startedAt,
		FinishedAt:  time.Now(),
		DurationMs:  time.Since(startedAt).Milliseconds(),
		EventData:   eventData,
		Error:       errMsg,
		Output:      output,
		DryRun:      rule.DryRun,
	}

	if _, err := d.stateDB.RecordExecution(rec); err != nil {
		if d.logger != nil {
			d.logger.Warn("failed to record execution", "rule", rule.Name, "error", err)
		}
	}
}

// FR-5: initLastRunStateFromDB populates lastRunState from the state DB on startup.
// Sourced from convention — bulk GetHistory is more efficient than per-rule GetLastState.
func (d *Daemon) initLastRunStateFromDB() {
	if d.stateDB == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Get recent history to populate lastRunState
	records, err := d.stateDB.GetHistory("", "", 100)
	if err != nil {
		if d.logger != nil {
			d.logger.Warn("could not load state from DB", "error", err)
		}
		return
	}

	// Records are ordered newest-first; only keep the first (most recent) per rule
	for _, rec := range records {
		if _, ok := d.lastRunState[rec.RuleName]; !ok {
			d.lastRunState[rec.RuleName] = rec.State
		}
	}
}

// checkDependencies checks if all depends_on_rules have completed successfully.
// Returns true if there are no dependencies or all dependencies have succeeded.
func (d *Daemon) checkDependencies(rule *config.Rule) bool {
	if len(rule.DependsOn) == 0 {
		return true
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, dep := range rule.DependsOn {
		state, ok := d.lastRunState[dep]
		if !ok {
			return false // dependency hasn't run yet
		}
		if state != "success" {
			return false // dependency didn't succeed
		}
	}
	return true
}

// FR-13: fireTriggeredRules fires triggered rules based on output content.
// If output contains TRIGGER:<rule-name> markers, only those specific rules fire.
// If no markers are found, all triggers_rules fire (backward compatible).
// Both implementations have the same logic.
func (d *Daemon) fireTriggeredRules(ctx context.Context, rule *config.Rule, event trigger.Event, output string) {
	if len(rule.Triggers) == 0 {
		return
	}

	logger := logging.WithRule(d.logger, rule.Name)

	// FR-13: Parse output for TRIGGER: markers
	triggered := parseTriggeredRules(output)

	if len(triggered) > 0 {
		// Only fire rules that appear in both triggers_rules and TRIGGER: markers
		triggerSet := make(map[string]bool)
		for _, name := range triggered {
			triggerSet[name] = true
		}

		for _, triggerName := range rule.Triggers {
			if triggerSet[triggerName] {
				logger.Info("conditional trigger fired", "triggered_rule", triggerName)
				select {
				case d.events <- trigger.Event{
					RuleName:  triggerName,
					Type:      "triggered",
					Timestamp: time.Now(),
					Data:      event.Data,
				}:
				default:
					logger.Warn("event channel full, dropping triggered rule", "rule", triggerName)
				}
			} else {
				logger.Debug("conditional trigger suppressed", "triggered_rule", triggerName)
			}
		}
	} else {
		// No markers: fire all triggers_rules (backward compatible)
		for _, triggerName := range rule.Triggers {
			select {
			case d.events <- trigger.Event{
				RuleName:  triggerName,
				Type:      "triggered",
				Timestamp: time.Now(),
				Data:      event.Data,
			}:
			default:
				logger.Warn("event channel full, dropping triggered rule", "rule", triggerName)
			}
		}
	}
}

// FR-13: parseTriggeredRules scans output for TRIGGER:<rule-name> markers.
func parseTriggeredRules(output string) []string {
	if output == "" {
		return nil
	}

	var triggered []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "TRIGGER:") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "TRIGGER:"))
			if name != "" {
				triggered = append(triggered, name)
			}
		}
	}
	return triggered
}

// FR-4: startHotReload watches the rules directory for changes and reloads rules.
// Sourced from convention — debounce channel pattern is cleaner.
func (d *Daemon) startHotReload(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		d.logger.Error("could not create rules watcher", "error", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(d.rulesDir); err != nil {
		d.logger.Error("could not watch rules directory", "error", err, "dir", d.rulesDir)
		return
	}

	d.logger.Info("hot-reload watcher started", "dir", d.rulesDir)

	// Debounce: wait 1 second after last event before reloading
	var debounceTimer *time.Timer
	debounceCh := make(chan struct{}, 1)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			ext := filepath.Ext(event.Name)
			if ext != ".yaml" && ext != ".yml" {
				continue
			}

			// Reset debounce timer
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(1*time.Second, func() {
				select {
				case debounceCh <- struct{}{}:
				default:
				}
			})

		case <-debounceCh:
			d.logger.Info("reloading rules (hot-reload)")
			d.reloadRules(ctx)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			d.logger.Error("rules watcher error", "error", err)

		case <-ctx.Done():
			return
		}
	}
}

// reloadRules re-validates and reloads rules from the rules directory.
// Sourced from convention — includes change detection and FR-15 re-validation.
func (d *Daemon) reloadRules(ctx context.Context) {
	// FR-14: Validate rules directory permissions before reloading
	if err := security.ValidateDirectoryPermissions(d.rulesDir); err != nil {
		d.logger.Error("CRITICAL: rules directory has unsafe permissions during reload", "error", err)
		return
	}

	rules, err := config.LoadRulesDir(d.rulesDir)
	if err != nil {
		d.logger.Error("failed to reload rules", "error", err)
		return
	}

	newRules := make(map[string]*config.Rule)
	for _, rule := range rules {
		// FR-15: Validate run_as_user against allowlist during reload too
		if rule.RunAsUser != "" && len(d.config.Daemon.AllowedRunAsUsers) > 0 {
			allowed := false
			for _, u := range d.config.Daemon.AllowedRunAsUsers {
				if u == rule.RunAsUser {
					allowed = true
					break
				}
			}
			if !allowed {
				d.logger.Error("rule run_as_user not in allowlist, skipping",
					"rule", rule.Name, "run_as_user", rule.RunAsUser)
				continue
			}
		}
		newRules[rule.Name] = rule
	}

	d.mu.Lock()
	// Stop triggers for removed rules
	for name, t := range d.triggers {
		if _, exists := newRules[name]; !exists {
			d.logger.Info("stopping trigger for removed rule", "rule", name)
			t.Stop()
			delete(d.triggers, name)
			delete(d.rules, name)
		}
	}

	// Add/update rules — with change detection from convention
	for name, rule := range newRules {
		oldRule, existed := d.rules[name]
		d.rules[name] = rule

		if !rule.Enabled {
			if t, ok := d.triggers[name]; ok {
				t.Stop()
				delete(d.triggers, name)
			}
			continue
		}

		// If rule is new or changed, restart its trigger
		needsRestart := !existed || oldRule == nil
		if !needsRestart {
			needsRestart = oldRule.Trigger.Type != rule.Trigger.Type ||
				oldRule.Trigger.CronExpression != rule.Trigger.CronExpression ||
				oldRule.Trigger.RunEvery != rule.Trigger.RunEvery ||
				oldRule.Trigger.RunAt != rule.Trigger.RunAt ||
				!sliceEqual(oldRule.Trigger.WatchPaths, rule.Trigger.WatchPaths) ||
				!sliceEqual(oldRule.Trigger.OnEvents, rule.Trigger.OnEvents)
		}

		if needsRestart {
			// Stop old trigger
			if t, ok := d.triggers[name]; ok {
				t.Stop()
				delete(d.triggers, name)
			}

			// Create and start new trigger
			t, err := trigger.New(rule.Name, rule.Trigger, rule.RunAsUser)
			if err != nil {
				d.logger.Error("failed to create trigger during reload", "rule", rule.Name, "error", err)
				continue
			}
			d.triggers[name] = t

			if wh, ok := t.(*trigger.Webhook); ok {
				d.webhooks[wh.ListenPath()] = wh
			}

			go func(t trigger.Trigger) {
				if err := t.Start(ctx, d.events); err != nil && err != context.Canceled {
					d.logger.Error("trigger error after reload", "rule", t.RuleName(), "error", err)
				}
			}(t)

			d.logger.Info("reloaded trigger", "rule", name)
		}
	}
	d.mu.Unlock()

	d.logger.Info("rules reloaded", "rules_loaded", len(newRules))
}

// sliceEqual compares two string slices for equality.
// Sourced from convention.
func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (d *Daemon) shutdown() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, t := range d.triggers {
		t.Stop()
	}

	// FR-5: Close state database
	if d.stateDB != nil {
		d.stateDB.Close()
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

	// Set daemon path for memory MCP injection
	if d.config.Memory.Enabled {
		if daemonPath, err := os.Executable(); err == nil {
			d.daemonPath = daemonPath
		}
	}

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

// FR-12: expandHomeForUser resolves ~ using the specified user's home directory.
// Falls back to os.UserHomeDir() if username is empty or lookup fails.
func expandHomeForUser(path, username string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}

	if username != "" {
		if u, err := user.Lookup(username); err == nil {
			return filepath.Join(u.HomeDir, path[2:])
		}
	}

	// Fallback to current user's home
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, path[2:])
	}
	return path
}

func expandHome(path string) string {
	return expandHomeForUser(path, "")
}
