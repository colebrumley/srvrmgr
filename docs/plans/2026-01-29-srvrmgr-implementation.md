# srvrmgr Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a macOS server management daemon that uses Claude agents to execute event-driven automation rules.

**Architecture:** TypeScript daemon with launchd integration. Rules are YAML configs with triggers, prompts, and sandbox constraints. Agents execute via Claude SDK with custom sandboxed tools.

**Tech Stack:** TypeScript, Node.js, Claude Agent SDK, Zod, chokidar, node-cron, Commander.js

---

## Phase 1: Project Setup & Types

### Task 1.1: Initialize Node.js Project

**Files:**
- Create: `package.json`
- Create: `tsconfig.json`
- Create: `.gitignore`

**Step 1: Create package.json**

```json
{
  "name": "srvrmgr",
  "version": "0.1.0",
  "description": "macOS server management daemon using Claude agents",
  "type": "module",
  "engines": {
    "node": ">=20.0.0"
  },
  "scripts": {
    "build": "tsc",
    "dev": "tsc --watch",
    "test": "vitest",
    "test:run": "vitest run",
    "lint": "eslint src/",
    "daemon": "node dist/daemon/index.js",
    "cli": "node dist/cli/index.js"
  },
  "bin": {
    "srvrmgr": "./bin/srvrmgr",
    "srvrmgrd": "./bin/srvrmgrd"
  },
  "dependencies": {
    "@anthropic-ai/sdk": "^0.52.0",
    "chokidar": "^3.6.0",
    "commander": "^12.1.0",
    "node-cron": "^3.0.3",
    "yaml": "^2.4.5",
    "zod": "^3.23.8"
  },
  "devDependencies": {
    "@types/node": "^20.14.0",
    "@types/node-cron": "^3.0.11",
    "typescript": "^5.5.0",
    "vitest": "^1.6.0"
  }
}
```

**Step 2: Create tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "lib": ["ES2022"],
    "outDir": "./dist",
    "rootDir": "./src",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "forceConsistentCasingInFileNames": true,
    "declaration": true,
    "declarationMap": true,
    "sourceMap": true,
    "resolveJsonModule": true
  },
  "include": ["src/**/*"],
  "exclude": ["node_modules", "dist"]
}
```

**Step 3: Create .gitignore**

```
node_modules/
dist/
*.log
.DS_Store
```

**Step 4: Install dependencies**

Run: `npm install`

**Step 5: Commit**

```bash
git add package.json tsconfig.json .gitignore
git commit -m "chore: initialize project with TypeScript and dependencies"
```

---

### Task 1.2: Define Config Types with Zod

**Files:**
- Create: `src/types/config.ts`

**Step 1: Write config schema**

```typescript
import { z } from 'zod';

export const DaemonConfigSchema = z.object({
  log_level: z.enum(['debug', 'info', 'warn', 'error']).default('info'),
  webhook_listen_port: z.number().int().min(1).max(65535).default(9876),
  webhook_listen_address: z.string().default('127.0.0.1'),
});

export const AgentConfigSchema = z.object({
  api_key_source: z.enum(['environment', 'file']).default('environment'),
  api_key_file: z.string().optional(),
});

export const AgentDefaultsSchema = z.object({
  agent_mode: z.enum(['isolated', 'persistent']).default('isolated'),
  model: z.string().default('claude-sonnet-4-20250514'),
  max_timeout_seconds: z.number().int().positive().default(300),
  max_memory_mb: z.number().int().positive().default(1024),
  allow_network: z.boolean().default(false),
});

export const SandboxDefaultsSchema = z.object({
  allowed_commands: z.array(z.string()).default(['ls', 'cat', 'head', 'tail', 'file', 'mkdir', 'cp', 'mv', 'rm']),
});

export const LoggingConfigSchema = z.object({
  rotate_on_size_mb: z.number().int().positive().default(50),
  rotate_on_days: z.number().int().positive().default(7),
  keep_rotated_files: z.number().int().nonnegative().default(5),
  format: z.enum(['json', 'text']).default('json'),
});

export const PersistentAgentsConfigSchema = z.object({
  max_idle_seconds: z.number().int().positive().default(3600),
  max_concurrent: z.number().int().positive().default(5),
});

export const RuleExecutionConfigSchema = z.object({
  max_concurrent_rules: z.number().int().positive().default(10),
  retry_on_failure: z.boolean().default(false),
  retry_attempts: z.number().int().positive().default(3),
  retry_delay_seconds: z.number().int().positive().default(30),
});

export const GlobalConfigSchema = z.object({
  daemon: DaemonConfigSchema.default({}),
  agent: AgentConfigSchema.default({}),
  agent_defaults: AgentDefaultsSchema.default({}),
  sandbox_defaults: SandboxDefaultsSchema.default({}),
  logging: LoggingConfigSchema.default({}),
  persistent_agents: PersistentAgentsConfigSchema.default({}),
  rule_execution: RuleExecutionConfigSchema.default({}),
});

export type DaemonConfig = z.infer<typeof DaemonConfigSchema>;
export type AgentConfig = z.infer<typeof AgentConfigSchema>;
export type AgentDefaults = z.infer<typeof AgentDefaultsSchema>;
export type SandboxDefaults = z.infer<typeof SandboxDefaultsSchema>;
export type LoggingConfig = z.infer<typeof LoggingConfigSchema>;
export type PersistentAgentsConfig = z.infer<typeof PersistentAgentsConfigSchema>;
export type RuleExecutionConfig = z.infer<typeof RuleExecutionConfigSchema>;
export type GlobalConfig = z.infer<typeof GlobalConfigSchema>;
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/types/config.ts
git commit -m "feat: add global config Zod schemas"
```

---

### Task 1.3: Define Rule Types with Zod

**Files:**
- Create: `src/types/rule.ts`

**Step 1: Write rule schema**

```typescript
import { z } from 'zod';

// Trigger schemas
const FilesystemTriggerSchema = z.object({
  type: z.literal('filesystem'),
  watch_paths: z.array(z.string()).min(1),
  on_events: z.array(z.enum(['file_created', 'file_modified', 'file_deleted', 'directory_created'])).min(1),
  ignore_patterns: z.array(z.string()).default([]),
  debounce_seconds: z.number().int().nonnegative().default(0),
});

const ScheduledTriggerSchema = z.object({
  type: z.literal('scheduled'),
  cron_expression: z.string().optional(),
  run_every: z.string().optional(),
  run_at: z.string().optional(),
}).refine(
  (data) => data.cron_expression || data.run_every || data.run_at,
  { message: 'Must specify cron_expression, run_every, or run_at' }
);

const SystemTriggerSchema = z.object({
  type: z.literal('system'),
  on_events: z.array(z.enum(['cpu_above', 'memory_above', 'disk_above', 'process_started', 'process_stopped'])).min(1),
  cpu_threshold_percent: z.number().int().min(0).max(100).optional(),
  memory_threshold_percent: z.number().int().min(0).max(100).optional(),
  disk_threshold_percent: z.number().int().min(0).max(100).optional(),
  watch_processes: z.array(z.string()).optional(),
  sustained_seconds: z.number().int().positive().default(30),
});

const WebhookTriggerSchema = z.object({
  type: z.literal('webhook'),
  listen_path: z.string().startsWith('/'),
  allowed_methods: z.array(z.enum(['GET', 'POST', 'PUT', 'DELETE'])).default(['POST']),
  require_secret: z.boolean().default(false),
  secret_env_var: z.string().optional(),
});

const LifecycleTriggerSchema = z.object({
  type: z.literal('lifecycle'),
  on_events: z.array(z.enum(['daemon_started', 'daemon_stopped'])).min(1),
});

const ManualTriggerSchema = z.object({
  type: z.literal('manual'),
});

export const TriggerSchema = z.discriminatedUnion('type', [
  FilesystemTriggerSchema,
  ScheduledTriggerSchema,
  SystemTriggerSchema,
  WebhookTriggerSchema,
  LifecycleTriggerSchema,
  ManualTriggerSchema,
]);

// Action schema
export const ActionSchema = z.object({
  prompt: z.string().min(1),
  include_file_metadata: z.boolean().default(false),
});

// Sandbox schema
export const SandboxSchema = z.object({
  allowed_read_paths: z.array(z.string()).default([]),
  allowed_write_paths: z.array(z.string()).default([]),
  allowed_commands: z.array(z.string()).optional(),
  allow_network: z.boolean().optional(),
  allowed_domains: z.array(z.string()).optional(),
  max_timeout_seconds: z.number().int().positive().optional(),
  max_memory_mb: z.number().int().positive().optional(),
});

// On failure schema
export const OnFailureSchema = z.object({
  retry: z.boolean().default(false),
  retry_attempts: z.number().int().positive().default(3),
  retry_delay_seconds: z.number().int().positive().default(30),
});

// Conditional trigger rule
const ConditionalTriggerRuleSchema = z.object({
  rule: z.string(),
  on_status: z.enum(['success', 'failure', 'any']),
});

const TriggerRuleSchema = z.union([z.string(), ConditionalTriggerRuleSchema]);

// Full rule schema
export const RuleSchema = z.object({
  name: z.string().min(1).regex(/^[a-z0-9-]+$/, 'Name must be lowercase alphanumeric with hyphens'),
  description: z.string().default(''),
  enabled: z.boolean().default(true),
  agent_mode: z.enum(['isolated', 'persistent']).optional(),
  run_as_user: z.string().optional(),
  trigger: TriggerSchema,
  action: ActionSchema,
  sandbox: SandboxSchema.default({}),
  on_failure: OnFailureSchema.default({}),
  depends_on_rules: z.array(z.string()).default([]),
  triggers_rules: z.array(TriggerRuleSchema).default([]),
});

export type Trigger = z.infer<typeof TriggerSchema>;
export type Action = z.infer<typeof ActionSchema>;
export type Sandbox = z.infer<typeof SandboxSchema>;
export type OnFailure = z.infer<typeof OnFailureSchema>;
export type Rule = z.infer<typeof RuleSchema>;

// Execution state
export const ExecutionState = z.enum([
  'triggered',
  'waiting_dependencies',
  'running',
  'success',
  'failure',
  'timeout',
  'skipped_dependency_failed',
  'skipped_disabled',
  'interrupted',
]);

export type ExecutionState = z.infer<typeof ExecutionState>;
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/types/rule.ts
git commit -m "feat: add rule config Zod schemas"
```

---

### Task 1.4: Create Types Index

**Files:**
- Create: `src/types/index.ts`

**Step 1: Create barrel export**

```typescript
export * from './config.js';
export * from './rule.js';
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/types/index.ts
git commit -m "feat: add types barrel export"
```

---

## Phase 2: Logging

### Task 2.1: Create Structured Logger

**Files:**
- Create: `src/logging/logger.ts`

**Step 1: Write logger implementation**

```typescript
import * as fs from 'node:fs';
import * as path from 'node:path';
import type { LoggingConfig } from '../types/index.js';

export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

interface LogEntry {
  timestamp: string;
  level: LogLevel;
  message: string;
  context?: Record<string, unknown>;
}

const LOG_LEVELS: Record<LogLevel, number> = {
  debug: 0,
  info: 1,
  warn: 2,
  error: 3,
};

export class Logger {
  private logPath: string;
  private config: LoggingConfig;
  private minLevel: number;
  private stream: fs.WriteStream | null = null;

  constructor(logPath: string, config: LoggingConfig, level: LogLevel = 'info') {
    this.logPath = logPath;
    this.config = config;
    this.minLevel = LOG_LEVELS[level];
    this.ensureLogDir();
  }

  private ensureLogDir(): void {
    const dir = path.dirname(this.logPath);
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
  }

  private getStream(): fs.WriteStream {
    if (!this.stream) {
      this.stream = fs.createWriteStream(this.logPath, { flags: 'a' });
    }
    return this.stream;
  }

  private formatEntry(entry: LogEntry): string {
    if (this.config.format === 'json') {
      return JSON.stringify(entry);
    }
    const ctx = entry.context ? ` ${JSON.stringify(entry.context)}` : '';
    return `[${entry.timestamp}] ${entry.level.toUpperCase()}: ${entry.message}${ctx}`;
  }

  private write(level: LogLevel, message: string, context?: Record<string, unknown>): void {
    if (LOG_LEVELS[level] < this.minLevel) {
      return;
    }

    const entry: LogEntry = {
      timestamp: new Date().toISOString(),
      level,
      message,
      context,
    };

    const line = this.formatEntry(entry) + '\n';
    this.getStream().write(line);

    // Also log to stderr for daemon visibility
    if (level === 'error' || level === 'warn') {
      process.stderr.write(line);
    }
  }

  debug(message: string, context?: Record<string, unknown>): void {
    this.write('debug', message, context);
  }

  info(message: string, context?: Record<string, unknown>): void {
    this.write('info', message, context);
  }

  warn(message: string, context?: Record<string, unknown>): void {
    this.write('warn', message, context);
  }

  error(message: string, context?: Record<string, unknown>): void {
    this.write('error', message, context);
  }

  close(): void {
    if (this.stream) {
      this.stream.end();
      this.stream = null;
    }
  }
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/logging/logger.ts
git commit -m "feat: add structured logger"
```

---

### Task 2.2: Add Log Rotation

**Files:**
- Create: `src/logging/rotation.ts`

**Step 1: Write rotation implementation**

```typescript
import * as fs from 'node:fs';
import * as path from 'node:path';
import type { LoggingConfig } from '../types/index.js';

export class LogRotator {
  private logPath: string;
  private config: LoggingConfig;

  constructor(logPath: string, config: LoggingConfig) {
    this.logPath = logPath;
    this.config = config;
  }

  shouldRotate(): { rotate: boolean; reason?: string } {
    if (!fs.existsSync(this.logPath)) {
      return { rotate: false };
    }

    const stats = fs.statSync(this.logPath);
    const sizeMb = stats.size / (1024 * 1024);
    const ageDays = (Date.now() - stats.mtimeMs) / (1000 * 60 * 60 * 24);

    if (sizeMb >= this.config.rotate_on_size_mb) {
      return { rotate: true, reason: `size ${sizeMb.toFixed(2)}MB >= ${this.config.rotate_on_size_mb}MB` };
    }

    if (ageDays >= this.config.rotate_on_days) {
      return { rotate: true, reason: `age ${ageDays.toFixed(1)} days >= ${this.config.rotate_on_days} days` };
    }

    return { rotate: false };
  }

  rotate(): string | null {
    if (!fs.existsSync(this.logPath)) {
      return null;
    }

    const dir = path.dirname(this.logPath);
    const ext = path.extname(this.logPath);
    const base = path.basename(this.logPath, ext);
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    const rotatedName = `${base}-${timestamp}${ext}`;
    const rotatedPath = path.join(dir, rotatedName);

    fs.renameSync(this.logPath, rotatedPath);
    this.cleanupOldLogs();

    return rotatedPath;
  }

  private cleanupOldLogs(): void {
    const dir = path.dirname(this.logPath);
    const ext = path.extname(this.logPath);
    const base = path.basename(this.logPath, ext);
    const pattern = new RegExp(`^${base}-.*${ext}$`);

    const rotatedFiles = fs.readdirSync(dir)
      .filter((f) => pattern.test(f))
      .map((f) => ({ name: f, path: path.join(dir, f), mtime: fs.statSync(path.join(dir, f)).mtimeMs }))
      .sort((a, b) => b.mtime - a.mtime);

    const toDelete = rotatedFiles.slice(this.config.keep_rotated_files);
    for (const file of toDelete) {
      fs.unlinkSync(file.path);
    }
  }
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/logging/rotation.ts
git commit -m "feat: add log rotation"
```

---

### Task 2.3: Create Logging Index

**Files:**
- Create: `src/logging/index.ts`

**Step 1: Create barrel export**

```typescript
export { Logger, type LogLevel } from './logger.js';
export { LogRotator } from './rotation.js';
```

**Step 2: Commit**

```bash
git add src/logging/index.ts
git commit -m "feat: add logging barrel export"
```

---

## Phase 3: Config & Rule Loading

### Task 3.1: Create Config Loader

**Files:**
- Create: `src/daemon/config-loader.ts`

**Step 1: Write config loader**

```typescript
import * as fs from 'node:fs';
import * as path from 'node:path';
import { parse as parseYaml } from 'yaml';
import { GlobalConfigSchema, type GlobalConfig } from '../types/index.js';

export const CONFIG_DIR = '/Library/Application Support/srvrmgr';
export const CONFIG_PATH = path.join(CONFIG_DIR, 'config.yaml');
export const RULES_DIR = path.join(CONFIG_DIR, 'rules');
export const STATE_DIR = path.join(CONFIG_DIR, 'state');
export const SECRETS_DIR = path.join(CONFIG_DIR, 'secrets');
export const LOG_DIR = '/Library/Logs/srvrmgr';

export function loadConfig(): GlobalConfig {
  if (!fs.existsSync(CONFIG_PATH)) {
    return GlobalConfigSchema.parse({});
  }

  const content = fs.readFileSync(CONFIG_PATH, 'utf-8');
  const raw = parseYaml(content);
  return GlobalConfigSchema.parse(raw);
}

export function getApiKey(config: GlobalConfig): string {
  if (config.agent.api_key_source === 'environment') {
    const key = process.env.ANTHROPIC_API_KEY;
    if (!key) {
      throw new Error('ANTHROPIC_API_KEY environment variable not set');
    }
    return key;
  }

  const keyFile = config.agent.api_key_file || path.join(SECRETS_DIR, 'api_key');
  if (!fs.existsSync(keyFile)) {
    throw new Error(`API key file not found: ${keyFile}`);
  }
  return fs.readFileSync(keyFile, 'utf-8').trim();
}

export function ensureDirectories(): void {
  const dirs = [CONFIG_DIR, RULES_DIR, STATE_DIR, SECRETS_DIR, LOG_DIR, path.join(LOG_DIR, 'rules')];
  for (const dir of dirs) {
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
  }
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/daemon/config-loader.ts
git commit -m "feat: add config loader"
```

---

### Task 3.2: Create Rule Loader

**Files:**
- Create: `src/daemon/rule-loader.ts`

**Step 1: Write rule loader**

```typescript
import * as fs from 'node:fs';
import * as path from 'node:path';
import { parse as parseYaml } from 'yaml';
import { RuleSchema, type Rule } from '../types/index.js';
import { RULES_DIR } from './config-loader.js';

export interface LoadedRule {
  rule: Rule;
  filePath: string;
}

export interface LoadResult {
  rules: LoadedRule[];
  errors: Array<{ file: string; error: string }>;
}

export function loadRule(filePath: string): Rule {
  const content = fs.readFileSync(filePath, 'utf-8');
  const raw = parseYaml(content);
  return RuleSchema.parse(raw);
}

export function loadAllRules(): LoadResult {
  const result: LoadResult = { rules: [], errors: [] };

  if (!fs.existsSync(RULES_DIR)) {
    return result;
  }

  const files = fs.readdirSync(RULES_DIR).filter((f) => f.endsWith('.yaml') || f.endsWith('.yml'));

  for (const file of files) {
    const filePath = path.join(RULES_DIR, file);
    try {
      const rule = loadRule(filePath);
      result.rules.push({ rule, filePath });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      result.errors.push({ file, error: message });
    }
  }

  return result;
}

export function validateRuleDependencies(rules: LoadedRule[]): string[] {
  const errors: string[] = [];
  const ruleNames = new Set(rules.map((r) => r.rule.name));

  for (const { rule } of rules) {
    for (const dep of rule.depends_on_rules) {
      if (!ruleNames.has(dep)) {
        errors.push(`Rule '${rule.name}' depends on unknown rule '${dep}'`);
      }
    }

    for (const trigger of rule.triggers_rules) {
      const triggerName = typeof trigger === 'string' ? trigger : trigger.rule;
      if (!ruleNames.has(triggerName)) {
        errors.push(`Rule '${rule.name}' triggers unknown rule '${triggerName}'`);
      }
    }
  }

  // Check for circular dependencies
  const visited = new Set<string>();
  const recursionStack = new Set<string>();

  function hasCycle(name: string): boolean {
    if (recursionStack.has(name)) return true;
    if (visited.has(name)) return false;

    visited.add(name);
    recursionStack.add(name);

    const rule = rules.find((r) => r.rule.name === name)?.rule;
    if (rule) {
      for (const dep of rule.depends_on_rules) {
        if (hasCycle(dep)) return true;
      }
    }

    recursionStack.delete(name);
    return false;
  }

  for (const { rule } of rules) {
    visited.clear();
    recursionStack.clear();
    if (hasCycle(rule.name)) {
      errors.push(`Circular dependency detected involving rule '${rule.name}'`);
    }
  }

  return errors;
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/daemon/rule-loader.ts
git commit -m "feat: add rule loader with dependency validation"
```

---

## Phase 4: Sandbox Tools

### Task 4.1: Create Path Validator

**Files:**
- Create: `src/sandbox/validators.ts`

**Step 1: Write validators**

```typescript
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as os from 'node:os';

export function expandPath(p: string, user?: string): string {
  if (p.startsWith('~/')) {
    const homeDir = user ? `/Users/${user}` : os.homedir();
    return path.join(homeDir, p.slice(2));
  }
  return p;
}

export function normalizePath(p: string): string {
  return path.normalize(path.resolve(p));
}

export function isPathAllowed(targetPath: string, allowedPaths: string[], user?: string): boolean {
  const normalizedTarget = normalizePath(expandPath(targetPath, user));

  // Resolve symlinks if the path exists
  let resolvedTarget = normalizedTarget;
  if (fs.existsSync(normalizedTarget)) {
    try {
      resolvedTarget = fs.realpathSync(normalizedTarget);
    } catch {
      // If we can't resolve, use the normalized path
    }
  }

  for (const allowed of allowedPaths) {
    const normalizedAllowed = normalizePath(expandPath(allowed, user));
    if (resolvedTarget === normalizedAllowed || resolvedTarget.startsWith(normalizedAllowed + path.sep)) {
      return true;
    }
  }

  return false;
}

export function hasPathTraversal(p: string): boolean {
  const normalized = path.normalize(p);
  return normalized.includes('..') || p.includes('\0');
}

export function parseCommand(cmd: string): { binary: string; args: string[] } | null {
  const parts = cmd.trim().split(/\s+/);
  if (parts.length === 0 || !parts[0]) {
    return null;
  }
  return { binary: path.basename(parts[0]), args: parts.slice(1) };
}

export function isCommandAllowed(cmd: string, allowedCommands: string[]): { allowed: boolean; reason?: string } {
  // Block shell operators that could escape sandbox
  const dangerousPatterns = [/`/, /\$\(/, /\$\{/, /;\s*/, /\|\|/, /&&/, /\|/, />/,  /</, /\n/];
  for (const pattern of dangerousPatterns) {
    if (pattern.test(cmd)) {
      return { allowed: false, reason: `Command contains blocked shell operator` };
    }
  }

  const parsed = parseCommand(cmd);
  if (!parsed) {
    return { allowed: false, reason: 'Could not parse command' };
  }

  if (!allowedCommands.includes(parsed.binary)) {
    return { allowed: false, reason: `Command '${parsed.binary}' not in allowed list` };
  }

  return { allowed: true };
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/sandbox/validators.ts
git commit -m "feat: add sandbox path and command validators"
```

---

### Task 4.2: Create Sandboxed Read File Tool

**Files:**
- Create: `src/sandbox/tools/read-file.ts`

**Step 1: Write read file tool**

```typescript
import * as fs from 'node:fs';
import { isPathAllowed, hasPathTraversal, expandPath } from '../validators.js';

export interface ReadFileParams {
  path: string;
}

export interface ReadFileResult {
  success: boolean;
  content?: string;
  error?: string;
}

export function createReadFileTool(allowedReadPaths: string[], user?: string) {
  return {
    name: 'read_file',
    description: 'Read the contents of a file',
    input_schema: {
      type: 'object' as const,
      properties: {
        path: { type: 'string', description: 'The path to the file to read' },
      },
      required: ['path'],
    },
    execute: (params: ReadFileParams): ReadFileResult => {
      const targetPath = expandPath(params.path, user);

      if (hasPathTraversal(params.path)) {
        return { success: false, error: 'Path traversal detected' };
      }

      if (!isPathAllowed(targetPath, allowedReadPaths, user)) {
        return { success: false, error: `Path not in allowed read paths: ${params.path}` };
      }

      try {
        const content = fs.readFileSync(targetPath, 'utf-8');
        return { success: true, content };
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        return { success: false, error: message };
      }
    },
  };
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/sandbox/tools/read-file.ts
git commit -m "feat: add sandboxed read file tool"
```

---

### Task 4.3: Create Sandboxed Write File Tool

**Files:**
- Create: `src/sandbox/tools/write-file.ts`

**Step 1: Write write file tool**

```typescript
import * as fs from 'node:fs';
import * as path from 'node:path';
import { isPathAllowed, hasPathTraversal, expandPath } from '../validators.js';

export interface WriteFileParams {
  path: string;
  content: string;
}

export interface WriteFileResult {
  success: boolean;
  error?: string;
}

export function createWriteFileTool(allowedWritePaths: string[], user?: string) {
  return {
    name: 'write_file',
    description: 'Write content to a file',
    input_schema: {
      type: 'object' as const,
      properties: {
        path: { type: 'string', description: 'The path to the file to write' },
        content: { type: 'string', description: 'The content to write to the file' },
      },
      required: ['path', 'content'],
    },
    execute: (params: WriteFileParams): WriteFileResult => {
      const targetPath = expandPath(params.path, user);

      if (hasPathTraversal(params.path)) {
        return { success: false, error: 'Path traversal detected' };
      }

      if (!isPathAllowed(targetPath, allowedWritePaths, user)) {
        return { success: false, error: `Path not in allowed write paths: ${params.path}` };
      }

      try {
        const dir = path.dirname(targetPath);
        if (!fs.existsSync(dir)) {
          // Check if we can create parent directories
          if (!isPathAllowed(dir, allowedWritePaths, user)) {
            return { success: false, error: `Cannot create directory outside allowed paths: ${dir}` };
          }
          fs.mkdirSync(dir, { recursive: true });
        }
        fs.writeFileSync(targetPath, params.content, 'utf-8');
        return { success: true };
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        return { success: false, error: message };
      }
    },
  };
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/sandbox/tools/write-file.ts
git commit -m "feat: add sandboxed write file tool"
```

---

### Task 4.4: Create Sandboxed Execute Command Tool

**Files:**
- Create: `src/sandbox/tools/execute-command.ts`

**Step 1: Write execute command tool**

```typescript
import { execSync } from 'node:child_process';
import { isCommandAllowed } from '../validators.js';

export interface ExecuteCommandParams {
  command: string;
  working_directory?: string;
}

export interface ExecuteCommandResult {
  success: boolean;
  stdout?: string;
  stderr?: string;
  exit_code?: number;
  error?: string;
}

export function createExecuteCommandTool(allowedCommands: string[], user?: string) {
  return {
    name: 'execute_command',
    description: 'Execute a shell command',
    input_schema: {
      type: 'object' as const,
      properties: {
        command: { type: 'string', description: 'The command to execute' },
        working_directory: { type: 'string', description: 'Optional working directory' },
      },
      required: ['command'],
    },
    execute: (params: ExecuteCommandParams): ExecuteCommandResult => {
      const check = isCommandAllowed(params.command, allowedCommands);
      if (!check.allowed) {
        return { success: false, error: check.reason };
      }

      try {
        const options: { cwd?: string; uid?: number; encoding: BufferEncoding; timeout: number } = {
          encoding: 'utf-8',
          timeout: 30000,
        };

        if (params.working_directory) {
          options.cwd = params.working_directory;
        }

        // If running as specific user, we'd use sudo -u here
        // For now, execute directly
        const stdout = execSync(params.command, options);
        return { success: true, stdout: stdout.toString(), exit_code: 0 };
      } catch (err: unknown) {
        if (err && typeof err === 'object' && 'status' in err) {
          const execErr = err as { status: number; stdout?: Buffer; stderr?: Buffer };
          return {
            success: false,
            stdout: execErr.stdout?.toString(),
            stderr: execErr.stderr?.toString(),
            exit_code: execErr.status,
            error: `Command exited with code ${execErr.status}`,
          };
        }
        const message = err instanceof Error ? err.message : String(err);
        return { success: false, error: message };
      }
    },
  };
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/sandbox/tools/execute-command.ts
git commit -m "feat: add sandboxed execute command tool"
```

---

### Task 4.5: Create Sandboxed Fetch URL Tool

**Files:**
- Create: `src/sandbox/tools/fetch-url.ts`

**Step 1: Write fetch URL tool**

```typescript
export interface FetchUrlParams {
  url: string;
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
  headers?: Record<string, string>;
  body?: string;
}

export interface FetchUrlResult {
  success: boolean;
  status?: number;
  headers?: Record<string, string>;
  body?: string;
  error?: string;
}

export function createFetchUrlTool(allowNetwork: boolean, allowedDomains?: string[]) {
  return {
    name: 'fetch_url',
    description: 'Fetch content from a URL',
    input_schema: {
      type: 'object' as const,
      properties: {
        url: { type: 'string', description: 'The URL to fetch' },
        method: { type: 'string', enum: ['GET', 'POST', 'PUT', 'DELETE'], description: 'HTTP method' },
        headers: { type: 'object', description: 'Optional headers' },
        body: { type: 'string', description: 'Optional request body' },
      },
      required: ['url'],
    },
    execute: async (params: FetchUrlParams): Promise<FetchUrlResult> => {
      if (!allowNetwork) {
        return { success: false, error: 'Network access is disabled for this rule' };
      }

      let url: URL;
      try {
        url = new URL(params.url);
      } catch {
        return { success: false, error: 'Invalid URL' };
      }

      if (allowedDomains && allowedDomains.length > 0) {
        if (!allowedDomains.includes(url.hostname)) {
          return { success: false, error: `Domain '${url.hostname}' not in allowed domains` };
        }
      }

      try {
        const response = await fetch(params.url, {
          method: params.method || 'GET',
          headers: params.headers,
          body: params.body,
        });

        const headers: Record<string, string> = {};
        response.headers.forEach((value, key) => {
          headers[key] = value;
        });

        const body = await response.text();
        return {
          success: true,
          status: response.status,
          headers,
          body,
        };
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        return { success: false, error: message };
      }
    },
  };
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/sandbox/tools/fetch-url.ts
git commit -m "feat: add sandboxed fetch URL tool"
```

---

### Task 4.6: Create Sandbox Factory

**Files:**
- Create: `src/sandbox/index.ts`

**Step 1: Write sandbox factory**

```typescript
import type { Sandbox, SandboxDefaults } from '../types/index.js';
import { createReadFileTool } from './tools/read-file.js';
import { createWriteFileTool } from './tools/write-file.js';
import { createExecuteCommandTool } from './tools/execute-command.js';
import { createFetchUrlTool } from './tools/fetch-url.js';

export interface SandboxedTools {
  readFile: ReturnType<typeof createReadFileTool>;
  writeFile: ReturnType<typeof createWriteFileTool>;
  executeCommand: ReturnType<typeof createExecuteCommandTool>;
  fetchUrl: ReturnType<typeof createFetchUrlTool>;
}

export function createSandboxedTools(
  sandbox: Sandbox,
  defaults: SandboxDefaults,
  user?: string
): SandboxedTools {
  const allowedCommands = sandbox.allowed_commands ?? defaults.allowed_commands;
  const allowNetwork = sandbox.allow_network ?? false;

  return {
    readFile: createReadFileTool(sandbox.allowed_read_paths, user),
    writeFile: createWriteFileTool(sandbox.allowed_write_paths, user),
    executeCommand: createExecuteCommandTool(allowedCommands, user),
    fetchUrl: createFetchUrlTool(allowNetwork, sandbox.allowed_domains),
  };
}

export { expandPath, normalizePath, isPathAllowed, isCommandAllowed } from './validators.js';
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/sandbox/index.ts
git commit -m "feat: add sandbox factory"
```

---

## Phase 5: Agent Integration

### Task 5.1: Create Agent Context Builder

**Files:**
- Create: `src/agent/context.ts`

**Step 1: Write context builder**

```typescript
import * as fs from 'node:fs';
import type { Rule, Trigger } from '../types/index.js';
import { expandPath } from '../sandbox/index.js';

export interface TriggerContext {
  trigger_type: string;
  timestamp: string;
  [key: string]: unknown;
}

export interface FilesystemTriggerContext extends TriggerContext {
  trigger_type: 'filesystem';
  file_path: string;
  event: string;
  file_metadata?: {
    size: number;
    created: string;
    modified: string;
    is_directory: boolean;
  };
}

export function buildTriggerContext(
  trigger: Trigger,
  eventData: Record<string, unknown>,
  includeMetadata: boolean,
  user?: string
): TriggerContext {
  const base: TriggerContext = {
    trigger_type: trigger.type,
    timestamp: new Date().toISOString(),
  };

  if (trigger.type === 'filesystem') {
    const filePath = eventData.path as string;
    const ctx: FilesystemTriggerContext = {
      ...base,
      trigger_type: 'filesystem',
      file_path: filePath,
      event: eventData.event as string,
    };

    if (includeMetadata && filePath) {
      const expandedPath = expandPath(filePath, user);
      try {
        const stats = fs.statSync(expandedPath);
        ctx.file_metadata = {
          size: stats.size,
          created: stats.birthtime.toISOString(),
          modified: stats.mtime.toISOString(),
          is_directory: stats.isDirectory(),
        };
      } catch {
        // File may no longer exist (e.g., for delete events)
      }
    }

    return ctx;
  }

  if (trigger.type === 'scheduled') {
    return { ...base, scheduled_time: eventData.scheduled_time };
  }

  if (trigger.type === 'system') {
    return { ...base, ...eventData };
  }

  if (trigger.type === 'webhook') {
    return {
      ...base,
      method: eventData.method,
      path: eventData.path,
      headers: eventData.headers,
      body: eventData.body,
    };
  }

  if (trigger.type === 'lifecycle') {
    return { ...base, event: eventData.event };
  }

  return base;
}

export function buildPrompt(rule: Rule, context: TriggerContext): string {
  let prompt = rule.action.prompt;

  // Simple template substitution
  for (const [key, value] of Object.entries(context)) {
    const placeholder = `{{${key}}}`;
    if (typeof value === 'string') {
      prompt = prompt.replaceAll(placeholder, value);
    } else if (value !== undefined) {
      prompt = prompt.replaceAll(placeholder, JSON.stringify(value));
    }
  }

  return prompt;
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/agent/context.ts
git commit -m "feat: add agent context builder"
```

---

### Task 5.2: Create Isolated Agent Runner

**Files:**
- Create: `src/agent/isolated.ts`

**Step 1: Write isolated agent runner**

```typescript
import Anthropic from '@anthropic-ai/sdk';
import type { Rule, GlobalConfig, ExecutionState } from '../types/index.js';
import { createSandboxedTools, type SandboxedTools } from '../sandbox/index.js';
import { buildTriggerContext, buildPrompt, type TriggerContext } from './context.js';
import { Logger } from '../logging/index.js';

export interface ExecutionResult {
  state: ExecutionState;
  output?: string;
  error?: string;
  duration_ms: number;
}

export class IsolatedAgentRunner {
  private client: Anthropic;
  private config: GlobalConfig;
  private logger: Logger;

  constructor(apiKey: string, config: GlobalConfig, logger: Logger) {
    this.client = new Anthropic({ apiKey });
    this.config = config;
    this.logger = logger;
  }

  async execute(
    rule: Rule,
    eventData: Record<string, unknown>
  ): Promise<ExecutionResult> {
    const startTime = Date.now();

    const context = buildTriggerContext(
      rule.trigger,
      eventData,
      rule.action.include_file_metadata,
      rule.run_as_user
    );

    const prompt = buildPrompt(rule, context);
    const tools = createSandboxedTools(rule.sandbox, this.config.sandbox_defaults, rule.run_as_user);

    const timeout = rule.sandbox.max_timeout_seconds ?? this.config.agent_defaults.max_timeout_seconds;
    const model = this.config.agent_defaults.model;

    this.logger.info(`Starting isolated agent for rule '${rule.name}'`, { model, timeout });

    try {
      const result = await this.runAgentLoop(prompt, tools, model, timeout);
      return {
        state: 'success',
        output: result,
        duration_ms: Date.now() - startTime,
      };
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      if (message.includes('timeout')) {
        return {
          state: 'timeout',
          error: message,
          duration_ms: Date.now() - startTime,
        };
      }
      return {
        state: 'failure',
        error: message,
        duration_ms: Date.now() - startTime,
      };
    }
  }

  private async runAgentLoop(
    prompt: string,
    tools: SandboxedTools,
    model: string,
    timeoutSeconds: number
  ): Promise<string> {
    const toolDefinitions = [
      tools.readFile,
      tools.writeFile,
      tools.executeCommand,
      tools.fetchUrl,
    ].map((t) => ({
      name: t.name,
      description: t.description,
      input_schema: t.input_schema,
    }));

    const messages: Anthropic.MessageParam[] = [{ role: 'user', content: prompt }];
    const deadline = Date.now() + timeoutSeconds * 1000;

    while (Date.now() < deadline) {
      const response = await this.client.messages.create({
        model,
        max_tokens: 4096,
        tools: toolDefinitions,
        messages,
      });

      // Check for text response (completion)
      const textContent = response.content.find((c) => c.type === 'text');
      const toolUseContent = response.content.filter((c) => c.type === 'tool_use');

      if (toolUseContent.length === 0) {
        return textContent?.type === 'text' ? textContent.text : '';
      }

      // Process tool calls
      messages.push({ role: 'assistant', content: response.content });

      const toolResults: Anthropic.ToolResultBlockParam[] = [];
      for (const toolUse of toolUseContent) {
        if (toolUse.type !== 'tool_use') continue;

        const tool = [tools.readFile, tools.writeFile, tools.executeCommand, tools.fetchUrl].find(
          (t) => t.name === toolUse.name
        );

        if (!tool) {
          toolResults.push({
            type: 'tool_result',
            tool_use_id: toolUse.id,
            content: JSON.stringify({ error: `Unknown tool: ${toolUse.name}` }),
          });
          continue;
        }

        this.logger.debug(`Executing tool '${toolUse.name}'`, { input: toolUse.input });

        const result = await Promise.resolve(tool.execute(toolUse.input as never));
        toolResults.push({
          type: 'tool_result',
          tool_use_id: toolUse.id,
          content: JSON.stringify(result),
        });
      }

      messages.push({ role: 'user', content: toolResults });

      if (response.stop_reason === 'end_turn') {
        return textContent?.type === 'text' ? textContent.text : '';
      }
    }

    throw new Error('Agent execution timeout');
  }
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/agent/isolated.ts
git commit -m "feat: add isolated agent runner"
```

---

### Task 5.3: Create Agent Index

**Files:**
- Create: `src/agent/index.ts`

**Step 1: Create barrel export**

```typescript
export { IsolatedAgentRunner, type ExecutionResult } from './isolated.js';
export { buildTriggerContext, buildPrompt, type TriggerContext } from './context.js';
```

**Step 2: Commit**

```bash
git add src/agent/index.ts
git commit -m "feat: add agent barrel export"
```

---

## Phase 6: Triggers

### Task 6.1: Create Trigger Interface

**Files:**
- Create: `src/triggers/types.ts`

**Step 1: Write trigger types**

```typescript
import type { Trigger } from '../types/index.js';

export type TriggerCallback = (eventData: Record<string, unknown>) => void;

export interface TriggerHandler {
  start(): void;
  stop(): void;
}

export interface TriggerFactory {
  create(trigger: Trigger, callback: TriggerCallback): TriggerHandler;
  supports(type: string): boolean;
}
```

**Step 2: Commit**

```bash
git add src/triggers/types.ts
git commit -m "feat: add trigger interface types"
```

---

### Task 6.2: Create Filesystem Trigger

**Files:**
- Create: `src/triggers/filesystem.ts`

**Step 1: Write filesystem trigger**

```typescript
import chokidar from 'chokidar';
import type { Trigger } from '../types/index.js';
import type { TriggerHandler, TriggerCallback, TriggerFactory } from './types.js';
import { expandPath } from '../sandbox/index.js';

class FilesystemTriggerHandler implements TriggerHandler {
  private watcher: chokidar.FSWatcher | null = null;
  private trigger: Extract<Trigger, { type: 'filesystem' }>;
  private callback: TriggerCallback;
  private debounceTimers: Map<string, NodeJS.Timeout> = new Map();
  private user?: string;

  constructor(trigger: Extract<Trigger, { type: 'filesystem' }>, callback: TriggerCallback, user?: string) {
    this.trigger = trigger;
    this.callback = callback;
    this.user = user;
  }

  start(): void {
    const paths = this.trigger.watch_paths.map((p) => expandPath(p, this.user));

    this.watcher = chokidar.watch(paths, {
      ignored: this.trigger.ignore_patterns,
      persistent: true,
      ignoreInitial: true,
    });

    const eventMap: Record<string, string> = {
      add: 'file_created',
      change: 'file_modified',
      unlink: 'file_deleted',
      addDir: 'directory_created',
    };

    for (const [chokidarEvent, ourEvent] of Object.entries(eventMap)) {
      if (this.trigger.on_events.includes(ourEvent as typeof this.trigger.on_events[number])) {
        this.watcher.on(chokidarEvent, (path) => this.handleEvent(ourEvent, path));
      }
    }
  }

  private handleEvent(event: string, path: string): void {
    const debounceMs = this.trigger.debounce_seconds * 1000;

    if (debounceMs > 0) {
      const key = `${event}:${path}`;
      const existing = this.debounceTimers.get(key);
      if (existing) {
        clearTimeout(existing);
      }

      const timer = setTimeout(() => {
        this.debounceTimers.delete(key);
        this.callback({ event, path });
      }, debounceMs);

      this.debounceTimers.set(key, timer);
    } else {
      this.callback({ event, path });
    }
  }

  stop(): void {
    if (this.watcher) {
      this.watcher.close();
      this.watcher = null;
    }
    for (const timer of this.debounceTimers.values()) {
      clearTimeout(timer);
    }
    this.debounceTimers.clear();
  }
}

export const filesystemTriggerFactory: TriggerFactory = {
  supports: (type: string) => type === 'filesystem',
  create: (trigger: Trigger, callback: TriggerCallback) => {
    if (trigger.type !== 'filesystem') {
      throw new Error('Invalid trigger type');
    }
    return new FilesystemTriggerHandler(trigger, callback);
  },
};
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/triggers/filesystem.ts
git commit -m "feat: add filesystem trigger"
```

---

### Task 6.3: Create Scheduled Trigger

**Files:**
- Create: `src/triggers/scheduled.ts`

**Step 1: Write scheduled trigger**

```typescript
import cron from 'node-cron';
import type { Trigger } from '../types/index.js';
import type { TriggerHandler, TriggerCallback, TriggerFactory } from './types.js';

class ScheduledTriggerHandler implements TriggerHandler {
  private task: cron.ScheduledTask | null = null;
  private trigger: Extract<Trigger, { type: 'scheduled' }>;
  private callback: TriggerCallback;

  constructor(trigger: Extract<Trigger, { type: 'scheduled' }>, callback: TriggerCallback) {
    this.trigger = trigger;
    this.callback = callback;
  }

  start(): void {
    const cronExpression = this.getCronExpression();
    this.task = cron.schedule(cronExpression, () => {
      this.callback({ scheduled_time: new Date().toISOString() });
    });
  }

  private getCronExpression(): string {
    if (this.trigger.cron_expression) {
      return this.trigger.cron_expression;
    }

    if (this.trigger.run_at) {
      const [hours, minutes] = this.trigger.run_at.split(':').map(Number);
      return `${minutes} ${hours} * * *`;
    }

    if (this.trigger.run_every) {
      const match = this.trigger.run_every.match(/^(\d+)(s|m|h|d)$/);
      if (!match) {
        throw new Error(`Invalid run_every format: ${this.trigger.run_every}`);
      }

      const value = parseInt(match[1], 10);
      const unit = match[2];

      switch (unit) {
        case 's':
          return `*/${value} * * * * *`;
        case 'm':
          return `*/${value} * * * *`;
        case 'h':
          return `0 */${value} * * *`;
        case 'd':
          return `0 0 */${value} * *`;
        default:
          throw new Error(`Unknown time unit: ${unit}`);
      }
    }

    throw new Error('No schedule specified');
  }

  stop(): void {
    if (this.task) {
      this.task.stop();
      this.task = null;
    }
  }
}

export const scheduledTriggerFactory: TriggerFactory = {
  supports: (type: string) => type === 'scheduled',
  create: (trigger: Trigger, callback: TriggerCallback) => {
    if (trigger.type !== 'scheduled') {
      throw new Error('Invalid trigger type');
    }
    return new ScheduledTriggerHandler(trigger, callback);
  },
};
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/triggers/scheduled.ts
git commit -m "feat: add scheduled trigger"
```

---

### Task 6.4: Create Lifecycle Trigger

**Files:**
- Create: `src/triggers/lifecycle.ts`

**Step 1: Write lifecycle trigger**

```typescript
import type { Trigger } from '../types/index.js';
import type { TriggerHandler, TriggerCallback, TriggerFactory } from './types.js';

class LifecycleTriggerHandler implements TriggerHandler {
  private trigger: Extract<Trigger, { type: 'lifecycle' }>;
  private callback: TriggerCallback;
  private started = false;

  constructor(trigger: Extract<Trigger, { type: 'lifecycle' }>, callback: TriggerCallback) {
    this.trigger = trigger;
    this.callback = callback;
  }

  start(): void {
    this.started = true;
    if (this.trigger.on_events.includes('daemon_started')) {
      // Fire immediately on start
      setImmediate(() => {
        if (this.started) {
          this.callback({ event: 'daemon_started' });
        }
      });
    }
  }

  stop(): void {
    if (this.started && this.trigger.on_events.includes('daemon_stopped')) {
      this.callback({ event: 'daemon_stopped' });
    }
    this.started = false;
  }
}

export const lifecycleTriggerFactory: TriggerFactory = {
  supports: (type: string) => type === 'lifecycle',
  create: (trigger: Trigger, callback: TriggerCallback) => {
    if (trigger.type !== 'lifecycle') {
      throw new Error('Invalid trigger type');
    }
    return new LifecycleTriggerHandler(trigger, callback);
  },
};
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/triggers/lifecycle.ts
git commit -m "feat: add lifecycle trigger"
```

---

### Task 6.5: Create Manual Trigger

**Files:**
- Create: `src/triggers/manual.ts`

**Step 1: Write manual trigger**

```typescript
import type { Trigger } from '../types/index.js';
import type { TriggerHandler, TriggerCallback, TriggerFactory } from './types.js';

class ManualTriggerHandler implements TriggerHandler {
  private callback: TriggerCallback;
  private _manualTrigger: (() => void) | null = null;

  constructor(_trigger: Extract<Trigger, { type: 'manual' }>, callback: TriggerCallback) {
    this.callback = callback;
  }

  start(): void {
    this._manualTrigger = () => {
      this.callback({ manual: true, triggered_at: new Date().toISOString() });
    };
  }

  stop(): void {
    this._manualTrigger = null;
  }

  trigger(): void {
    if (this._manualTrigger) {
      this._manualTrigger();
    }
  }
}

export const manualTriggerFactory: TriggerFactory = {
  supports: (type: string) => type === 'manual',
  create: (trigger: Trigger, callback: TriggerCallback) => {
    if (trigger.type !== 'manual') {
      throw new Error('Invalid trigger type');
    }
    return new ManualTriggerHandler(trigger, callback);
  },
};

export { ManualTriggerHandler };
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/triggers/manual.ts
git commit -m "feat: add manual trigger"
```

---

### Task 6.6: Create Trigger Registry

**Files:**
- Create: `src/triggers/index.ts`

**Step 1: Write trigger registry**

```typescript
import type { Trigger } from '../types/index.js';
import type { TriggerHandler, TriggerCallback, TriggerFactory } from './types.js';
import { filesystemTriggerFactory } from './filesystem.js';
import { scheduledTriggerFactory } from './scheduled.js';
import { lifecycleTriggerFactory } from './lifecycle.js';
import { manualTriggerFactory, ManualTriggerHandler } from './manual.js';

export type { TriggerHandler, TriggerCallback, TriggerFactory } from './types.js';
export { ManualTriggerHandler } from './manual.js';

const factories: TriggerFactory[] = [
  filesystemTriggerFactory,
  scheduledTriggerFactory,
  lifecycleTriggerFactory,
  manualTriggerFactory,
];

export function createTrigger(trigger: Trigger, callback: TriggerCallback): TriggerHandler {
  const factory = factories.find((f) => f.supports(trigger.type));
  if (!factory) {
    throw new Error(`No factory for trigger type: ${trigger.type}`);
  }
  return factory.create(trigger, callback);
}

export function isTriggerTypeSupported(type: string): boolean {
  return factories.some((f) => f.supports(type));
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/triggers/index.ts
git commit -m "feat: add trigger registry"
```

---

## Phase 7: Rule Executor

### Task 7.1: Create Rule Executor

**Files:**
- Create: `src/daemon/executor.ts`

**Step 1: Write rule executor**

```typescript
import type { Rule, GlobalConfig, ExecutionState } from '../types/index.js';
import type { LoadedRule } from './rule-loader.js';
import { IsolatedAgentRunner, type ExecutionResult } from '../agent/index.js';
import { createTrigger, type TriggerHandler, ManualTriggerHandler } from '../triggers/index.js';
import { Logger } from '../logging/index.js';

interface RuleExecution {
  rule: Rule;
  handler: TriggerHandler;
  lastResult?: ExecutionResult;
}

export class RuleExecutor {
  private config: GlobalConfig;
  private apiKey: string;
  private logger: Logger;
  private executions: Map<string, RuleExecution> = new Map();
  private agentRunner: IsolatedAgentRunner;
  private runningCount = 0;

  constructor(config: GlobalConfig, apiKey: string, logger: Logger) {
    this.config = config;
    this.apiKey = apiKey;
    this.logger = logger;
    this.agentRunner = new IsolatedAgentRunner(apiKey, config, logger);
  }

  registerRule(loadedRule: LoadedRule): void {
    const { rule } = loadedRule;

    if (this.executions.has(rule.name)) {
      this.logger.warn(`Rule '${rule.name}' already registered, skipping`);
      return;
    }

    const handler = createTrigger(rule.trigger, (eventData) => {
      this.handleTrigger(rule, eventData);
    });

    this.executions.set(rule.name, { rule, handler });
    this.logger.info(`Registered rule '${rule.name}'`, { trigger: rule.trigger.type });
  }

  unregisterRule(name: string): void {
    const execution = this.executions.get(name);
    if (execution) {
      execution.handler.stop();
      this.executions.delete(name);
      this.logger.info(`Unregistered rule '${name}'`);
    }
  }

  startAll(): void {
    for (const [name, execution] of this.executions) {
      if (execution.rule.enabled) {
        execution.handler.start();
        this.logger.info(`Started rule '${name}'`);
      }
    }
  }

  stopAll(): void {
    for (const [name, execution] of this.executions) {
      execution.handler.stop();
      this.logger.info(`Stopped rule '${name}'`);
    }
  }

  async triggerManually(name: string): Promise<ExecutionResult> {
    const execution = this.executions.get(name);
    if (!execution) {
      throw new Error(`Rule '${name}' not found`);
    }

    if (execution.rule.trigger.type === 'manual') {
      const handler = execution.handler as ManualTriggerHandler;
      handler.trigger();
    }

    // For non-manual rules, trigger directly
    return this.executeRule(execution.rule, { manual: true });
  }

  private async handleTrigger(rule: Rule, eventData: Record<string, unknown>): Promise<void> {
    if (!rule.enabled) {
      this.logger.info(`Rule '${rule.name}' is disabled, skipping`);
      return;
    }

    if (this.runningCount >= this.config.rule_execution.max_concurrent_rules) {
      this.logger.warn(`Max concurrent rules reached, queuing '${rule.name}'`);
      // Simple queue: just wait and retry
      setTimeout(() => this.handleTrigger(rule, eventData), 1000);
      return;
    }

    // Check dependencies
    for (const depName of rule.depends_on_rules) {
      const dep = this.executions.get(depName);
      if (dep?.lastResult?.state !== 'success') {
        this.logger.info(`Rule '${rule.name}' waiting for dependency '${depName}'`);
        return;
      }
    }

    const result = await this.executeRule(rule, eventData);

    // Handle triggered rules
    for (const triggerRule of rule.triggers_rules) {
      const triggerName = typeof triggerRule === 'string' ? triggerRule : triggerRule.rule;
      const onStatus = typeof triggerRule === 'string' ? 'success' : triggerRule.on_status;

      const shouldTrigger =
        onStatus === 'any' ||
        (onStatus === 'success' && result.state === 'success') ||
        (onStatus === 'failure' && result.state === 'failure');

      if (shouldTrigger) {
        const triggered = this.executions.get(triggerName);
        if (triggered) {
          this.handleTrigger(triggered.rule, { triggered_by: rule.name });
        }
      }
    }
  }

  private async executeRule(rule: Rule, eventData: Record<string, unknown>): Promise<ExecutionResult> {
    this.runningCount++;
    this.logger.info(`Executing rule '${rule.name}'`, { eventData });

    try {
      const result = await this.agentRunner.execute(rule, eventData);

      const execution = this.executions.get(rule.name);
      if (execution) {
        execution.lastResult = result;
      }

      this.logger.info(`Rule '${rule.name}' completed`, {
        state: result.state,
        duration_ms: result.duration_ms,
      });

      // Handle retries on failure
      if (result.state === 'failure' && rule.on_failure.retry) {
        // Retry logic would go here
      }

      return result;
    } finally {
      this.runningCount--;
    }
  }

  getStatus(): { rules: number; running: number } {
    return {
      rules: this.executions.size,
      running: this.runningCount,
    };
  }
}
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/daemon/executor.ts
git commit -m "feat: add rule executor"
```

---

## Phase 8: Daemon

### Task 8.1: Create Daemon Entry Point

**Files:**
- Create: `src/daemon/index.ts`

**Step 1: Write daemon entry point**

```typescript
import * as path from 'node:path';
import { loadConfig, getApiKey, ensureDirectories, LOG_DIR } from './config-loader.js';
import { loadAllRules, validateRuleDependencies } from './rule-loader.js';
import { RuleExecutor } from './executor.js';
import { Logger } from '../logging/index.js';

async function main(): Promise<void> {
  console.log('srvrmgrd starting...');

  // Ensure directories exist
  ensureDirectories();

  // Load config
  const config = loadConfig();
  const apiKey = getApiKey(config);

  // Create logger
  const logPath = path.join(LOG_DIR, 'srvrmgrd.log');
  const logger = new Logger(logPath, config.logging, config.daemon.log_level);

  logger.info('Daemon starting', { pid: process.pid });

  // Load rules
  const { rules, errors } = loadAllRules();

  for (const err of errors) {
    logger.error(`Failed to load rule: ${err.file}`, { error: err.error });
  }

  // Validate dependencies
  const depErrors = validateRuleDependencies(rules);
  for (const err of depErrors) {
    logger.error(err);
  }

  if (errors.length > 0 || depErrors.length > 0) {
    logger.warn('Some rules failed to load, continuing with valid rules');
  }

  logger.info(`Loaded ${rules.length} rules`);

  // Create executor
  const executor = new RuleExecutor(config, apiKey, logger);

  // Register rules
  for (const loadedRule of rules) {
    executor.registerRule(loadedRule);
  }

  // Start all rules
  executor.startAll();

  logger.info('Daemon started successfully');

  // Handle shutdown
  const shutdown = (): void => {
    logger.info('Daemon shutting down');
    executor.stopAll();
    logger.close();
    process.exit(0);
  };

  process.on('SIGTERM', shutdown);
  process.on('SIGINT', shutdown);

  // Keep process alive
  setInterval(() => {
    const status = executor.getStatus();
    logger.debug('Heartbeat', status);
  }, 60000);
}

main().catch((err) => {
  console.error('Fatal error:', err);
  process.exit(1);
});
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add src/daemon/index.ts
git commit -m "feat: add daemon entry point"
```

---

### Task 8.2: Create Daemon Executable

**Files:**
- Create: `bin/srvrmgrd`

**Step 1: Write daemon executable**

```bash
#!/usr/bin/env node
import('../dist/daemon/index.js');
```

**Step 2: Make executable**

Run: `chmod +x bin/srvrmgrd`

**Step 3: Commit**

```bash
git add bin/srvrmgrd
git commit -m "feat: add daemon executable"
```

---

## Phase 9: CLI

### Task 9.1: Create CLI Entry Point

**Files:**
- Create: `src/cli/index.ts`

**Step 1: Write CLI entry point**

```typescript
import { Command } from 'commander';
import { startCommand } from './commands/start.js';
import { stopCommand } from './commands/stop.js';
import { statusCommand } from './commands/status.js';
import { listCommand } from './commands/list.js';
import { validateCommand } from './commands/validate.js';
import { runCommand } from './commands/run.js';
import { initCommand } from './commands/init.js';
import { logsCommand } from './commands/logs.js';

const program = new Command();

program
  .name('srvrmgr')
  .description('macOS server management CLI')
  .version('0.1.0');

program.addCommand(startCommand);
program.addCommand(stopCommand);
program.addCommand(statusCommand);
program.addCommand(listCommand);
program.addCommand(validateCommand);
program.addCommand(runCommand);
program.addCommand(initCommand);
program.addCommand(logsCommand);

program.parse();
```

**Step 2: Verify it compiles**

Run: `npx tsc --noEmit`
Expected: Error (commands don't exist yet)

**Step 3: Continue to next task**

---

### Task 9.2: Create CLI Init Command

**Files:**
- Create: `src/cli/commands/init.ts`

**Step 1: Write init command**

```typescript
import { Command } from 'commander';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { stringify as stringifyYaml } from 'yaml';
import { CONFIG_DIR, RULES_DIR, STATE_DIR, SECRETS_DIR, LOG_DIR, CONFIG_PATH } from '../../daemon/config-loader.js';

export const initCommand = new Command('init')
  .description('Initialize srvrmgr directories and default config')
  .action(() => {
    const dirs = [CONFIG_DIR, RULES_DIR, STATE_DIR, SECRETS_DIR, LOG_DIR, path.join(LOG_DIR, 'rules')];

    for (const dir of dirs) {
      if (!fs.existsSync(dir)) {
        fs.mkdirSync(dir, { recursive: true });
        console.log(`Created: ${dir}`);
      } else {
        console.log(`Exists: ${dir}`);
      }
    }

    if (!fs.existsSync(CONFIG_PATH)) {
      const defaultConfig = {
        daemon: {
          log_level: 'info',
          webhook_listen_port: 9876,
          webhook_listen_address: '127.0.0.1',
        },
        agent: {
          api_key_source: 'file',
          api_key_file: path.join(SECRETS_DIR, 'api_key'),
        },
        agent_defaults: {
          agent_mode: 'isolated',
          model: 'claude-sonnet-4-20250514',
          max_timeout_seconds: 300,
          max_memory_mb: 1024,
          allow_network: false,
        },
        sandbox_defaults: {
          allowed_commands: ['ls', 'cat', 'head', 'tail', 'file', 'mkdir', 'cp', 'mv', 'rm'],
        },
        logging: {
          rotate_on_size_mb: 50,
          rotate_on_days: 7,
          keep_rotated_files: 5,
          format: 'json',
        },
        persistent_agents: {
          max_idle_seconds: 3600,
          max_concurrent: 5,
        },
        rule_execution: {
          max_concurrent_rules: 10,
          retry_on_failure: false,
          retry_attempts: 3,
          retry_delay_seconds: 30,
        },
      };

      fs.writeFileSync(CONFIG_PATH, stringifyYaml(defaultConfig), 'utf-8');
      console.log(`Created: ${CONFIG_PATH}`);
    } else {
      console.log(`Exists: ${CONFIG_PATH}`);
    }

    console.log('\nInitialization complete.');
    console.log(`\nNext steps:`);
    console.log(`1. Add your API key: echo "sk-ant-..." | sudo tee "${path.join(SECRETS_DIR, 'api_key')}"`);
    console.log(`2. Set permissions: sudo chmod 600 "${path.join(SECRETS_DIR, 'api_key')}"`);
    console.log(`3. Create rules in: ${RULES_DIR}`);
    console.log(`4. Start daemon: sudo srvrmgr start`);
  });
```

**Step 2: Commit**

```bash
git add src/cli/commands/init.ts
git commit -m "feat: add CLI init command"
```

---

### Task 9.3: Create CLI Start Command

**Files:**
- Create: `src/cli/commands/start.ts`

**Step 1: Write start command**

```typescript
import { Command } from 'commander';
import { execSync } from 'node:child_process';
import * as fs from 'node:fs';
import * as path from 'node:path';

const PLIST_PATH = '/Library/LaunchDaemons/com.srvrmgr.daemon.plist';
const PLIST_TEMPLATE = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.srvrmgr.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/srvrmgrd</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>ThrottleInterval</key>
    <integer>10</integer>
    <key>StandardOutPath</key>
    <string>/Library/Logs/srvrmgr/srvrmgrd.stdout.log</string>
    <key>StandardErrorPath</key>
    <string>/Library/Logs/srvrmgr/srvrmgrd.stderr.log</string>
    <key>UserName</key>
    <string>root</string>
</dict>
</plist>`;

export const startCommand = new Command('start')
  .description('Start the srvrmgr daemon')
  .action(() => {
    if (process.getuid?.() !== 0) {
      console.error('Error: This command must be run as root (use sudo)');
      process.exit(1);
    }

    // Install plist if needed
    if (!fs.existsSync(PLIST_PATH)) {
      fs.writeFileSync(PLIST_PATH, PLIST_TEMPLATE, 'utf-8');
      console.log('Installed launchd plist');
    }

    try {
      // Check if already loaded
      try {
        execSync('launchctl list com.srvrmgr.daemon', { stdio: 'pipe' });
        console.log('Daemon is already running');
        return;
      } catch {
        // Not loaded, continue to load
      }

      execSync(`launchctl load ${PLIST_PATH}`, { stdio: 'inherit' });
      console.log('Daemon started');
    } catch (err) {
      console.error('Failed to start daemon:', err);
      process.exit(1);
    }
  });
```

**Step 2: Commit**

```bash
git add src/cli/commands/start.ts
git commit -m "feat: add CLI start command"
```

---

### Task 9.4: Create CLI Stop Command

**Files:**
- Create: `src/cli/commands/stop.ts`

**Step 1: Write stop command**

```typescript
import { Command } from 'commander';
import { execSync } from 'node:child_process';

const PLIST_PATH = '/Library/LaunchDaemons/com.srvrmgr.daemon.plist';

export const stopCommand = new Command('stop')
  .description('Stop the srvrmgr daemon')
  .action(() => {
    if (process.getuid?.() !== 0) {
      console.error('Error: This command must be run as root (use sudo)');
      process.exit(1);
    }

    try {
      execSync(`launchctl unload ${PLIST_PATH}`, { stdio: 'inherit' });
      console.log('Daemon stopped');
    } catch (err) {
      console.error('Failed to stop daemon:', err);
      process.exit(1);
    }
  });
```

**Step 2: Commit**

```bash
git add src/cli/commands/stop.ts
git commit -m "feat: add CLI stop command"
```

---

### Task 9.5: Create CLI Status Command

**Files:**
- Create: `src/cli/commands/status.ts`

**Step 1: Write status command**

```typescript
import { Command } from 'commander';
import { execSync } from 'node:child_process';

export const statusCommand = new Command('status')
  .description('Show daemon status')
  .action(() => {
    try {
      const output = execSync('launchctl list com.srvrmgr.daemon', { encoding: 'utf-8' });
      const lines = output.trim().split('\n');

      if (lines.length >= 2) {
        const parts = lines[1].split('\t');
        const pid = parts[0];
        const exitCode = parts[1];

        if (pid !== '-') {
          console.log(`Status: Running (PID ${pid})`);
        } else {
          console.log(`Status: Stopped (last exit code: ${exitCode})`);
        }
      }
    } catch {
      console.log('Status: Not installed');
    }
  });
```

**Step 2: Commit**

```bash
git add src/cli/commands/status.ts
git commit -m "feat: add CLI status command"
```

---

### Task 9.6: Create CLI List Command

**Files:**
- Create: `src/cli/commands/list.ts`

**Step 1: Write list command**

```typescript
import { Command } from 'commander';
import { loadAllRules } from '../../daemon/rule-loader.js';

export const listCommand = new Command('list')
  .description('List all rules')
  .action(() => {
    const { rules, errors } = loadAllRules();

    if (rules.length === 0 && errors.length === 0) {
      console.log('No rules found');
      return;
    }

    console.log('Rules:\n');

    for (const { rule } of rules) {
      const status = rule.enabled ? '✓' : '✗';
      console.log(`  ${status} ${rule.name}`);
      console.log(`    Trigger: ${rule.trigger.type}`);
      if (rule.description) {
        console.log(`    Description: ${rule.description}`);
      }
      console.log();
    }

    if (errors.length > 0) {
      console.log('Errors:\n');
      for (const err of errors) {
        console.log(`  ✗ ${err.file}: ${err.error}`);
      }
    }
  });
```

**Step 2: Commit**

```bash
git add src/cli/commands/list.ts
git commit -m "feat: add CLI list command"
```

---

### Task 9.7: Create CLI Validate Command

**Files:**
- Create: `src/cli/commands/validate.ts`

**Step 1: Write validate command**

```typescript
import { Command } from 'commander';
import * as path from 'node:path';
import { loadRule, loadAllRules, validateRuleDependencies } from '../../daemon/rule-loader.js';
import { RULES_DIR } from '../../daemon/config-loader.js';

export const validateCommand = new Command('validate')
  .description('Validate rule configuration')
  .argument('[rule-name]', 'Specific rule to validate')
  .action((ruleName?: string) => {
    if (ruleName) {
      const filePath = path.join(RULES_DIR, `${ruleName}.yaml`);
      try {
        const rule = loadRule(filePath);
        console.log(`✓ Rule '${rule.name}' is valid`);
      } catch (err) {
        console.error(`✗ Rule '${ruleName}' is invalid:`);
        console.error(`  ${err instanceof Error ? err.message : err}`);
        process.exit(1);
      }
      return;
    }

    const { rules, errors } = loadAllRules();
    let hasErrors = false;

    for (const { rule } of rules) {
      console.log(`✓ ${rule.name}`);
    }

    for (const err of errors) {
      console.error(`✗ ${err.file}: ${err.error}`);
      hasErrors = true;
    }

    const depErrors = validateRuleDependencies(rules);
    for (const err of depErrors) {
      console.error(`✗ ${err}`);
      hasErrors = true;
    }

    if (hasErrors) {
      process.exit(1);
    } else {
      console.log(`\nAll ${rules.length} rules are valid`);
    }
  });
```

**Step 2: Commit**

```bash
git add src/cli/commands/validate.ts
git commit -m "feat: add CLI validate command"
```

---

### Task 9.8: Create CLI Run Command

**Files:**
- Create: `src/cli/commands/run.ts`

**Step 1: Write run command**

```typescript
import { Command } from 'commander';
import * as path from 'node:path';
import { loadConfig, getApiKey, LOG_DIR } from '../../daemon/config-loader.js';
import { loadRule } from '../../daemon/rule-loader.js';
import { RULES_DIR } from '../../daemon/config-loader.js';
import { IsolatedAgentRunner } from '../../agent/index.js';
import { Logger } from '../../logging/index.js';

export const runCommand = new Command('run')
  .description('Manually run a rule')
  .argument('<rule-name>', 'Rule to run')
  .action(async (ruleName: string) => {
    const filePath = path.join(RULES_DIR, `${ruleName}.yaml`);

    let rule;
    try {
      rule = loadRule(filePath);
    } catch (err) {
      console.error(`Failed to load rule: ${err instanceof Error ? err.message : err}`);
      process.exit(1);
    }

    const config = loadConfig();
    const apiKey = getApiKey(config);
    const logPath = path.join(LOG_DIR, 'rules', `${ruleName}.log`);
    const logger = new Logger(logPath, config.logging, config.daemon.log_level);

    console.log(`Running rule '${ruleName}'...`);

    const runner = new IsolatedAgentRunner(apiKey, config, logger);
    const result = await runner.execute(rule, { manual: true });

    console.log(`\nResult: ${result.state}`);
    console.log(`Duration: ${result.duration_ms}ms`);

    if (result.output) {
      console.log(`\nOutput:\n${result.output}`);
    }

    if (result.error) {
      console.error(`\nError: ${result.error}`);
    }

    logger.close();
    process.exit(result.state === 'success' ? 0 : 1);
  });
```

**Step 2: Commit**

```bash
git add src/cli/commands/run.ts
git commit -m "feat: add CLI run command"
```

---

### Task 9.9: Create CLI Logs Command

**Files:**
- Create: `src/cli/commands/logs.ts`

**Step 1: Write logs command**

```typescript
import { Command } from 'commander';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { LOG_DIR } from '../../daemon/config-loader.js';

export const logsCommand = new Command('logs')
  .description('View logs')
  .argument('[rule-name]', 'Specific rule logs (omit for daemon logs)')
  .option('-f, --follow', 'Follow log output')
  .option('-n, --lines <number>', 'Number of lines to show', '50')
  .action((ruleName: string | undefined, options: { follow?: boolean; lines: string }) => {
    const logPath = ruleName
      ? path.join(LOG_DIR, 'rules', `${ruleName}.log`)
      : path.join(LOG_DIR, 'srvrmgrd.log');

    if (!fs.existsSync(logPath)) {
      console.error(`Log file not found: ${logPath}`);
      process.exit(1);
    }

    const lines = parseInt(options.lines, 10);

    if (options.follow) {
      // Tail -f equivalent
      const content = fs.readFileSync(logPath, 'utf-8');
      const allLines = content.trim().split('\n');
      console.log(allLines.slice(-lines).join('\n'));

      fs.watchFile(logPath, { interval: 100 }, () => {
        const newContent = fs.readFileSync(logPath, 'utf-8');
        const newLines = newContent.trim().split('\n');
        const lastLine = allLines[allLines.length - 1];
        const startIdx = newLines.indexOf(lastLine) + 1;
        if (startIdx > 0 && startIdx < newLines.length) {
          for (let i = startIdx; i < newLines.length; i++) {
            console.log(newLines[i]);
            allLines.push(newLines[i]);
          }
        }
      });
    } else {
      const content = fs.readFileSync(logPath, 'utf-8');
      const allLines = content.trim().split('\n');
      console.log(allLines.slice(-lines).join('\n'));
    }
  });
```

**Step 2: Commit**

```bash
git add src/cli/commands/logs.ts
git commit -m "feat: add CLI logs command"
```

---

### Task 9.10: Finalize CLI

**Files:**
- Verify: `src/cli/index.ts` compiles

**Step 1: Verify CLI compiles**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 2: Commit CLI entry point**

```bash
git add src/cli/index.ts
git commit -m "feat: add CLI entry point"
```

---

### Task 9.11: Create CLI Executable

**Files:**
- Create: `bin/srvrmgr`

**Step 1: Write CLI executable**

```bash
#!/usr/bin/env node
import('../dist/cli/index.js');
```

**Step 2: Make executable**

Run: `chmod +x bin/srvrmgr`

**Step 3: Commit**

```bash
git add bin/srvrmgr
git commit -m "feat: add CLI executable"
```

---

## Phase 10: Installation

### Task 10.1: Create launchd Template

**Files:**
- Create: `install/com.srvrmgr.daemon.plist`

**Step 1: Write plist template**

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.srvrmgr.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/srvrmgrd</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>ThrottleInterval</key>
    <integer>10</integer>
    <key>StandardOutPath</key>
    <string>/Library/Logs/srvrmgr/srvrmgrd.stdout.log</string>
    <key>StandardErrorPath</key>
    <string>/Library/Logs/srvrmgr/srvrmgrd.stderr.log</string>
    <key>UserName</key>
    <string>root</string>
</dict>
</plist>
```

**Step 2: Commit**

```bash
git add install/com.srvrmgr.daemon.plist
git commit -m "feat: add launchd plist template"
```

---

### Task 10.2: Build and Test

**Step 1: Build project**

Run: `npm run build`
Expected: Compiles without errors

**Step 2: Verify dist structure**

Run: `ls -la dist/`
Expected: daemon/, cli/, types/, logging/, sandbox/, agent/, triggers/

**Step 3: Commit build artifacts to gitignore**

Already done in Task 1.1

---

### Task 10.3: Create Example Rule

**Files:**
- Create: `examples/organize-downloads.yaml`

**Step 1: Write example rule**

```yaml
name: organize-downloads
description: Keep Downloads folder organized by file type
enabled: true
agent_mode: isolated
run_as_user: cole

trigger:
  type: filesystem
  watch_paths:
    - ~/Downloads
  on_events: [file_created]
  ignore_patterns: ["*.tmp", ".DS_Store", "*.crdownload"]
  debounce_seconds: 5

action:
  prompt: |
    A new file appeared in Downloads: {{file_path}}

    Check its type and move it to the appropriate folder:
    - Images (.jpg, .png, .gif, .webp) → ~/Pictures/Downloads
    - Documents (.pdf, .doc, .docx, .txt) → ~/Documents/Downloads
    - Videos (.mp4, .mov, .avi) → ~/Movies/Downloads
    - Archives (.zip, .tar, .gz, .rar) → extract to ~/Downloads/Extracted/
    - Other → leave in place

    Create destination folders if they don't exist.
    Report what you did.
  include_file_metadata: true

sandbox:
  allowed_read_paths:
    - ~/Downloads
  allowed_write_paths:
    - ~/Downloads
    - ~/Pictures/Downloads
    - ~/Documents/Downloads
    - ~/Movies/Downloads
  allowed_commands: [mv, cp, mkdir, unzip, tar, file, ls]
  allow_network: false
  max_timeout_seconds: 60
  max_memory_mb: 512

on_failure:
  retry: false

depends_on_rules: []
triggers_rules: []
```

**Step 2: Commit**

```bash
git add examples/organize-downloads.yaml
git commit -m "docs: add example organize-downloads rule"
```

---

## Summary

This plan implements srvrmgr in 10 phases:

1. **Project Setup** - package.json, TypeScript config, Zod schemas
2. **Logging** - Structured logger with rotation
3. **Config Loading** - YAML parsing with Zod validation
4. **Sandbox Tools** - Sandboxed file, command, and network operations
5. **Agent Integration** - Claude SDK integration with tool dispatch
6. **Triggers** - Filesystem, scheduled, lifecycle, manual triggers
7. **Rule Executor** - Orchestration with dependencies
8. **Daemon** - Main daemon process
9. **CLI** - All management commands
10. **Installation** - launchd template, example rules

Each task is small (2-5 minutes) with clear steps. Build frequently to catch errors early.
