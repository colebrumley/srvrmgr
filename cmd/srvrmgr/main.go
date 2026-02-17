// cmd/srvrmgr/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
  uninstall         Uninstall srvrmgr (stop daemon, remove plist)`)
}

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
		fmt.Println("Daemon is running")
	} else {
		fmt.Println("Daemon is not running")
	}
	return nil
}

func isRunning() bool {
	cmd := exec.Command("launchctl", "list", launchdLabel)
	return cmd.Run() == nil
}

func cmdList() error {
	rulesDir := filepath.Join(defaultConfigDir, "rules")
	rules, err := config.LoadRulesDir(rulesDir)
	if err != nil {
		return err
	}

	if len(rules) == 0 {
		fmt.Println("No rules found")
		return nil
	}

	fmt.Printf("%-20s %-10s %-15s %s\n", "NAME", "ENABLED", "TRIGGER", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 70))

	for _, rule := range rules {
		enabled := "yes"
		if !rule.Enabled {
			enabled = "no"
		}
		desc := rule.Description
		if len(desc) > 30 {
			desc = desc[:27] + "..."
		}
		fmt.Printf("%-20s %-10s %-15s %s\n", rule.Name, enabled, rule.Trigger.Type, desc)
	}

	return nil
}

func cmdValidate(args []string) error {
	rulesDir := filepath.Join(defaultConfigDir, "rules")

	if len(args) > 0 {
		// Validate specific rule
		rulePath := filepath.Join(rulesDir, args[0]+".yaml")
		if _, err := config.LoadRule(rulePath); err != nil {
			return fmt.Errorf("invalid rule %s: %w", args[0], err)
		}
		fmt.Printf("Rule '%s' is valid\n", args[0])
		return nil
	}

	// Validate all rules
	rules, err := config.LoadRulesDir(rulesDir)
	if err != nil {
		return err
	}

	fmt.Printf("Validated %d rules\n", len(rules))
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
