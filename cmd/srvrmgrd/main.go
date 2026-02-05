// cmd/srvrmgrd/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/colebrumley/srvrmgr/internal/daemon"
	"github.com/colebrumley/srvrmgr/internal/mcp"
)

const (
	defaultConfigPath = "/Library/Application Support/srvrmgr/config.yaml"
	defaultRulesDir   = "/Library/Application Support/srvrmgr/rules"
)

const defaultMCPPort = "9877"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "mcp-server":
			runMCPServer()
			return
		case "mcp-http-server":
			runMCPHTTPServer()
			return
		}
	}

	runDaemon()
}

func runMCPServer() {
	dbPath := os.Getenv("SRVRMGR_MEMORY_DB")
	if dbPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error getting home directory: %v\n", err)
			os.Exit(1)
		}
		dbPath = filepath.Join(homeDir, "Library/Application Support/srvrmgr/memory.db")
	}

	server, err := mcp.NewServer(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating MCP server: %v\n", err)
		os.Exit(1)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := server.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}

func runMCPHTTPServer() {
	dbPath := os.Getenv("SRVRMGR_MEMORY_DB")
	if dbPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error getting home directory: %v\n", err)
			os.Exit(1)
		}
		dbPath = filepath.Join(homeDir, "Library/Application Support/srvrmgr/memory.db")
	}

	port := os.Getenv("SRVRMGR_MCP_PORT")
	if port == "" {
		port = defaultMCPPort
	}
	addr := "127.0.0.1:" + port

	fmt.Fprintf(os.Stderr, "Loading embedding model...\n")
	server, err := mcp.NewServer(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating MCP server: %v\n", err)
		os.Exit(1)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\nShutting down MCP HTTP server...\n")
		cancel()
	}()

	fmt.Fprintf(os.Stderr, "MCP HTTP server listening on %s\n", addr)
	if err := server.RunHTTP(ctx, addr); err != nil {
		fmt.Fprintf(os.Stderr, "MCP HTTP server error: %v\n", err)
		os.Exit(1)
	}
}

func runDaemon() {
	configPath := os.Getenv("SRVRMGR_CONFIG")
	if configPath == "" {
		configPath = defaultConfigPath
	}

	rulesDir := os.Getenv("SRVRMGR_RULES_DIR")
	if rulesDir == "" {
		rulesDir = defaultRulesDir
	}

	d := daemon.New(configPath, rulesDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nReceived shutdown signal")
		cancel()
	}()

	if err := d.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "daemon error: %v\n", err)
		os.Exit(1)
	}
}
