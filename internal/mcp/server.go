// internal/mcp/server.go
package mcp

import (
	"context"
	"fmt"
	"net/http"

	"github.com/colebrumley/srvrmgr/internal/embedder"
	"github.com/colebrumley/srvrmgr/internal/memory"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wraps the MCP server with memory tools
type Server struct {
	db       *memory.DB
	embedder *embedder.Embedder
	server   *mcp.Server
}

// RememberInput is the input schema for the remember tool
type RememberInput struct {
	Content  string `json:"content" jsonschema:"The knowledge to store"`
	Category string `json:"category,omitempty" jsonschema:"Optional category: file-patterns, api-behaviors, system-quirks, naming-conventions"`
}

// RememberOutput is the output schema for the remember tool
type RememberOutput struct {
	ID      int64  `json:"id"`
	Message string `json:"message"`
}

// RecallInput is the input schema for the recall tool
type RecallInput struct {
	Query    string `json:"query" jsonschema:"Search terms"`
	Category string `json:"category,omitempty" jsonschema:"Optional category filter"`
	Mode     string `json:"mode,omitempty" jsonschema:"Search mode: semantic (default) or keyword"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max results (default 10)"`
}

// RecallOutput is the output schema for the recall tool
type RecallOutput struct {
	Memories []MemoryResult `json:"memories"`
	Count    int            `json:"count"`
}

// MemoryResult is a single memory in recall results
type MemoryResult struct {
	ID       int64   `json:"id"`
	Content  string  `json:"content"`
	Category string  `json:"category,omitempty"`
	Score    float32 `json:"score,omitempty"`
}

// ForgetInput is the input schema for the forget tool
type ForgetInput struct {
	ID int64 `json:"id" jsonschema:"Memory ID to remove (from recall results)"`
}

// ForgetOutput is the output schema for the forget tool
type ForgetOutput struct {
	Message string `json:"message"`
}

// NewServer creates a new MCP server with memory tools
func NewServer(dbPath string) (*Server, error) {
	db, err := memory.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening memory database: %w", err)
	}

	emb, err := embedder.New()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("creating embedder: %w", err)
	}

	s := &Server{db: db, embedder: emb}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "srvrmgr-memory",
		Version: "1.0.0",
	}, nil)

	// Register remember tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "remember",
		Description: "Store domain knowledge you've learned that would help future rule executions. Use for: file patterns/conventions, API behaviors, system quirks, naming conventions. Be specific and factual. Don't store: execution logs, temporary state, or procedural instructions.",
	}, s.handleRemember)

	// Register recall tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recall",
		Description: "Search stored memories for relevant domain knowledge. Use before making assumptions about files, APIs, or system behaviors you've encountered before.",
	}, s.handleRecall)

	// Register forget tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "forget",
		Description: "Remove a memory that's no longer accurate or relevant. Use when you learn something that contradicts a stored memory.",
	}, s.handleForget)

	s.server = server
	return s, nil
}

func (s *Server) handleRemember(ctx context.Context, req *mcp.CallToolRequest, input RememberInput) (*mcp.CallToolResult, RememberOutput, error) {
	// Generate embedding
	embedding, err := s.embedder.Embed(input.Content)
	if err != nil {
		// Log warning but continue without embedding
		embedding = nil
	}

	id, err := s.db.RememberWithEmbedding(input.Content, input.Category, "", embedding)
	if err != nil {
		return nil, RememberOutput{}, fmt.Errorf("failed to store memory: %w", err)
	}
	return nil, RememberOutput{
		ID:      id,
		Message: fmt.Sprintf("Stored memory with ID %d", id),
	}, nil
}

func (s *Server) handleRecall(ctx context.Context, req *mcp.CallToolRequest, input RecallInput) (*mcp.CallToolResult, RecallOutput, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}

	mode := input.Mode
	if mode == "" {
		mode = "semantic"
	}

	results := []MemoryResult{}

	if mode == "keyword" {
		// Use FTS5 keyword search
		memories, err := s.db.Recall(input.Query, input.Category)
		if err != nil {
			return nil, RecallOutput{}, fmt.Errorf("failed to search memories: %w", err)
		}
		for _, m := range memories {
			results = append(results, MemoryResult{
				ID:       m.ID,
				Content:  m.Content,
				Category: m.Category,
			})
		}
	} else {
		// Use semantic search
		queryEmbedding, err := s.embedder.Embed(input.Query)
		if err != nil {
			return nil, RecallOutput{}, fmt.Errorf("failed to embed query: %w", err)
		}

		memories, err := s.db.RecallSemantic(queryEmbedding, input.Category, limit)
		if err != nil {
			return nil, RecallOutput{}, fmt.Errorf("failed to search memories: %w", err)
		}
		for _, m := range memories {
			results = append(results, MemoryResult{
				ID:       m.ID,
				Content:  m.Content,
				Category: m.Category,
				Score:    m.Score,
			})
		}
	}

	return nil, RecallOutput{
		Memories: results,
		Count:    len(results),
	}, nil
}

func (s *Server) handleForget(ctx context.Context, req *mcp.CallToolRequest, input ForgetInput) (*mcp.CallToolResult, ForgetOutput, error) {
	err := s.db.Forget(input.ID)
	if err != nil {
		if err == memory.ErrNotFound {
			return nil, ForgetOutput{}, fmt.Errorf("memory with ID %d not found", input.ID)
		}
		return nil, ForgetOutput{}, fmt.Errorf("failed to delete memory: %w", err)
	}
	return nil, ForgetOutput{
		Message: fmt.Sprintf("Deleted memory with ID %d", input.ID),
	}, nil
}

// Run starts the MCP server on stdio
func (s *Server) Run(ctx context.Context) error {
	return s.server.Run(ctx, &mcp.StdioTransport{})
}

// RunHTTP starts the MCP server as an HTTP server on the given address
// Uses SSE transport with endpoint at /sse for compatibility with Claude Code
func (s *Server) RunHTTP(ctx context.Context, addr string) error {
	sseHandler := mcp.NewSSEHandler(func(r *http.Request) *mcp.Server {
		return s.server
	}, nil)

	mux := http.NewServeMux()
	// Serve SSE at both root and /sse path for compatibility
	mux.Handle("/", sseHandler)
	mux.Handle("/sse", sseHandler)

	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Shutdown gracefully on context cancellation
	go func() {
		<-ctx.Done()
		httpServer.Shutdown(context.Background())
	}()

	err := httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// MCPServer returns the underlying MCP server for direct use
func (s *Server) MCPServer() *mcp.Server {
	return s.server
}

// Close closes the database connection and embedder
func (s *Server) Close() error {
	if s.embedder != nil {
		s.embedder.Close()
	}
	return s.db.Close()
}
