package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDump_NoScene(t *testing.T) {
	cfg := &Config{
		Capabilities: map[string]*Capability{
			"webx": {Type: "cli", Command: "webx", Source: SourceGlobal},
		},
		Scenes:       map[string]*Scene{},
		DefaultScene: "default",
		ScopeRoots:   map[ScopeKind]string{ScopeKindGlobal: "/global"},
	}

	dump, err := Dump(cfg, "", "0.2.0")
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	if dump.SchemaVersion != 1 {
		t.Errorf("schema_version: got %d", dump.SchemaVersion)
	}
	if dump.ActiveScene != nil {
		t.Errorf("active_scene should be nil when no --scene, got %v", *dump.ActiveScene)
	}
	if len(dump.ConfigSources) != 1 || dump.ConfigSources[0].Layer != "global" {
		t.Errorf("config_sources: %+v", dump.ConfigSources)
	}

	dc := dump.Capabilities["webx"]
	if dc == nil {
		t.Fatal("webx missing")
	}
	if !strings.HasPrefix(dc.ProcessHash, "sha256:") {
		t.Errorf("process_hash prefix wrong: %s", dc.ProcessHash)
	}
	if !strings.HasPrefix(dc.ToolsHash, "sha256:") {
		t.Errorf("tools_hash prefix wrong: %s", dc.ToolsHash)
	}
	if dc.Source != "global" {
		t.Errorf("source: %s", dc.Source)
	}
}

func TestDump_WithScene_ExposesInlineAndLineage(t *testing.T) {
	inline := &Capability{Type: "mcp", Command: "npx", Description: "inline", Source: SourceSceneInline}
	cfg := &Config{
		Capabilities: map[string]*Capability{
			"play": {Type: "mcp", Command: "npx", Description: "global", Source: SourceGlobal},
		},
		Scenes: map[string]*Scene{
			"base":  {AutoEnable: AutoEnable{Optional: []string{"play"}}, Source: SourceGlobal},
			"child": {Extends: []string{"base"}, Capabilities: map[string]*Capability{"play": inline}, Source: SourceProject},
		},
		DefaultScene: "default",
	}
	dump, err := Dump(cfg, "child", "0.2.0")
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	if dump.ActiveScene == nil || *dump.ActiveScene != "child" {
		t.Errorf("active_scene: %v", dump.ActiveScene)
	}
	dc := dump.Capabilities["play"]
	if dc == nil {
		t.Fatal("play missing in capabilities view")
	}
	if dc.Description == nil || *dc.Description != "inline" {
		t.Errorf("inline description should override global, got %v", dc.Description)
	}
	if !strings.Contains(dc.Source, "scene:child (inline)") {
		t.Errorf("source should reflect inline scene, got %q", dc.Source)
	}

	ds := dump.Scenes["child"]
	if ds == nil {
		t.Fatal("scene child missing")
	}
	if len(ds.ExtendsResolved) != 2 || ds.ExtendsResolved[0] != "base" || ds.ExtendsResolved[1] != "child" {
		t.Errorf("extends_resolved: %+v", ds.ExtendsResolved)
	}
	if len(ds.InlineCapabilityNames) != 1 || ds.InlineCapabilityNames[0] != "play" {
		t.Errorf("inline_capability_names: %+v", ds.InlineCapabilityNames)
	}
}

func TestDump_EmptyCollectionsAreNull(t *testing.T) {
	// Dump's canonicalization collapses empty slices/maps to null so
	// consumers can do "present-and-non-empty" checks uniformly.
	cfg := &Config{
		Capabilities: map[string]*Capability{
			"minimal": {Type: "mcp", URL: "https://x", Source: SourceGlobal},
		},
		Scenes:       map[string]*Scene{},
		DefaultScene: "default",
	}
	dump, _ := Dump(cfg, "", "0.2.0")

	// JSON round-trip to confirm null serialization rather than "[]"/"{}".
	payload, err := json.Marshal(dump.Capabilities["minimal"])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(payload)
	// Args/Env/Tools/Aliases/Keywords/Tags/RequiredEnv all empty → null.
	for _, field := range []string{"\"args\":null", "\"env\":null", "\"tools\":null",
		"\"aliases\":null", "\"keywords\":null", "\"tags\":null", "\"required_env\":null"} {
		if !strings.Contains(s, field) {
			t.Errorf("expected %q in JSON, got: %s", field, s)
		}
	}
}

func TestDump_MCPToolsHashIsFixedSentinel(t *testing.T) {
	cfg := &Config{
		Capabilities: map[string]*Capability{
			"mcp1": {Type: "mcp", URL: "https://a", Source: SourceGlobal},
			"mcp2": {Type: "mcp", URL: "https://b", Source: SourceGlobal},
		},
		Scenes: map[string]*Scene{}, DefaultScene: "default",
	}
	dump, _ := Dump(cfg, "", "0.2.0")
	if dump.Capabilities["mcp1"].ToolsHash != dump.Capabilities["mcp2"].ToolsHash {
		t.Errorf("all MCP caps (no declared tools) must share the same tools_hash sentinel")
	}
}
