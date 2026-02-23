// internal/config/types.go
package config

// Global configuration loaded from config.yaml
type Global struct {
	Daemon         DaemonConfig   `yaml:"daemon"`
	ClaudeDefaults ClaudeConfig   `yaml:"claude_defaults"`
	Logging        LoggingConfig  `yaml:"logging"`
	RuleExecution  RuleExecConfig `yaml:"rule_execution"`
	Memory         MemoryConfig   `yaml:"memory"`
}

type DaemonConfig struct {
	LogLevel             string   `yaml:"log_level"`
	WebhookListenPort    int      `yaml:"webhook_listen_port"`
	WebhookListenAddress string   `yaml:"webhook_listen_address"`
	AllowedRunAsUsers    []string `yaml:"allowed_run_as_users"` // FR-15: allowlist for run_as_user
}

type ClaudeConfig struct {
	Model              string            `yaml:"model"`
	AllowedTools       []string          `yaml:"allowed_tools"`
	DisallowedTools    []string          `yaml:"disallowed_tools"`
	AddDirs            []string          `yaml:"add_dirs"`
	PermissionMode     string            `yaml:"permission_mode"`
	MaxBudgetUSD       float64           `yaml:"max_budget_usd"`
	SystemPrompt       string            `yaml:"system_prompt"`
	AppendSystemPrompt string            `yaml:"append_system_prompt"`
	MCPConfig          []string          `yaml:"mcp_config"`
	Memory             *bool             `yaml:"memory"`   // nil = inherit, true = enable, false = disable
	EnvVars            map[string]string `yaml:"env_vars"` // FR-18: environment variables for subprocess
}

type LoggingConfig struct {
	Format string `yaml:"format"`
	Debug  bool   `yaml:"debug"`
}

type RuleExecConfig struct {
	MaxConcurrent int `yaml:"max_concurrent"`
}

type MemoryConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

// Rule configuration loaded from individual YAML files
type Rule struct {
	Name              string       `yaml:"name"`
	Description       string       `yaml:"description"`
	Enabled           bool         `yaml:"enabled"`
	RunAsUser         string       `yaml:"run_as_user"`
	Trigger           Trigger      `yaml:"trigger"`
	Action            Action       `yaml:"action"`
	Claude            ClaudeConfig `yaml:"claude"`
	DryRun            bool         `yaml:"dry_run"`
	DependsOn         []string     `yaml:"depends_on_rules"`
	Triggers          []string     `yaml:"triggers_rules"`
	OnFailure         OnFailure    `yaml:"on_failure"`
	MaxTimeoutSeconds int          `yaml:"max_timeout_seconds"` // FR-3: per-rule timeout (default 300)
	MaxActions        int          `yaml:"max_actions"`         // FR-17: max tool calls per execution (default 50)
}

type Trigger struct {
	Type string `yaml:"type"`
	// Filesystem
	WatchPaths      []string `yaml:"watch_paths"`
	OnEvents        []string `yaml:"on_events"`
	IgnorePatterns  []string `yaml:"ignore_patterns"`
	DebounceSeconds int      `yaml:"debounce_seconds"`
	Recursive       bool     `yaml:"recursive"`
	// Scheduled
	CronExpression string `yaml:"cron_expression"`
	RunEvery       string `yaml:"run_every"`
	RunAt          string `yaml:"run_at"`
	// Webhook
	ListenPath     string   `yaml:"listen_path"`
	AllowedMethods []string `yaml:"allowed_methods"`
	RequireSecret  bool     `yaml:"require_secret"`
	SecretHeader   string   `yaml:"secret_header"`
	SecretEnvVar   string   `yaml:"secret_env_var"`
	// Lifecycle
	// (uses OnEvents)
}

type Action struct {
	Prompt string `yaml:"prompt"`
}

type OnFailure struct {
	Retry             bool `yaml:"retry"`
	RetryAttempts     int  `yaml:"retry_attempts"`
	RetryDelaySeconds int  `yaml:"retry_delay_seconds"`
}
