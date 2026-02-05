# Homebrew Installer & Uninstaller Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Homebrew formula for srvrmgr distribution and `srvrmgr uninstall` CLI command.

**Architecture:** Homebrew formula builds from source, installs binaries and bundled plist. Uninstall command handles daemon shutdown, plist removal, and optional config cleanup.

**Tech Stack:** Go, Homebrew Ruby DSL

---

### Task 1: Add `uninstall` Command to CLI

**Files:**
- Modify: `cmd/srvrmgr/main.go`

**Step 1: Add uninstall case to switch statement**

In `main()`, add the uninstall case after the `logs` case (around line 53):

```go
	case "logs":
		err = cmdLogs(args)
	case "uninstall":
		err = cmdUninstall(args)
	case "help", "-h", "--help":
```

**Step 2: Add uninstall to usage text**

Update `printUsage()` to include uninstall:

```go
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
```

**Step 3: Implement cmdUninstall function**

Add this function after `cmdLogs`:

```go
func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	keepConfig := fs.Bool("keep-config", false, "keep config and rules")
	removeConfig := fs.Bool("remove-config", false, "remove config and rules without prompting")
	fs.Parse(args)

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
```

**Step 4: Build and test manually**

Run:
```bash
make build
```

Expected: Builds successfully

**Step 5: Commit**

```bash
git add cmd/srvrmgr/main.go
git commit -m "feat: add uninstall command to CLI"
```

---

### Task 2: Update Makefile

**Files:**
- Modify: `Makefile`

**Step 1: Remove install and uninstall targets**

Replace entire Makefile with:

```makefile
.PHONY: build test clean

BINDIR := bin
DAEMON := $(BINDIR)/srvrmgrd
CLI := $(BINDIR)/srvrmgr

build: $(DAEMON) $(CLI)

$(DAEMON): cmd/srvrmgrd/main.go internal/**/*.go
	go build -o $@ ./cmd/srvrmgrd

$(CLI): cmd/srvrmgr/main.go internal/**/*.go
	go build -o $@ ./cmd/srvrmgr

test:
	go test -v ./...

clean:
	rm -rf $(BINDIR)
```

**Step 2: Verify build still works**

Run:
```bash
make clean && make build
```

Expected: Builds `bin/srvrmgrd` and `bin/srvrmgr`

**Step 3: Commit**

```bash
git add Makefile
git commit -m "chore: remove install/uninstall targets (use Homebrew)"
```

---

### Task 3: Create Homebrew Formula

**Files:**
- Create: `../homebrew-formulas/Formula/srvrmgr.rb`

**Step 1: Create the formula**

```ruby
class Srvrmgr < Formula
  desc "Server management daemon using Claude Code AI agents"
  homepage "https://github.com/colebrumley/srvrmgr"
  url "https://github.com/colebrumley/srvrmgr.git", branch: "main"
  version "0.1.0"
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", "-o", bin/"srvrmgrd", "./cmd/srvrmgrd"
    system "go", "build", "-o", bin/"srvrmgr", "./cmd/srvrmgr"
    prefix.install "install/com.srvrmgr.daemon.plist"
  end

  def caveats
    <<~EOS
      To complete installation:
        1. Initialize config:
             srvrmgr init

        2. Copy the launchd plist:
             sudo cp #{opt_prefix}/com.srvrmgr.daemon.plist /Library/LaunchDaemons/

        3. Start the daemon:
             sudo launchctl load /Library/LaunchDaemons/com.srvrmgr.daemon.plist

      To uninstall completely:
        sudo srvrmgr uninstall
    EOS
  end

  test do
    assert_match "srvrmgr", shell_output("#{bin}/srvrmgr help")
  end
end
```

**Step 2: Verify formula syntax**

Run:
```bash
cd ../homebrew-formulas && brew audit --strict Formula/srvrmgr.rb
```

Expected: No errors (warnings are OK)

**Step 3: Test formula installation locally**

Run:
```bash
brew install --build-from-source ./Formula/srvrmgr.rb
```

Expected: Installs successfully, shows caveats

**Step 4: Verify binaries installed**

Run:
```bash
which srvrmgr && srvrmgr help
```

Expected: Shows path and help output

**Step 5: Commit formula to tap repo**

```bash
cd ../homebrew-formulas
git add Formula/srvrmgr.rb
git commit -m "feat: add srvrmgr formula"
```

---

### Task 4: Update plist for Homebrew paths

**Files:**
- Modify: `install/com.srvrmgr.daemon.plist`

**Step 1: Update binary path to use Homebrew prefix**

The plist currently hardcodes `/usr/local/bin/srvrmgrd`. Homebrew on Apple Silicon installs to `/opt/homebrew/bin/`. Update to use a symlink-friendly path:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
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

    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin</string>
    </dict>
</dict>
</plist>
```

Note: The PATH environment variable ensures the daemon can find binaries on both Intel (`/usr/local/bin`) and Apple Silicon (`/opt/homebrew/bin`) Macs.

**Step 2: Commit**

```bash
git add install/com.srvrmgr.daemon.plist
git commit -m "chore: add PATH to plist for Homebrew compatibility"
```

---

### Task 5: Push changes and test end-to-end

**Step 1: Push srvrmgr changes**

```bash
git push origin main
```

**Step 2: Push homebrew-formulas changes**

```bash
cd ../homebrew-formulas
git push origin main
```

**Step 3: Test fresh install from tap**

```bash
brew uninstall srvrmgr 2>/dev/null || true
brew install colebrumley/formulas/srvrmgr
```

Expected: Installs and shows caveats

**Step 4: Test uninstall command**

```bash
sudo srvrmgr uninstall --keep-config
```

Expected: Removes plist, prints completion message
