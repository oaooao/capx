package runtime

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oaooao/capx/internal/config"
)

// MCPAdapter manages a backend MCP server connection.
type MCPAdapter struct {
	name   string
	cap    *config.Capability
	logger *log.Logger

	mu        sync.Mutex
	mcpClient client.MCPClient
	toolNames []string
}

// NewMCPAdapter creates a new MCP adapter.
func NewMCPAdapter(name string, cap *config.Capability, logger *log.Logger) *MCPAdapter {
	return &MCPAdapter{
		name:   name,
		cap:    cap,
		logger: logger,
	}
}

// Start connects to the backend MCP server and retrieves its tools.
func (a *MCPAdapter) Start(ctx context.Context) ([]server.ServerTool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var (
		mcpClient client.MCPClient
		err       error
	)

	switch a.cap.Transport {
	case "stdio":
		mcpClient, err = a.startStdio()
	case "http":
		mcpClient, err = a.startHTTP()
	default:
		return nil, fmt.Errorf("unknown MCP transport %q", a.cap.Transport)
	}
	if err != nil {
		return nil, err
	}

	// Initialize the backend server.
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "capx",
		Version: "1.0.0",
	}
	initReq.Params.Capabilities = mcp.ClientCapabilities{}

	if _, err := mcpClient.Initialize(ctx, initReq); err != nil {
		mcpClient.Close()
		return nil, fmt.Errorf("initializing backend %s: %w", a.name, err)
	}

	// List tools from the backend.
	listReq := mcp.ListToolsRequest{}
	toolsResult, err := mcpClient.ListTools(ctx, listReq)
	if err != nil {
		mcpClient.Close()
		return nil, fmt.Errorf("listing tools from %s: %w", a.name, err)
	}

	a.mcpClient = mcpClient

	// Build proxied tools with prefixed names.
	var serverTools []server.ServerTool
	for _, tool := range toolsResult.Tools {
		proxiedName := a.prefixedName(tool.Name)
		a.toolNames = append(a.toolNames, proxiedName)

		// Capture for closure.
		backendToolName := tool.Name
		backendClient := mcpClient

		proxiedTool := tool
		proxiedTool.Name = proxiedName
		if proxiedTool.Description != "" {
			proxiedTool.Description = fmt.Sprintf("[%s] %s", a.name, proxiedTool.Description)
		}

		handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Forward the call to the backend with the original tool name.
			fwdReq := mcp.CallToolRequest{}
			fwdReq.Params.Name = backendToolName
			fwdReq.Params.Arguments = req.Params.Arguments
			return backendClient.CallTool(ctx, fwdReq)
		}

		serverTools = append(serverTools, server.ServerTool{
			Tool:    proxiedTool,
			Handler: handler,
		})
	}

	a.logger.Printf("connected to %s: %d tools", a.name, len(serverTools))
	return serverTools, nil
}

func (a *MCPAdapter) startStdio() (client.MCPClient, error) {
	var env []string
	// Inherit current environment.
	env = append(env, os.Environ()...)
	// Add capability-specific env vars.
	for k, v := range a.cap.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	c, err := client.NewStdioMCPClient(a.cap.Command, env, a.cap.Args...)
	if err != nil {
		return nil, fmt.Errorf("spawning %s: %w", a.name, err)
	}
	return c, nil
}

func (a *MCPAdapter) startHTTP() (client.MCPClient, error) {
	c, err := client.NewStreamableHttpClient(a.cap.URL)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s at %s: %w", a.name, a.cap.URL, err)
	}
	return c, nil
}

// Stop terminates the backend connection.
func (a *MCPAdapter) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.mcpClient != nil {
		err := a.mcpClient.Close()
		a.mcpClient = nil
		return err
	}
	return nil
}

// ToolNames returns the prefixed tool names registered by this adapter.
func (a *MCPAdapter) ToolNames() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.toolNames
}

func (a *MCPAdapter) prefixedName(toolName string) string {
	// Replace / and - with _ for safe tool names.
	safe := strings.NewReplacer("/", "_", "-", "_").Replace(a.name)
	return safe + "__" + toolName
}
