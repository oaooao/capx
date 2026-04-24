package runtime

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/oaooao/capx/internal/config"
)

// fakeAdapter implements the Adapter interface for testing.
type fakeAdapter struct {
	started   bool
	stopped   bool
	toolNames []string
	startErr  error
}

func (f *fakeAdapter) Start(_ context.Context) ([]server.ServerTool, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.started = true
	return nil, nil // no actual tools, but runtime tracks it
}

func (f *fakeAdapter) Stop() error {
	f.stopped = true
	return nil
}

func (f *fakeAdapter) ToolNames() []string {
	return f.toolNames
}

func newTestRuntime(caps map[string]*config.Capability, scenes map[string]*config.Scene) *Runtime {
	cfg := &config.Config{
		Capabilities: caps,
		Scenes:       scenes,
		DefaultScene: "default",
	}
	mcpServer := server.NewMCPServer("test", "0.1.0", server.WithToolCapabilities(true))
	logger := log.New(os.Stderr, "[test] ", 0)
	return New(cfg, mcpServer, logger)
}

func TestListEmpty(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{},
		map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{}}},
	)
	infos := rt.List()
	if len(infos) != 0 {
		t.Errorf("expected empty list, got %d items", len(infos))
	}
}

func TestListShowsDisabledStatus(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{
			"a": {Type: "mcp", Description: "test a"},
			"b": {Type: "mcp", Description: "test b", Disabled: true},
		},
		map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{}}},
	)

	infos := rt.List()
	// Only visible (non-disabled) capabilities appear.
	if len(infos) != 1 {
		t.Errorf("expected 1 visible, got %d", len(infos))
	}
	if infos[0].Name != "a" {
		t.Errorf("expected 'a', got %q", infos[0].Name)
	}
	if infos[0].Status != StatusDisabled {
		t.Errorf("expected disabled status (not yet enabled), got %q", infos[0].Status)
	}
}

func TestEnableDisableLifecycle(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{
			"echo": {Type: "cli", Description: "echo tool", Tools: map[string]*config.CLITool{
				"say": {Description: "say something", Args: []string{"echo", "{{msg}}"}, Params: map[string]*config.CLIParam{
					"msg": {Type: "string", Required: true},
				}},
			}},
		},
		map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{}}},
	)

	ctx := context.Background()

	// Enable.
	err := rt.Enable(ctx, "echo")
	if err != nil {
		t.Fatalf("Enable failed: %v", err)
	}

	infos := rt.List()
	found := false
	for _, info := range infos {
		if info.Name == "echo" && info.Status == StatusEnabled {
			found = true
		}
	}
	if !found {
		t.Error("echo should be enabled after Enable()")
	}

	// Enable again should be no-op.
	err = rt.Enable(ctx, "echo")
	if err != nil {
		t.Errorf("second Enable should be no-op, got: %v", err)
	}

	// Disable.
	err = rt.Disable("echo")
	if err != nil {
		t.Fatalf("Disable failed: %v", err)
	}

	infos = rt.List()
	for _, info := range infos {
		if info.Name == "echo" && info.Status == StatusEnabled {
			t.Error("echo should not be enabled after Disable()")
		}
	}

	// Disable again should error.
	err = rt.Disable("echo")
	if err == nil {
		t.Error("disabling already-disabled capability should error")
	}
}

func TestEnableUnknownCapability(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{},
		map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{}}},
	)

	err := rt.Enable(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for unknown capability")
	}
}

func TestEnableDisabledCapability(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{
			"x": {Type: "mcp", Disabled: true},
		},
		map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{}}},
	)

	err := rt.Enable(context.Background(), "x")
	if err == nil {
		t.Error("expected error for disabled capability")
	}
}

func TestEnableByScene(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{
			"a": {Type: "cli", Description: "a", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo", "a"}},
			}},
			"b": {Type: "cli", Description: "b", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo", "b"}},
			}},
		},
		map[string]*config.Scene{
			"default": {AutoEnable: config.AutoEnable{Optional: []string{"a"}}},
			"both":    {AutoEnable: config.AutoEnable{Optional: []string{"a", "b"}}},
		},
	)

	ctx := context.Background()
	if err := rt.EnableByScene(ctx, "default"); err != nil {
		t.Fatalf("EnableByScene failed: %v", err)
	}

	infos := rt.List()
	for _, info := range infos {
		if info.Name == "a" && info.Status != StatusEnabled {
			t.Error("'a' should be enabled in default scene")
		}
		if info.Name == "b" && info.Status == StatusEnabled {
			t.Error("'b' should not be enabled in default scene")
		}
	}
}

func TestEnableBySceneUnknown(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{},
		map[string]*config.Scene{},
	)

	err := rt.EnableByScene(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for unknown scene")
	}
}

func TestSetScene(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{
			"a": {Type: "cli", Description: "a", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo", "a"}},
			}},
			"b": {Type: "cli", Description: "b", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo", "b"}},
			}},
		},
		map[string]*config.Scene{
			"scene1": {AutoEnable: config.AutoEnable{Optional: []string{"a"}}},
			"scene2": {AutoEnable: config.AutoEnable{Optional: []string{"b"}}},
		},
	)

	ctx := context.Background()

	// Start with scene1.
	rt.EnableByScene(ctx, "scene1")

	// Switch to scene2.
	if _, err := rt.SetScene(ctx, "scene2"); err != nil {
		t.Fatalf("SetScene failed: %v", err)
	}

	infos := rt.List()
	for _, info := range infos {
		if info.Name == "a" && info.Status == StatusEnabled {
			t.Error("'a' should be disabled after switching to scene2")
		}
		if info.Name == "b" && info.Status != StatusEnabled {
			t.Error("'b' should be enabled after switching to scene2")
		}
	}
}

func TestShutdown(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{
			"a": {Type: "cli", Description: "a", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo", "hi"}},
			}},
		},
		map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{Optional: []string{"a"}}}},
	)

	ctx := context.Background()
	rt.EnableByScene(ctx, "default")

	rt.Shutdown()

	infos := rt.List()
	for _, info := range infos {
		if info.Status == StatusEnabled {
			t.Errorf("after Shutdown, %s should not be enabled", info.Name)
		}
	}
}

func TestEnableUnknownType(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{
			"x": {Type: "unknown_type", Description: "bad type"},
		},
		map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{}}},
	)

	err := rt.Enable(context.Background(), "x")
	if err == nil {
		t.Error("expected error for unknown capability type")
	}
}

func TestEnableFailedStatusTracking(t *testing.T) {
	// A CLI capability with no tools defined will fail to start.
	rt := newTestRuntime(
		map[string]*config.Capability{
			"broken": {Type: "cli", Command: "nonexistent", Description: "broken tool", Tools: map[string]*config.CLITool{}},
		},
		map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{}}},
	)

	err := rt.Enable(context.Background(), "broken")
	if err == nil {
		t.Error("expected error for CLI with no tools")
	}

	// Verify it shows as failed in List.
	infos := rt.List()
	if len(infos) != 1 {
		t.Fatalf("expected 1 info, got %d", len(infos))
	}
	if infos[0].Status != StatusFailed {
		t.Errorf("expected failed status, got %q", infos[0].Status)
	}
	if infos[0].Error == "" {
		t.Error("failed capability should have error message")
	}
}

func TestEnableByScenePartialFailure(t *testing.T) {
	// Scene references one good and one bad capability.
	rt := newTestRuntime(
		map[string]*config.Capability{
			"good": {Type: "cli", Description: "works", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo", "ok"}},
			}},
			"bad": {Type: "cli", Description: "broken", Tools: map[string]*config.CLITool{}},
		},
		map[string]*config.Scene{
			"mixed": {AutoEnable: config.AutoEnable{Optional: []string{"good", "bad"}}},
		},
	)

	// EnableByScene should not fail entirely — it logs warnings and continues.
	err := rt.EnableByScene(context.Background(), "mixed")
	if err != nil {
		t.Fatalf("EnableByScene should not return error on partial failure: %v", err)
	}

	infos := rt.List()
	statusMap := make(map[string]CapabilityStatus)
	for _, info := range infos {
		statusMap[info.Name] = info.Status
	}

	if statusMap["good"] != StatusEnabled {
		t.Error("'good' should be enabled despite 'bad' failing")
	}
	if statusMap["bad"] != StatusFailed {
		t.Error("'bad' should be in failed state")
	}
}

func TestGenerateStateSummary(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{
			"a": {Type: "cli", Description: "tool A", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo"}},
			}},
			"b": {Type: "mcp", Description: "tool B"},
		},
		map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{Optional: []string{"a"}}}},
	)

	ctx := context.Background()
	rt.EnableByScene(ctx, "default")

	desc := rt.GenerateStateSummary()
	if desc == "" {
		t.Error("description should not be empty")
	}
	// Should contain enabled capability.
	if !containsStr(desc, "tool A") {
		t.Error("description should mention enabled tool A")
	}
	// Should contain available capability.
	if !containsStr(desc, "tool B") {
		t.Error("description should mention available tool B")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && searchStr(s, sub)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
