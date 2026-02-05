# Homebrew Installer & Uninstaller Design

## Overview

Add Homebrew formula for installing srvrmgr and a CLI uninstall command for clean removal.

## Goals

- Distribute srvrmgr via Homebrew tap at `../homebrew-formulas/`
- Maintain LaunchDaemon behavior (system-level, runs as root)
- Provide clean uninstall via `srvrmgr uninstall` command

## Homebrew Formula

**Location:** `../homebrew-formulas/Formula/srvrmgr.rb`

**Installation behavior:**
- Builds from source using `go build`
- Installs `srvrmgr` and `srvrmgrd` binaries to Homebrew bin prefix
- Bundles `com.srvrmgr.daemon.plist` in the formula prefix (not directly to LaunchDaemons)

**Caveats (post-install instructions):**
```
To complete installation:
  1. Initialize config: srvrmgr init
  2. Copy the plist: sudo cp #{opt_prefix}/com.srvrmgr.daemon.plist /Library/LaunchDaemons/
  3. Load the daemon: sudo launchctl load /Library/LaunchDaemons/com.srvrmgr.daemon.plist

To uninstall completely:
  srvrmgr uninstall
```

## Uninstall Command

**Command:** `srvrmgr uninstall`

**Behavior:**
1. Check for root privileges, exit with error if not sudo
2. Stop daemon if running via `launchctl unload`
3. Remove plist from `/Library/LaunchDaemons/`
4. Prompt user: "Remove config and rules? (y/N)"
   - If yes: remove `/Library/Application Support/srvrmgr/`
5. Print: "Run `brew uninstall srvrmgr` to remove binaries"

**Flags:**
- `--keep-config` - Skip prompt, preserve config/rules
- `--remove-config` - Skip prompt, remove everything

## Makefile Changes

- Remove `install` and `uninstall` targets (Homebrew handles installation)
- Keep `build`, `test`, `clean` targets

## File Changes

| File | Change |
|------|--------|
| `../homebrew-formulas/Formula/srvrmgr.rb` | New - Homebrew formula |
| `cmd/srvrmgr/main.go` | Add `uninstall` subcommand |
| `Makefile` | Remove install/uninstall targets |
