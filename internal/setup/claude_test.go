package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/oaooao/capx/internal/config"
)

func TestSetupClaudeCode_Migration(t *testing.T) {
	// Set up fake home directory.
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create fake ~/.claude.json with existing MCP servers.
	claudeJSON := map[string]any{
		"numStartups": 42,
		"mcpServers": map[string]any{
			"context7": map[string]any{
				"type": "http",
				"url":  "https://mcp.context7.com/mcp",
			},
			"xcode": map[string]any{
				"command": "xcodebuildmcp",
				"args":    []string{"mcp"},
			},
		},
	}
	claudePath := filepath.Join(home, ".claude.json")
	writeJSON(t, claudePath, claudeJSON)

	// Create capx config directory.
	configPath := filepath.Join(home, ".config", "capx", "config.yaml")

	// Run migration.
	if err := SetupClaudeCode(configPath); err != nil {
		t.Fatalf("SetupClaudeCode failed: %v", err)
	}

	// Verify capx config was created with migrated servers.
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("capx config should exist: %v", err)
	}
	if _, ok := cfg.Capabilities["context7"]; !ok {
		t.Error("context7 should be migrated to capx config")
	}
	if _, ok := cfg.Capabilities["xcode"]; !ok {
		t.Error("xcode should be migrated to capx config")
	}
	if cap := cfg.Capabilities["context7"]; cap.Transport != "http" || cap.URL != "https://mcp.context7.com/mcp" {
		t.Errorf("context7 migration incorrect: %+v", cap)
	}
	if cap := cfg.Capabilities["xcode"]; cap.Transport != "stdio" || cap.Command != "xcodebuildmcp" {
		t.Errorf("xcode migration incorrect: %+v", cap)
	}

	// Verify claude.json now only has capx.
	updatedData, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("reading updated claude.json: %v", err)
	}
	var updated map[string]json.RawMessage
	json.Unmarshal(updatedData, &updated)

	var servers map[string]json.RawMessage
	json.Unmarshal(updated["mcpServers"], &servers)
	if len(servers) != 1 {
		t.Errorf("expected 1 server (capx), got %d", len(servers))
	}
	if _, ok := servers["capx"]; !ok {
		t.Error("claude.json should now contain capx server")
	}

	// Verify non-MCP fields are preserved.
	var numStartups float64
	json.Unmarshal(updated["numStartups"], &numStartups)
	if numStartups != 42 {
		t.Errorf("numStartups should be preserved, got %v", numStartups)
	}

	// Verify backup exists.
	backupDir := filepath.Join(home, ".config", "capx", "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("backup dir should exist: %v", err)
	}
	if len(entries) == 0 {
		t.Error("should have created a backup")
	}
}

func TestSetupClaudeCode_NoServers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	claudeJSON := map[string]any{"numStartups": 1}
	claudePath := filepath.Join(home, ".claude.json")
	writeJSON(t, claudePath, claudeJSON)

	configPath := filepath.Join(home, ".config", "capx", "config.yaml")
	// Should not error, just report nothing to migrate.
	if err := SetupClaudeCode(configPath); err != nil {
		t.Fatalf("should handle empty mcpServers: %v", err)
	}
}

func TestSetupClaudeCode_SkipsExistingCapx(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	claudeJSON := map[string]any{
		"mcpServers": map[string]any{
			"capx": map[string]any{
				"command": "capx",
				"args":    []string{"serve"},
			},
			"context7": map[string]any{
				"url": "https://mcp.context7.com/mcp",
			},
		},
	}
	claudePath := filepath.Join(home, ".claude.json")
	writeJSON(t, claudePath, claudeJSON)

	configPath := filepath.Join(home, ".config", "capx", "config.yaml")
	if err := SetupClaudeCode(configPath); err != nil {
		t.Fatalf("SetupClaudeCode failed: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	// capx should not be migrated as a capability.
	if _, ok := cfg.Capabilities["capx"]; ok {
		t.Error("capx itself should not be migrated as a capability")
	}
	// But context7 should be.
	if _, ok := cfg.Capabilities["context7"]; !ok {
		t.Error("context7 should be migrated")
	}
}

func TestSetupClaudeCode_DoesNotOverwriteExistingCaps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Pre-create capx config with context7 already configured differently.
	configPath := filepath.Join(home, ".config", "capx", "config.yaml")
	existing := &config.Config{
		Capabilities: map[string]*config.Capability{
			"context7": {Type: "mcp", Transport: "http", URL: "https://custom.url/mcp", Description: "custom"},
		},
		Scenes:       map[string]*config.Scene{"default": {AutoEnable: []string{"context7"}}},
		DefaultScene: "default",
	}
	config.Save(existing, configPath)

	// claude.json also has context7 but with different URL.
	claudeJSON := map[string]any{
		"mcpServers": map[string]any{
			"context7": map[string]any{"url": "https://original.url/mcp"},
		},
	}
	writeJSON(t, filepath.Join(home, ".claude.json"), claudeJSON)

	SetupClaudeCode(configPath)

	cfg, _ := config.Load(configPath)
	// Should keep the existing capx config, not overwrite.
	if cfg.Capabilities["context7"].URL != "https://custom.url/mcp" {
		t.Error("existing capability should not be overwritten")
	}
}

func TestSetupClaudeCode_MissingClaudeJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".config", "capx", "config.yaml")
	err := SetupClaudeCode(configPath)
	if err == nil {
		t.Error("should error when ~/.claude.json doesn't exist")
	}
}

func TestSetupClaudeCode_InvalidJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	os.WriteFile(filepath.Join(home, ".claude.json"), []byte("not json{{{"), 0o644)

	configPath := filepath.Join(home, ".config", "capx", "config.yaml")
	err := SetupClaudeCode(configPath)
	if err == nil {
		t.Error("should error on invalid JSON")
	}
}

func writeJSON(t *testing.T, path string, data any) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
