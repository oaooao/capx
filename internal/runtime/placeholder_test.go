package runtime

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oaooao/capx/internal/config"
)

type fakeAdapterWithTools struct {
	fakeAdapter
	name string
}

func (f *fakeAdapterWithTools) Start(_ context.Context) ([]server.ServerTool, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.started = true
	toolName := f.name + "_tool"
	f.toolNames = []string{toolName}
	return []server.ServerTool{
		{Tool: mcp.Tool{Name: toolName}},
	}, nil
}

func (f *fakeAdapterWithTools) ToolNames() []string {
	return f.toolNames
}

func TestPlaceholderToolName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"context7", "context7"},
		{"xcodebuild-mcp", "xcodebuild_mcp"},
		{"some/nested", "some_nested"},
		{"a-b/c-d", "a_b_c_d"},
	}
	for _, tt := range tests {
		got := placeholderToolName(tt.input)
		if got != tt.want {
			t.Errorf("placeholderToolName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRegisterPlaceholders(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{
			"active": {Type: "cli", Description: "already active", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo", "hi"}},
			}},
			"inactive":  {Type: "mcp", Description: "not yet active"},
			"off":       {Type: "mcp", Description: "turned off", Disabled: true},
			"inactive2": {Type: "cli", Description: "also inactive"},
		},
		map[string]*config.Scene{
			"default": {AutoEnable: config.AutoEnable{Optional: []string{"active"}}},
		},
	)

	rt.adapterFactoryOverride = func(name string, cap *config.Capability) (Adapter, error) {
		return &fakeAdapter{}, nil
	}

	ctx := context.Background()
	_ = rt.Enable(ctx, "active")

	rt.RegisterPlaceholders()

	rt.mu.RLock()
	_, hasInactive := rt.placeholders["inactive"]
	_, hasInactive2 := rt.placeholders["inactive2"]
	_, hasActive := rt.placeholders["active"]
	_, hasOff := rt.placeholders["off"]
	rt.mu.RUnlock()

	if !hasInactive {
		t.Error("inactive capability should have a placeholder")
	}
	if !hasInactive2 {
		t.Error("inactive2 capability should have a placeholder")
	}
	if hasActive {
		t.Error("active capability should NOT have a placeholder")
	}
	if hasOff {
		t.Error("disabled capability should NOT have a placeholder")
	}
}

func TestPlaceholderRemovedOnEnable(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{
			"cap1": {Type: "cli", Description: "test cap", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo", "hi"}},
			}},
		},
		map[string]*config.Scene{
			"default": {AutoEnable: config.AutoEnable{}},
		},
	)
	rt.adapterFactoryOverride = func(name string, cap *config.Capability) (Adapter, error) {
		return &fakeAdapter{}, nil
	}

	rt.RegisterPlaceholders()

	rt.mu.RLock()
	_, has := rt.placeholders["cap1"]
	rt.mu.RUnlock()
	if !has {
		t.Fatal("cap1 should have placeholder before enable")
	}

	ctx := context.Background()
	if err := rt.Enable(ctx, "cap1"); err != nil {
		t.Fatalf("Enable failed: %v", err)
	}

	rt.mu.RLock()
	_, has = rt.placeholders["cap1"]
	rt.mu.RUnlock()
	if has {
		t.Error("cap1 placeholder should be removed after enable")
	}
}

func TestPlaceholderRestoredOnDisable(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{
			"cap1": {Type: "cli", Description: "test cap", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo", "hi"}},
			}},
		},
		map[string]*config.Scene{
			"default": {AutoEnable: config.AutoEnable{}},
		},
	)
	rt.adapterFactoryOverride = func(name string, cap *config.Capability) (Adapter, error) {
		return &fakeAdapter{}, nil
	}

	ctx := context.Background()
	_ = rt.Enable(ctx, "cap1")

	rt.mu.RLock()
	_, has := rt.placeholders["cap1"]
	rt.mu.RUnlock()
	if has {
		t.Fatal("should not have placeholder while enabled")
	}

	_ = rt.Disable("cap1")

	rt.mu.RLock()
	_, has = rt.placeholders["cap1"]
	rt.mu.RUnlock()
	if !has {
		t.Error("placeholder should be restored after disable")
	}
}

func TestPlaceholderSetSceneLifecycle(t *testing.T) {
	rt := newTestRuntime(
		map[string]*config.Capability{
			"shared": {Type: "cli", Description: "shared cap", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo", "shared"}},
			}},
			"scene1only": {Type: "cli", Description: "only in scene1", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo", "s1"}},
			}},
			"scene2only": {Type: "cli", Description: "only in scene2", Tools: map[string]*config.CLITool{
				"run": {Description: "run", Args: []string{"echo", "s2"}},
			}},
		},
		map[string]*config.Scene{
			"scene1": {AutoEnable: config.AutoEnable{Optional: []string{"shared", "scene1only"}}},
			"scene2": {AutoEnable: config.AutoEnable{Optional: []string{"shared", "scene2only"}}},
		},
	)
	rt.adapterFactoryOverride = func(name string, cap *config.Capability) (Adapter, error) {
		return &fakeAdapterWithTools{name: name}, nil
	}
	rt.cfg.DefaultScene = "scene1"

	ctx := context.Background()
	_, _ = rt.SetScene(ctx, "scene1")
	rt.RegisterPlaceholders()

	rt.mu.RLock()
	_, hasScene2Only := rt.placeholders["scene2only"]
	_, hasScene1Only := rt.placeholders["scene1only"]
	_, hasShared := rt.placeholders["shared"]
	rt.mu.RUnlock()

	if !hasScene2Only {
		t.Error("scene2only should have placeholder in scene1")
	}
	if hasScene1Only {
		t.Error("scene1only should NOT have placeholder (it's active)")
	}
	if hasShared {
		t.Error("shared should NOT have placeholder (it's active)")
	}

	// Switch to scene2: scene1only should get a placeholder, scene2only should lose its.
	_, _ = rt.SetScene(ctx, "scene2")

	rt.mu.RLock()
	_, hasScene1Only = rt.placeholders["scene1only"]
	_, hasScene2Only = rt.placeholders["scene2only"]
	rt.mu.RUnlock()

	if !hasScene1Only {
		t.Error("scene1only should have placeholder after switching to scene2")
	}
	if hasScene2Only {
		t.Error("scene2only should NOT have placeholder after switching to scene2")
	}
}
