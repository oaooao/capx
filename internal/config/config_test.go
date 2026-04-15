package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	yaml := `
capabilities:
  context7:
    type: mcp
    transport: http
    url: https://mcp.context7.com/mcp
    description: "docs"
    tags: [docs]
  xcode:
    type: mcp
    transport: stdio
    command: xcodebuildmcp
    args: ["mcp"]
    env:
      FOO: bar
    description: "xcode"
    disabled: true
  webx:
    type: cli
    command: webx
    description: "web"
    tools:
      read:
        description: "read url"
        args: ["read", "{{url}}"]
        params:
          url: { type: string, required: true }
scenes:
  default:
    auto_enable: [context7, webx]
  full:
    auto_enable: [all]
default_scene: default
`
	path := writeTempFile(t, "config.yaml", yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Capabilities) != 3 {
		t.Errorf("expected 3 capabilities, got %d", len(cfg.Capabilities))
	}
	if cfg.DefaultScene != "default" {
		t.Errorf("expected default scene 'default', got %q", cfg.DefaultScene)
	}

	// Check MCP capability.
	ctx7 := cfg.Capabilities["context7"]
	if ctx7.Type != "mcp" || ctx7.Transport != "http" || ctx7.URL != "https://mcp.context7.com/mcp" {
		t.Errorf("context7 parsed incorrectly: %+v", ctx7)
	}

	// Check disabled capability.
	xcode := cfg.Capabilities["xcode"]
	if !xcode.Disabled {
		t.Error("xcode should be disabled")
	}
	if xcode.Env["FOO"] != "bar" {
		t.Errorf("xcode env not parsed: %v", xcode.Env)
	}

	// Check CLI capability with tools.
	webx := cfg.Capabilities["webx"]
	if webx.Type != "cli" {
		t.Errorf("webx type should be cli, got %q", webx.Type)
	}
	if len(webx.Tools) != 1 {
		t.Errorf("webx should have 1 tool, got %d", len(webx.Tools))
	}
	readTool := webx.Tools["read"]
	if readTool == nil {
		t.Fatal("webx.tools.read is nil")
	}
	if !readTool.Params["url"].Required {
		t.Error("webx read url param should be required")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	path := writeTempFile(t, "bad.yaml", "not: [valid: yaml: {{")
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadDefaults(t *testing.T) {
	path := writeTempFile(t, "minimal.yaml", "capabilities: {}")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.DefaultScene != "default" {
		t.Errorf("expected default scene 'default', got %q", cfg.DefaultScene)
	}
	if cfg.Capabilities == nil {
		t.Error("Capabilities map should be initialized")
	}
	if cfg.Scenes == nil {
		t.Error("Scenes map should be initialized")
	}
}

func TestSaveAndReload(t *testing.T) {
	cfg := &Config{
		Capabilities: map[string]*Capability{
			"test": {Type: "mcp", Transport: "http", URL: "https://example.com", Description: "test"},
		},
		Scenes:       map[string]*Scene{"default": {AutoEnable: []string{"test"}}},
		DefaultScene: "default",
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after save failed: %v", err)
	}

	if loaded.Capabilities["test"].URL != "https://example.com" {
		t.Error("round-trip failed: URL mismatch")
	}
	if loaded.DefaultScene != "default" {
		t.Error("round-trip failed: default_scene mismatch")
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "config.yaml")

	cfg := &Config{
		Capabilities: map[string]*Capability{},
		Scenes:       map[string]*Scene{},
		DefaultScene: "default",
	}

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save should create nested dirs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("file should exist after save")
	}
}

func TestVisibleCapabilities(t *testing.T) {
	cfg := &Config{
		Capabilities: map[string]*Capability{
			"a": {Type: "mcp", Disabled: false},
			"b": {Type: "mcp", Disabled: true},
			"c": {Type: "cli", Disabled: false},
		},
	}

	visible := cfg.VisibleCapabilities()
	if len(visible) != 2 {
		t.Errorf("expected 2 visible, got %d", len(visible))
	}
	if _, ok := visible["b"]; ok {
		t.Error("disabled capability 'b' should not be visible")
	}
	if _, ok := visible["a"]; !ok {
		t.Error("capability 'a' should be visible")
	}
	if _, ok := visible["c"]; !ok {
		t.Error("capability 'c' should be visible")
	}
}

func TestResolveScene(t *testing.T) {
	cfg := &Config{
		Capabilities: map[string]*Capability{
			"a": {Type: "mcp"},
			"b": {Type: "mcp"},
			"c": {Type: "mcp", Disabled: true},
		},
		Scenes: map[string]*Scene{
			"default": {AutoEnable: []string{"a"}},
			"full":    {AutoEnable: []string{"all"}},
		},
	}

	// Normal scene.
	names, err := cfg.ResolveScene("default")
	if err != nil {
		t.Fatalf("ResolveScene failed: %v", err)
	}
	if len(names) != 1 || names[0] != "a" {
		t.Errorf("expected [a], got %v", names)
	}

	// "all" scene should return all non-disabled.
	names, err = cfg.ResolveScene("full")
	if err != nil {
		t.Fatalf("ResolveScene full failed: %v", err)
	}
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["a"] || !nameSet["b"] {
		t.Errorf("full scene should contain a and b, got %v", names)
	}
	if nameSet["c"] {
		t.Error("full scene should not contain disabled capability c")
	}

	// Unknown scene.
	_, err = cfg.ResolveScene("nonexistent")
	if err == nil {
		t.Error("expected error for unknown scene")
	}
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
