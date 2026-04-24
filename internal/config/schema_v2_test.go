package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestAutoEnable_FlatList verifies YAML list form: [a, b, c] → all optional.
func TestAutoEnable_FlatList(t *testing.T) {
	yaml := `
capabilities: {}
scenes:
  web:
    auto_enable: [playwright, context7, webx]
`
	cfg := loadInline(t, yaml)
	ae := cfg.Scenes["web"].AutoEnable
	if len(ae.Required) != 0 {
		t.Errorf("flat list should have no required, got %v", ae.Required)
	}
	want := []string{"playwright", "context7", "webx"}
	if !reflect.DeepEqual(ae.Optional, want) {
		t.Errorf("Optional mismatch: got %v, want %v", ae.Optional, want)
	}
}

// TestAutoEnable_SplitForm verifies YAML mapping form: {required:[...], optional:[...]}.
func TestAutoEnable_SplitForm(t *testing.T) {
	yaml := `
capabilities: {}
scenes:
  macos-dev:
    auto_enable:
      required: [XcodeBuildMCP/macos]
      optional: [apple-docs, webx]
`
	cfg := loadInline(t, yaml)
	ae := cfg.Scenes["macos-dev"].AutoEnable
	if !reflect.DeepEqual(ae.Required, []string{"XcodeBuildMCP/macos"}) {
		t.Errorf("Required mismatch: got %v", ae.Required)
	}
	if !reflect.DeepEqual(ae.Optional, []string{"apple-docs", "webx"}) {
		t.Errorf("Optional mismatch: got %v", ae.Optional)
	}
}

// TestAutoEnable_All_Deduplicates verifies All() returns required+optional with dedup.
func TestAutoEnable_All_Deduplicates(t *testing.T) {
	ae := AutoEnable{
		Required: []string{"a", "b"},
		Optional: []string{"b", "c"}, // b duplicates required
	}
	got := ae.All()
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("All() = %v, want %v", got, want)
	}
}

// TestAutoEnable_IsRequired checks the required-lookup helper.
func TestAutoEnable_IsRequired(t *testing.T) {
	ae := AutoEnable{Required: []string{"a"}, Optional: []string{"b"}}
	if !ae.IsRequired("a") {
		t.Error("a should be required")
	}
	if ae.IsRequired("b") {
		t.Error("b should not be required")
	}
	if ae.IsRequired("c") {
		t.Error("c should not be required (not in either list)")
	}
}

// TestAutoEnable_InvalidNode rejects non-list non-mapping YAML values.
func TestAutoEnable_InvalidNode(t *testing.T) {
	yaml := `
capabilities: {}
scenes:
  bad:
    auto_enable: "not-a-list"
`
	_, err := loadInlineErr(yaml)
	if err == nil {
		t.Fatal("expected error for scalar auto_enable, got nil")
	}
}

// TestCapability_NewFields verifies v0.2 additions parse correctly.
func TestCapability_NewFields(t *testing.T) {
	yaml := `
capabilities:
  playwright:
    type: mcp
    command: npx
    args: ["@playwright/mcp@latest"]
    aliases: [browser, puppeteer]
    keywords: [browser, automation, e2e]
    required_env: [PLAYWRIGHT_HOME]
scenes: {}
`
	cfg := loadInline(t, yaml)
	cap := cfg.Capabilities["playwright"]
	if cap == nil {
		t.Fatal("playwright not loaded")
	}
	if !reflect.DeepEqual(cap.Aliases, []string{"browser", "puppeteer"}) {
		t.Errorf("Aliases = %v", cap.Aliases)
	}
	if !reflect.DeepEqual(cap.Keywords, []string{"browser", "automation", "e2e"}) {
		t.Errorf("Keywords = %v", cap.Keywords)
	}
	if !reflect.DeepEqual(cap.RequiredEnv, []string{"PLAYWRIGHT_HOME"}) {
		t.Errorf("RequiredEnv = %v", cap.RequiredEnv)
	}
}

// TestScene_NewFields verifies v0.2 scene metadata fields parse.
func TestScene_NewFields(t *testing.T) {
	yaml := `
capabilities: {}
scenes:
  web:
    description: "Web dev"
    extends: [base, frontend]
    aliases: [front]
    tags: [dev, web]
    auto_enable: [playwright]
    capabilities:
      custom-tool:
        type: cli
        command: mytool
`
	cfg := loadInline(t, yaml)
	s := cfg.Scenes["web"]
	if s.Description != "Web dev" {
		t.Errorf("Description = %q", s.Description)
	}
	if !reflect.DeepEqual(s.Extends, []string{"base", "frontend"}) {
		t.Errorf("Extends = %v", s.Extends)
	}
	if !reflect.DeepEqual(s.Aliases, []string{"front"}) {
		t.Errorf("Aliases = %v", s.Aliases)
	}
	if !reflect.DeepEqual(s.Tags, []string{"dev", "web"}) {
		t.Errorf("Tags = %v", s.Tags)
	}
	if s.Capabilities["custom-tool"] == nil {
		t.Error("inline capability custom-tool not loaded")
	}
	if s.Capabilities["custom-tool"].Command != "mytool" {
		t.Errorf("inline cap command = %q", s.Capabilities["custom-tool"].Command)
	}
}

// TestLegacy_SourceStamp verifies legacy Load stamps SourceLegacy on all entities.
func TestLegacy_SourceStamp(t *testing.T) {
	yaml := `
capabilities:
  foo:
    type: cli
    command: foo
scenes:
  default:
    auto_enable: [foo]
`
	cfg := loadInline(t, yaml)
	if cfg.Capabilities["foo"].Source != SourceLegacy {
		t.Errorf("legacy cap Source = %q, want %q", cfg.Capabilities["foo"].Source, SourceLegacy)
	}
	if cfg.Scenes["default"].Source != SourceLegacy {
		t.Errorf("legacy scene Source = %q, want %q", cfg.Scenes["default"].Source, SourceLegacy)
	}
}

// loadInline writes the given YAML to a temp file and loads it, failing the test on error.
func loadInline(t *testing.T, yaml string) *Config {
	t.Helper()
	cfg, err := loadInlineErr(yaml)
	if err != nil {
		t.Fatalf("loadInline: %v", err)
	}
	return cfg
}

func loadInlineErr(yaml string) (*Config, error) {
	dir, err := os.MkdirTemp("", "capx-cfg-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		return nil, err
	}
	return Load(path)
}
