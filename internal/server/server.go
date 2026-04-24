package capxserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oaooao/capx/internal/config"
	"github.com/oaooao/capx/internal/runtime"
)

const Version = "1.0.0"

// Serve starts the capx MCP server on stdio.
func Serve(cfg *config.Config, scene string) error {
	logger := log.New(log.Writer(), "[capx] ", log.LstdFlags)

	mcpServer := newCapxMCPServer()

	rt := runtime.New(cfg, mcpServer, logger)

	// Register management tools.
	registerManagementTools(mcpServer, rt, cfg)

	// Enable initial scene.
	ctx := context.Background()
	if err := rt.EnableByScene(ctx, scene); err != nil {
		logger.Printf("warning: failed to enable scene %s: %v", scene, err)
	}

	// Run stdio server. Shutdown on exit.
	defer rt.Shutdown()
	return server.ServeStdio(mcpServer)
}

func newCapxMCPServer() *server.MCPServer {
	return server.NewMCPServer(
		"capx",
		Version,
		server.WithToolCapabilities(true),
		server.WithInstructions(BuildInstructions()),
	)
}

func BuildInstructions() string {
	return strings.TrimSpace(`
capx is an Agent Capability Runtime. It manages MCP servers and CLI tools behind one MCP connection.

At the start of a session, call scene_info to inspect the current workbench: active scene, ready capabilities, failed capabilities, and degraded state.

Scenes shape the agent's default tool choices; they are not hard filters. If a task needs a capability that is not currently ready, use search to discover candidates, describe to inspect one, then enable it for this session. Use set_scene when the whole task context changes.

If the user asks to use, call, invoke, run, or launch a named MCP, CLI, command, tool, or capability, and that named capability is not already visible in the current tool set or you are unsure, proactively search capx for it before giving up or substituting another tool.

set_scene returns ok, rejected, or partial_failure. Always inspect failed[] and degradation fields before assuming the switch succeeded.
`)
}

// stringArg safely extracts a string-typed tool argument, returning "" for
// missing or non-string values.
func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func registerManagementTools(s *server.MCPServer, rt *runtime.Runtime, cfg *config.Config) {
	// list
	listTool := mcp.NewTool("list",
		mcp.WithDescription("List all available capabilities and their status"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(listTool, func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		infos := rt.List()
		data, _ := json.MarshalIndent(infos, "", "  ")
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(string(data))},
		}, nil
	})

	// enable
	enableTool := mcp.NewTool("enable",
		mcp.WithDescription("Enable a capability by name"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Name of the capability to enable")),
	)
	s.AddTool(enableTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, _ := req.GetArguments()["name"].(string)
		if name == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent("error: 'name' argument is required")},
				IsError: true,
			}, nil
		}

		if err := rt.Enable(ctx, name); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("error: %v", err))},
				IsError: true,
			}, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("✓ Enabled %s\n\n%s", name, rt.GenerateStateSummary()))},
		}, nil
	})

	// disable
	disableTool := mcp.NewTool("disable",
		mcp.WithDescription("Disable a capability by name"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Name of the capability to disable")),
	)
	s.AddTool(disableTool, func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, _ := req.GetArguments()["name"].(string)
		if name == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent("error: 'name' argument is required")},
				IsError: true,
			}, nil
		}

		if err := rt.Disable(name); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("error: %v", err))},
				IsError: true,
			}, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("✓ Disabled %s\n\n%s", name, rt.GenerateStateSummary()))},
		}, nil
	})

	// set_scene
	sceneNames := make([]string, 0, len(cfg.Scenes))
	for name := range cfg.Scenes {
		sceneNames = append(sceneNames, name)
	}

	setSceneTool := mcp.NewTool("set_scene",
		mcp.WithDescription("Switch to a different capability scene. Available scenes: "+strings.Join(sceneNames, ", ")),
		mcp.WithString("scene", mcp.Required(), mcp.Description("Name of the scene to switch to"), mcp.Enum(sceneNames...)),
	)
	s.AddTool(setSceneTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sceneName, _ := req.GetArguments()["scene"].(string)
		if sceneName == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent("error: 'scene' argument is required")},
				IsError: true,
			}, nil
		}

		// SetScene always returns a structured result; err is non-nil only on
		// StatusRejected, in which case the result carries the diagnostic.
		result, _ := rt.SetScene(ctx, sceneName)
		payload, mErr := json.MarshalIndent(result, "", "  ")
		if mErr != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("error encoding result: %v", mErr))},
				IsError: true,
			}, nil
		}
		isError := result.Status == runtime.StatusRejected
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(string(payload))},
			IsError: isError,
		}, nil
	})

	// search — Level-1 capability discovery (§A.10).
	searchTool := mcp.NewTool("search",
		mcp.WithDescription(
			"Level-1 capx capability search. Returns {name, type, summary} for matching caps. "+
				"All filter fields are optional; an empty call returns every visible cap "+
				"(useful as a dictionary source for prompt-easy/typefree).",
		),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("query", mcp.Description("Substring match over name, description, aliases, keywords")),
		mcp.WithString("type", mcp.Description("Filter to 'mcp' or 'cli'"), mcp.Enum("mcp", "cli")),
		mcp.WithString("tag", mcp.Description("Filter to caps with this tag")),
		mcp.WithString("scene", mcp.Description("Restrict to caps visible in this scene")),
	)
	s.AddTool(searchTool, func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		query := config.SearchQuery{
			Query: stringArg(args, "query"),
			Type:  stringArg(args, "type"),
			Tag:   stringArg(args, "tag"),
			Scene: stringArg(args, "scene"),
		}
		results, err := cfg.Search(query)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("error: %v", err))},
				IsError: true,
			}, nil
		}
		payload, _ := json.MarshalIndent(results, "", "  ")
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(string(payload))},
		}, nil
	})

	// describe — Level-2 capability detail (§A.10).
	describeTool := mcp.NewTool("describe",
		mcp.WithDescription(
			"Level-2 capx capability detail. Returns full metadata for one cap, "+
				"optionally resolved against a specific scene to disambiguate inline overrides.",
		),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("name", mcp.Required(), mcp.Description("Capability name")),
		mcp.WithString("scene", mcp.Description("Optional scene context (resolves inline overrides)")),
	)
	s.AddTool(describeTool, func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		name := stringArg(args, "name")
		if name == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent("error: 'name' argument is required")},
				IsError: true,
			}, nil
		}
		detail, err := cfg.Describe(name, stringArg(args, "scene"))
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
	})

	// scene_info — cross-agent portable scene summary (§A.11).
	// Agents are encouraged to call this once at session start to understand
	// the current workbench, and again after any set_scene or enable/disable
	// to re-read the ready/failed classification.
	sceneInfoTool := mcp.NewTool("scene_info",
		mcp.WithDescription(
			"Return structured metadata for the currently active capx scene: "+
				"description, ready/failed capability lists, degradation status, "+
				"and a summary of the last scene switch. Recommended to call once "+
				"at session start to understand the available workbench.",
		),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(sceneInfoTool, func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		info := rt.SceneInfo()
		payload, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("error encoding scene_info: %v", err))},
				IsError: true,
			}, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(string(payload))},
		}, nil
	})
}
