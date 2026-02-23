// cmd/srvrmgr/main.go
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
	"github.com/colebrumley/srvrmgr/internal/daemon"
	"gopkg.in/yaml.v3"
)

const (
	defaultConfigDir = "/Library/Application Support/srvrmgr"
	defaultLogsDir   = "/Library/Logs/srvrmgr"
	launchdLabel     = "com.srvrmgr.daemon"
	launchdPlist     = "/Library/LaunchDaemons/com.srvrmgr.daemon.plist"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "init":
		err = cmdInit()
	case "start":
		err = cmdStart()
	case "stop":
		err = cmdStop()
	case "restart":
		err = cmdRestart()
	case "status":
		err = cmdStatus()
	case "list":
		err = cmdList()
	case "validate":
		err = cmdValidate(args)
	case "run":
		err = cmdRun(args)
	case "logs":
		err = cmdLogs(args)
	case "history":
		err = cmdHistory(args)
	case "uninstall":
		err = cmdUninstall(args)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`srvrmgr - Server management daemon using Claude Code

Usage: srvrmgr <command> [options]

Commands:
  init              Initialize configuration directories
  start             Start the daemon
  stop              Stop the daemon
  restart           Restart the daemon
  status            Show daemon status
  list              List all rules
  validate [rule]   Validate rules
  run <rule>        Manually run a rule
  logs [rule]       View logs
  history [rule]    View execution history
  uninstall         Uninstall srvrmgr (stop daemon, remove plist)`)
}

// --- Helpers ---

func loadConfig() *config.Global {
	cfg, err := config.LoadGlobal(filepath.Join(defaultConfigDir, "config.yaml"))
	if err != nil {
		return &config.Global{
			Daemon: config.DaemonConfig{
				WebhookListenPort:    9876,
				WebhookListenAddress: "127.0.0.1",
			},
		}
	}
	return cfg
}

func queryDaemon(path string) ([]byte, error) {
	cfg := loadConfig()
	url := fmt.Sprintf("http://%s:%d%s", cfg.Daemon.WebhookListenAddress, cfg.Daemon.WebhookListenPort, path)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func printTable(headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	fmt.Fprintln(tw, strings.Repeat("─", 60))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	tw.Flush()
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func triggerDetail(t config.Trigger) string {
	var detail string
	switch t.Type {
	case "scheduled":
		if t.RunEvery != "" {
			detail = "every " + t.RunEvery
		} else if t.CronExpression != "" {
			detail = t.CronExpression
		} else if t.RunAt != "" {
			detail = "at " + t.RunAt
		}
	case "filesystem":
		if len(t.WatchPaths) > 0 {
			detail = t.WatchPaths[0]
			if len(t.WatchPaths) > 1 {
				detail += fmt.Sprintf(" (+%d more)", len(t.WatchPaths)-1)
			}
		}
	case "webhook":
		detail = t.ListenPath
	case "lifecycle":
		detail = strings.Join(t.OnEvents, ", ")
	case "manual":
		detail = "-"
	}
	return truncate(detail, 30)
}

func formatDuration(ms int64) string {
	return time.Duration(ms * int64(time.Millisecond)).Truncate(100 * time.Millisecond).String()
}

func rulesDir() (string, error) {
	dir := filepath.Join(defaultConfigDir, "rules")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", fmt.Errorf("rules directory not found: %s (run 'srvrmgr init' first)", dir)
	}
	return dir, nil
}

func boolYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// --- Commands ---

func cmdInit() error {
	rulesDir := filepath.Join(defaultConfigDir, "rules")
	dirs := []string{
		defaultConfigDir,
		rulesDir,
		defaultLogsDir,
		filepath.Join(defaultLogsDir, "rules"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
		fmt.Printf("Created %s\n", dir)
	}

	// FR-14: Set secure permissions on rules directory.
	// Sourced from convention — 0700 is more restrictive than architect's 0750.
	if err := os.Chmod(rulesDir, 0700); err != nil {
		return fmt.Errorf("setting rules directory permissions: %w", err)
	}

	// Create default config if it doesn't exist
	configPath := filepath.Join(defaultConfigDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := config.Global{
			Daemon: config.DaemonConfig{
				LogLevel:             "info",
				WebhookListenPort:    9876,
				WebhookListenAddress: "127.0.0.1",
			},
			ClaudeDefaults: config.ClaudeConfig{
				Model:          "sonnet",
				PermissionMode: "default",
			},
			Logging: config.LoggingConfig{
				Format: "json",
				Debug:  false,
			},
			RuleExecution: config.RuleExecConfig{
				MaxConcurrent: 10,
			},
		}

		data, err := yaml.Marshal(defaultConfig)
		if err != nil {
			return err
		}

		if err := os.WriteFile(configPath, data, 0644); err != nil {
			return err
		}
		fmt.Printf("Created %s\n", configPath)
	}

	fmt.Println("\nInitialization complete. Add rules to:", filepath.Join(defaultConfigDir, "rules"))
	return nil
}

func cmdStart() error {
	// Check if already running
	if isRunning() {
		fmt.Println("Daemon is already running")
		return nil
	}

	// Load the daemon via launchctl
	cmd := exec.Command("sudo", "launchctl", "load", launchdPlist)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	fmt.Println("Daemon started")
	return nil
}

func cmdStop() error {
	if !isRunning() {
		fmt.Println("Daemon is not running")
		return nil
	}

	cmd := exec.Command("sudo", "launchctl", "unload", launchdPlist)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}

	fmt.Println("Daemon stopped")
	return nil
}

func cmdRestart() error {
	if isRunning() {
		if err := cmdStop(); err != nil {
			return err
		}
	}
	return cmdStart()
}

func cmdStatus() error {
	if isRunning() {
		body, err := queryDaemon("/health")
		if err != nil {
			fmt.Println("Daemon:  running (API unreachable)")
			dir, dirErr := rulesDir()
			if dirErr == nil {
				rules, loadErr := config.LoadRulesDir(dir)
				if loadErr == nil {
					fmt.Printf("Rules:   %d rules on disk\n", len(rules))
				}
			}
			return nil
		}

		var health struct {
			Status       string `json:"status"`
			Uptime       string `json:"uptime"`
			RulesLoaded  int    `json:"rules_loaded"`
			RulesEnabled int    `json:"rules_enabled"`
		}
		if err := json.Unmarshal(body, &health); err != nil {
			return fmt.Errorf("parsing health response: %w", err)
		}

		fmt.Println("Daemon:  running")
		fmt.Printf("Uptime:  %s\n", health.Uptime)
		fmt.Printf("Rules:   %d loaded, %d enabled\n", health.RulesLoaded, health.RulesEnabled)

		body, err = queryDaemon("/api/rules")
		if err == nil {
			var ruleStates []struct {
				Name      string `json:"name"`
				Enabled   bool   `json:"enabled"`
				DryRun    bool   `json:"dry_run"`
				LastState string `json:"last_state"`
			}
			if json.Unmarshal(body, &ruleStates) == nil && len(ruleStates) > 0 {
				fmt.Println()
				var rows [][]string
				for _, r := range ruleStates {
					dryRun := boolYesNo(r.DryRun)
					if !r.Enabled {
						dryRun = "-"
					}
					lastState := r.LastState
					if lastState == "" {
						lastState = "-"
					}
					rows = append(rows, []string{r.Name, boolYesNo(r.Enabled), dryRun, lastState})
				}
				printTable([]string{"NAME", "ENABLED", "DRY RUN", "LAST STATE"}, rows)
			}
		}
	} else {
		fmt.Println("Daemon:  not running")
		dir, dirErr := rulesDir()
		if dirErr == nil {
			rules, loadErr := config.LoadRulesDir(dir)
			if loadErr == nil {
				fmt.Printf("Rules:   %d rules on disk\n", len(rules))
			}
		}
	}
	return nil
}

func isRunning() bool {
	cmd := exec.Command("launchctl", "list", launchdLabel)
	return cmd.Run() == nil
}

func cmdList() error {
	dir, err := rulesDir()
	if err != nil {
		return err
	}
	rules, err := config.LoadRulesDir(dir)
	if err != nil {
		return err
	}

	if len(rules) == 0 {
		fmt.Println("No rules found")
		return nil
	}

	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Name < rules[j].Name
	})

	var rows [][]string
	for _, rule := range rules {
		timeout := fmt.Sprintf("%ds", rule.MaxTimeoutSeconds)
		if rule.MaxTimeoutSeconds == 0 {
			timeout = "300s"
		}
		rows = append(rows, []string{
			truncate(rule.Name, 30),
			boolYesNo(rule.Enabled),
			rule.Trigger.Type,
			triggerDetail(rule.Trigger),
			boolYesNo(rule.DryRun),
			timeout,
			truncate(rule.Description, 30),
		})
	}

	printTable([]string{"NAME", "ENABLED", "TRIGGER", "DETAIL", "DRY RUN", "TIMEOUT", "DESCRIPTION"}, rows)
	return nil
}

func cmdValidate(args []string) error {
	dir, err := rulesDir()
	if err != nil {
		return err
	}

	if len(args) > 0 {
		return cmdValidateOne(dir, args[0])
	}
	return cmdValidateAll(dir)
}

func cmdValidateOne(dir, name string) error {
	// Try .yaml then .yml
	rulePath := filepath.Join(dir, name+".yaml")
	if _, err := os.Stat(rulePath); os.IsNotExist(err) {
		rulePath = filepath.Join(dir, name+".yml")
		if _, err := os.Stat(rulePath); os.IsNotExist(err) {
			return fmt.Errorf("rule file not found: %s.yaml or %s.yml", name, name)
		}
	}

	rule, err := config.LoadRule(rulePath)
	if err != nil {
		fmt.Printf("Rule '%s' is INVALID: %v\n", name, err)
		return err
	}

	fmt.Printf("Rule '%s' is valid\n", name)

	model := rule.Claude.Model
	if model == "" {
		model = "sonnet"
	}
	timeout := rule.MaxTimeoutSeconds
	if timeout == 0 {
		timeout = 300
	}
	maxActions := rule.MaxActions
	if maxActions == 0 {
		maxActions = 50
	}
	dependsOn := "-"
	if len(rule.DependsOn) > 0 {
		dependsOn = strings.Join(rule.DependsOn, ", ")
	}
	triggers := "-"
	if len(rule.Triggers) > 0 {
		triggers = strings.Join(rule.Triggers, ", ")
	}
	retry := "-"
	if rule.OnFailure.Retry {
		attempts := rule.OnFailure.RetryAttempts
		if attempts == 0 {
			attempts = 3
		}
		delay := rule.OnFailure.RetryDelaySeconds
		retry = fmt.Sprintf("%d attempts, %ds delay", attempts, delay)
	}

	fmt.Println()
	fmt.Printf("  Trigger:      %s (%s)\n", rule.Trigger.Type, triggerDetail(rule.Trigger))
	fmt.Printf("  Model:        %s\n", model)
	fmt.Printf("  Dry run:      %s\n", boolYesNo(rule.DryRun))
	fmt.Printf("  Timeout:      %ds\n", timeout)
	fmt.Printf("  Max actions:  %d\n", maxActions)
	fmt.Printf("  Depends on:   %s\n", dependsOn)
	fmt.Printf("  Triggers:     %s\n", triggers)
	fmt.Printf("  Retry:        %s\n", retry)

	// Run global validation for warnings
	global := loadConfig()
	allRulesSlice, _ := config.LoadRulesDir(dir)
	allRules := make(map[string]*config.Rule)
	for _, r := range allRulesSlice {
		allRules[r.Name] = r
	}
	warnings := config.ValidateRuleWithGlobal(rule, global, allRules)
	if len(warnings) > 0 {
		fmt.Println()
		for _, w := range warnings {
			fmt.Printf("  Warning: %s\n", w)
		}
	}

	return nil
}

func cmdValidateAll(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading rules directory: %w", err)
	}

	global := loadConfig()

	// Load all valid rules for global validation context
	allRulesSlice, _ := config.LoadRulesDir(dir)
	allRules := make(map[string]*config.Rule)
	for _, r := range allRulesSlice {
		allRules[r.Name] = r
	}

	var rows [][]string
	var valid, invalid int

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		rulePath := filepath.Join(dir, entry.Name())
		rule, err := config.LoadRule(rulePath)
		if err != nil {
			invalid++
			rows = append(rows, []string{
				strings.TrimSuffix(entry.Name(), ext),
				"FAIL",
				truncate(err.Error(), 50),
			})
			continue
		}

		valid++
		warnings := config.ValidateRuleWithGlobal(rule, global, allRules)
		warnText := "-"
		if len(warnings) > 0 {
			warnText = truncate(strings.Join(warnings, "; "), 50)
		}
		rows = append(rows, []string{rule.Name, "ok", warnText})
	}

	total := valid + invalid
	printTable([]string{"RULE", "STATUS", "WARNINGS"}, rows)
	fmt.Printf("\n%d valid, %d invalid (total %d)\n", valid, invalid, total)

	if invalid > 0 {
		return fmt.Errorf("%d of %d rules are invalid", invalid, total)
	}
	return nil
}

func cmdHistory(args []string) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	limit := fs.Int("limit", 50, "max records to return")
	state := fs.String("state", "", "filter by state (success, failure, timeout, cancelled)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *state != "" {
		validStates := map[string]bool{"success": true, "failure": true, "timeout": true, "cancelled": true}
		if !validStates[*state] {
			return fmt.Errorf("invalid state %q: must be one of success, failure, timeout, cancelled", *state)
		}
	}

	if !isRunning() {
		return fmt.Errorf("daemon is not running")
	}

	query := fmt.Sprintf("/api/history?limit=%d", *limit)
	if ruleName := fs.Arg(0); ruleName != "" {
		query += "&rule=" + ruleName
	}
	if *state != "" {
		query += "&state=" + *state
	}

	body, err := queryDaemon(query)
	if err != nil {
		return fmt.Errorf("querying daemon: %w", err)
	}

	var records []struct {
		RuleName    string `json:"RuleName"`
		TriggerType string `json:"TriggerType"`
		State       string `json:"State"`
		StartedAt   string `json:"StartedAt"`
		DurationMs  int64  `json:"DurationMs"`
		Error       string `json:"Error"`
	}
	if err := json.Unmarshal(body, &records); err != nil {
		return fmt.Errorf("parsing history response: %w", err)
	}

	if len(records) == 0 {
		fmt.Println("No execution history found")
		return nil
	}

	var rows [][]string
	for _, rec := range records {
		started := rec.StartedAt
		if t, err := time.Parse(time.RFC3339, rec.StartedAt); err == nil {
			started = t.Format("2006-01-02 15:04")
		}
		errMsg := "-"
		if rec.Error != "" {
			errMsg = truncate(rec.Error, 40)
		}
		rows = append(rows, []string{
			rec.RuleName,
			rec.TriggerType,
			rec.State,
			started,
			formatDuration(rec.DurationMs),
			errMsg,
		})
	}

	printTable([]string{"RULE", "TRIGGER", "STATE", "STARTED", "DURATION", "ERROR"}, rows)
	return nil
}

func cmdRun(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: srvrmgr run <rule-name>")
	}

	ruleName := args[0]
	configPath := filepath.Join(defaultConfigDir, "config.yaml")
	rulesDir := filepath.Join(defaultConfigDir, "rules")

	d := daemon.New(configPath, rulesDir)

	ctx := context.Background()
	return d.RunRule(ctx, ruleName, map[string]any{})
}

func cmdLogs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	follow := fs.Bool("f", false, "follow logs")
	// FR-10: --follow alias for -f.
	// Sourced from convention.
	fs.BoolVar(follow, "follow", false, "follow logs")
	fs.Parse(args)

	var logPath string
	if fs.NArg() > 0 {
		// Specific rule logs
		logPath = filepath.Join(defaultLogsDir, "rules", fs.Arg(0)+".log")
	} else {
		// Daemon logs
		logPath = filepath.Join(defaultLogsDir, "srvrmgrd.log")
	}

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return fmt.Errorf("log file not found: %s", logPath)
	}

	tailArgs := []string{"-n", "50"}
	if *follow {
		tailArgs = append(tailArgs, "-f")
	}
	tailArgs = append(tailArgs, logPath)

	cmd := exec.Command("tail", tailArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	keepConfig := fs.Bool("keep-config", false, "keep config and rules")
	removeConfig := fs.Bool("remove-config", false, "remove config and rules without prompting")
	fs.Parse(args)

	// Validate flags
	if *keepConfig && *removeConfig {
		return fmt.Errorf("cannot specify both --keep-config and --remove-config")
	}

	// Check for root
	if os.Geteuid() != 0 {
		return fmt.Errorf("uninstall must be run as root (use sudo)")
	}

	// Stop daemon if running
	if isRunning() {
		fmt.Println("Stopping daemon...")
		cmd := exec.Command("launchctl", "unload", launchdPlist)
		cmd.Run() // Ignore error, plist might not be loaded
	}

	// Remove plist
	if _, err := os.Stat(launchdPlist); err == nil {
		if err := os.Remove(launchdPlist); err != nil {
			return fmt.Errorf("removing plist: %w", err)
		}
		fmt.Println("Removed", launchdPlist)
	}

	// Handle config removal
	if !*keepConfig {
		removeIt := *removeConfig
		if !removeIt {
			fmt.Print("Remove config and rules? (y/N): ")
			var response string
			fmt.Scanln(&response)
			removeIt = strings.ToLower(response) == "y"
		}

		if removeIt {
			if err := os.RemoveAll(defaultConfigDir); err != nil {
				return fmt.Errorf("removing config dir: %w", err)
			}
			fmt.Println("Removed", defaultConfigDir)

			if err := os.RemoveAll(defaultLogsDir); err != nil {
				return fmt.Errorf("removing logs dir: %w", err)
			}
			fmt.Println("Removed", defaultLogsDir)
		}
	}

	fmt.Println("\nUninstall complete. Run 'brew uninstall srvrmgr' to remove binaries.")
	return nil
}
