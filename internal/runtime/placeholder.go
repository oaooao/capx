package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

func placeholderToolName(capName string) string {
	return strings.NewReplacer("/", "_", "-", "_").Replace(capName)
}

func (r *Runtime) registerPlaceholder(name string) {
	cap, ok := r.cfg.Capabilities[name]
	if !ok || cap.Disabled {
		return
	}

	toolName := placeholderToolName(name)
	desc := cap.Description
	if desc == "" {
		desc = fmt.Sprintf("Capability: %s (inactive)", name)
	}

	tool := mcp.NewTool(toolName,
		mcp.WithDescription(desc),
		mcp.WithString("action",
			mcp.Required(),
			mcp.Description("describe: view metadata without activating. enable: activate and register tools."),
			mcp.Enum("describe", "enable"),
		),
	)

	capName := name
	r.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action, _ := req.GetArguments()["action"].(string)
		switch action {
		case "describe":
			detail, err := r.cfg.Describe(capName, "")
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("error: %v", err))},
					IsError: true,
				}, nil
			}
			payload, _ := json.MarshalIndent(detail, "", "  ")
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(string(payload))},
			}, nil
		case "enable":
			if err := r.Enable(ctx, capName); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("error: %v", err))},
					IsError: true,
				}, nil
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("✓ Enabled %s\n\n%s", capName, r.GenerateStateSummary()))},
			}, nil
		default:
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent("error: action must be 'describe' or 'enable'")},
				IsError: true,
			}, nil
		}
	})

	r.mu.Lock()
	r.placeholders[name] = toolName
	r.mu.Unlock()
}

func (r *Runtime) removePlaceholder(name string) {
	r.mu.Lock()
	toolName, ok := r.placeholders[name]
	if ok {
		delete(r.placeholders, name)
	}
	r.mu.Unlock()

	if ok {
		r.mcpServer.DeleteTools(toolName)
	}
}

// removePlaceholderLocked removes a placeholder while r.mu is already held.
func (r *Runtime) removePlaceholderLocked(name string) {
	toolName, ok := r.placeholders[name]
	if ok {
		delete(r.placeholders, name)
		r.mcpServer.DeleteTools(toolName)
	}
}

// RegisterPlaceholders registers placeholder tools for all declared-but-inactive,
// non-disabled capabilities. Called once after the initial scene enable.
func (r *Runtime) RegisterPlaceholders() {
	r.mu.RLock()
	activeNames := make(map[string]bool, len(r.active))
	for name := range r.active {
		activeNames[name] = true
	}
	r.mu.RUnlock()

	for name, cap := range r.cfg.Capabilities {
		if cap.Disabled || activeNames[name] {
			continue
		}
		r.registerPlaceholder(name)
	}
}
