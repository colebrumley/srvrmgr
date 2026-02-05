// internal/mcp/server.go
package mcp

import (
	"context"
	"fmt"

	"github.com/colebrumley/srvrmgr/internal/memory"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wraps the MCP server with memory tools
type Server struct {
	db     *memory.DB
	server *mcp.Server
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
	Query    string `json:"query" jsonschema:"Search terms (full-text search)"`
	Category string `json:"category,omitempty" jsonschema:"Optional category filter"`
}

// RecallOutput is the output schema for the recall tool
type RecallOutput struct {
	Memories []MemoryResult `json:"memories"`
	Count    int            `json:"count"`
}

// MemoryResult is a single memory in recall results
type MemoryResult struct {
	ID       int64  `json:"id"`
	Content  string `json:"content"`
	Category string `json:"category,omitempty"`
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

	s := &Server{db: db}

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
	id, err := s.db.Remember(input.Content, input.Category, "")
	if err != nil {
		return nil, RememberOutput{}, fmt.Errorf("failed to store memory: %w", err)
	}
	return nil, RememberOutput{
		ID:      id,
		Message: fmt.Sprintf("Stored memory with ID %d", id),
	}, nil
}

func (s *Server) handleRecall(ctx context.Context, req *mcp.CallToolRequest, input RecallInput) (*mcp.CallToolResult, RecallOutput, error) {
	memories, err := s.db.Recall(input.Query, input.Category)
	if err != nil {
		return nil, RecallOutput{}, fmt.Errorf("failed to search memories: %w", err)
	}

	results := make([]MemoryResult, len(memories))
	for i, m := range memories {
		results[i] = MemoryResult{
			ID:       m.ID,
			Content:  m.Content,
			Category: m.Category,
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

// Close closes the database connection
func (s *Server) Close() error {
	return s.db.Close()
}
