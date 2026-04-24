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

	mcpServer := server.NewMCPServer(
		"capx",
		Version,
		server.WithToolCapabilities(true),
	)

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

		// Return updated description showing new state.
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("✓ Enabled %s\n\n%s", name, rt.GenerateDescription()))},
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
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("✓ Disabled %s\n\n%s", name, rt.GenerateDescription()))},
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
