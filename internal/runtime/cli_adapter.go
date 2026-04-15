package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oaooao/capx/internal/config"
)

// CLIAdapter wraps a CLI tool as MCP tools.
type CLIAdapter struct {
	name      string
	cap       *config.Capability
	toolNames []string
}

// NewCLIAdapter creates a new CLI adapter.
func NewCLIAdapter(name string, cap *config.Capability) *CLIAdapter {
	return &CLIAdapter{
		name: name,
		cap:  cap,
	}
}

// Start builds MCP tool definitions from the CLI tool config.
func (a *CLIAdapter) Start(_ context.Context) ([]server.ServerTool, error) {
	if len(a.cap.Tools) == 0 {
		return nil, fmt.Errorf("CLI capability %s has no tools defined", a.name)
	}

	var serverTools []server.ServerTool

	for toolName, toolDef := range a.cap.Tools {
		prefixed := a.prefixedName(toolName)
		a.toolNames = append(a.toolNames, prefixed)

		// Build the MCP tool schema from params.
		opts := []mcp.ToolOption{
			mcp.WithDescription(fmt.Sprintf("[%s] %s", a.name, toolDef.Description)),
		}

		for paramName, param := range toolDef.Params {
			var propOpts []mcp.PropertyOption
			if param.Description != "" {
				propOpts = append(propOpts, mcp.Description(param.Description))
			}
			if len(param.Enum) > 0 {
				propOpts = append(propOpts, mcp.Enum(param.Enum...))
			}
			if param.Required {
				propOpts = append(propOpts, mcp.Required())
			}

			switch param.Type {
			case "string":
				opts = append(opts, mcp.WithString(paramName, propOpts...))
			case "number":
				opts = append(opts, mcp.WithNumber(paramName, propOpts...))
			case "boolean":
				opts = append(opts, mcp.WithBoolean(paramName, propOpts...))
			}
		}

		tool := mcp.NewTool(prefixed, opts...)

		// Capture for closure.
		command := a.cap.Command
		argTemplate := toolDef.Args
		envVars := a.cap.Env

		handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := templateArgs(argTemplate, req.GetArguments())

			cmd := exec.CommandContext(ctx, command, args...)
			// Inherit system environment, then overlay capability-specific vars.
			cmd.Env = os.Environ()
			for k, v := range envVars {
				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
			}

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if err != nil {
				errMsg := stderr.String()
				if errMsg == "" {
					errMsg = err.Error()
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{mcp.NewTextContent(errMsg)},
					IsError: true,
				}, nil
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(stdout.String())},
			}, nil
		}

		serverTools = append(serverTools, server.ServerTool{
			Tool:    tool,
			Handler: handler,
		})
	}

	return serverTools, nil
}

// Stop is a no-op for CLI adapters (no persistent process).
func (a *CLIAdapter) Stop() error {
	return nil
}

// ToolNames returns registered tool names.
func (a *CLIAdapter) ToolNames() []string {
	return a.toolNames
}

func (a *CLIAdapter) prefixedName(toolName string) string {
	safe := strings.NewReplacer("/", "_", "-", "_").Replace(a.name)
	return safe + "_" + toolName
}

// templateArgs replaces {{param}} placeholders in args with actual values.
func templateArgs(template []string, params map[string]any) []string {
	result := make([]string, 0, len(template))
	for _, arg := range template {
		replaced := arg
		for key, val := range params {
			placeholder := "{{" + key + "}}"
			replaced = strings.ReplaceAll(replaced, placeholder, fmt.Sprintf("%v", val))
		}
		// Skip args that still contain unreplaced placeholders for optional params.
		if !strings.Contains(replaced, "{{") {
			result = append(result, replaced)
		}
	}
	return result
}
