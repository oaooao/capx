package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// setupGlobalAndProject sets up a temp global dir, temp project dir, and
// points CAPX_HOME/env variables so LoadMerged sees them as the two scopes.
//
// Returns (pwdUnderProject, globalDir, projectDir).
func setupGlobalAndProject(t *testing.T, globalFiles, projectFiles map[string]string) (string, string, string) {
	t.Helper()
	// Isolate: clear CAPX_HOME; point XDG_CONFIG_HOME at a sandbox parent.
	t.Setenv("CAPX_HOME", "")
	sandbox, err := os.MkdirTemp("", "capx-merge-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sandbox) })

	// Global scope at sandbox/.xdg/capx/
	xdg := filepath.Join(sandbox, ".xdg")
	globalDir := filepath.Join(xdg, "capx")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	for rel, content := range globalFiles {
		full := filepath.Join(globalDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Project scope at sandbox/proj/.capx/
	projRoot := filepath.Join(sandbox, "proj")
	projectDir := filepath.Join(projRoot, ".capx")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range projectFiles {
		full := filepath.Join(projectDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// pwd nested inside project for realistic discovery.
	pwd := filepath.Join(projRoot, "src", "deep")
	if err := os.MkdirAll(pwd, 0o755); err != nil {
		t.Fatal(err)
	}

	return pwd, globalDir, projectDir
}

// TestLoadMerged_GlobalOnly loads when only a global scope exists.
func TestLoadMerged_GlobalOnly(t *testing.T) {
	pwd, _, _ := setupGlobalAndProject(t,
		map[string]string{
			"capabilities.yaml": `
capabilities:
  context7:
    type: mcp
    url: https://mcp.context7.com/mcp
`,
		},
		nil,
	)
	// Move pwd away from project to a sibling not under any scope.
	sandbox := filepath.Dir(filepath.Dir(filepath.Dir(pwd)))
	neutral := filepath.Join(sandbox, "elsewhere")
	if err := os.MkdirAll(neutral, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadMerged(neutral)
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	if cfg.Capabilities["context7"] == nil {
		t.Fatal("context7 not loaded from global")
	}
	if cfg.Capabilities["context7"].Source != SourceGlobal {
		t.Errorf("context7 Source = %q", cfg.Capabilities["context7"].Source)
	}
}

// TestLoadMerged_ProjectOverridesGlobal: same-name cap defined in both scopes;
// project wins with integer-object replace (fields NOT inherited from global).
func TestLoadMerged_ProjectOverridesGlobal(t *testing.T) {
	pwd, _, _ := setupGlobalAndProject(t,
		map[string]string{
			"capabilities.yaml": `
capabilities:
  playwright:
    type: mcp
    command: npx
    description: "global playwright"
    aliases: [browser-global]
`,
		},
		map[string]string{
			"capabilities.yaml": `# capx-allow-override: playwright
capabilities:
  playwright:
    type: mcp
    command: npx
    args: ["--headless"]
    description: "project override"
`,
		},
	)
	cfg, err := LoadMerged(pwd)
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	c := cfg.Capabilities["playwright"]
	if c == nil {
		t.Fatal("playwright missing")
	}
	if c.Source != SourceProject {
		t.Errorf("Source = %q, want SourceProject (project wins)", c.Source)
	}
	// Integer-object replace: global's aliases [browser-global] must NOT carry over.
	if len(c.Aliases) != 0 {
		t.Errorf("Aliases should be empty after replace, got %v", c.Aliases)
	}
	// Project fields present.
	if c.Description != "project override" {
		t.Errorf("Description = %q", c.Description)
	}
	if !reflect.DeepEqual(c.Args, []string{"--headless"}) {
		t.Errorf("Args = %v", c.Args)
	}
	// No warning because # capx-allow-override silenced it.
	for _, w := range cfg.Warnings {
		if w.Kind == "cross_scope_capability_override" {
			t.Errorf("unexpected override warning: %+v", w)
		}
	}
}

// TestLoadMerged_OverrideWarning: cross-scope override WITHOUT allow-override
// directive produces a warning.
func TestLoadMerged_OverrideWarning(t *testing.T) {
	pwd, _, _ := setupGlobalAndProject(t,
		map[string]string{
			"capabilities.yaml": `
capabilities:
  shared:
    type: cli
    command: global
`,
		},
		map[string]string{
			"capabilities.yaml": `
capabilities:
  shared:
    type: cli
    command: project
`,
		},
	)
	cfg, err := LoadMerged(pwd)
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	if cfg.Capabilities["shared"].Command != "project" {
		t.Errorf("project should win: command = %q", cfg.Capabilities["shared"].Command)
	}
	found := false
	for _, w := range cfg.Warnings {
		if w.Kind == "cross_scope_capability_override" && strings.Contains(w.Message, "shared") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected override warning for 'shared', got warnings: %+v", cfg.Warnings)
	}
}

// TestLoadMerged_SceneOverride: same scene name in both scopes, project wins.
func TestLoadMerged_SceneOverride(t *testing.T) {
	pwd, _, _ := setupGlobalAndProject(t,
		map[string]string{
			"scenes/default.yaml": `
description: "global default"
auto_enable: [ctx]
`,
		},
		map[string]string{
			"scenes/default.yaml": `
description: "project default"
auto_enable: [ctx, webx]
`,
		},
	)
	cfg, err := LoadMerged(pwd)
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	s := cfg.Scenes["default"]
	if s.Description != "project default" {
		t.Errorf("Description = %q", s.Description)
	}
	found := false
	for _, w := range cfg.Warnings {
		if w.Kind == "cross_scope_scene_override" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected scene override warning")
	}
}

// TestLoadMerged_SettingsOverlay verifies field-level overlay of settings.
func TestLoadMerged_SettingsOverlay(t *testing.T) {
	pwd, _, _ := setupGlobalAndProject(t,
		map[string]string{
			"settings.yaml": `
default_scene: global-default
`,
		},
		map[string]string{
			"settings.yaml": `
default_scene: project-default
`,
		},
	)
	cfg, err := LoadMerged(pwd)
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	if cfg.DefaultScene != "project-default" {
		t.Errorf("DefaultScene = %q, want project-default", cfg.DefaultScene)
	}
}

// TestLoadMerged_SettingsInheritance: project settings.yaml absent → global wins.
func TestLoadMerged_SettingsInheritance(t *testing.T) {
	pwd, _, _ := setupGlobalAndProject(t,
		map[string]string{
			"settings.yaml": `
default_scene: global-default
`,
		},
		map[string]string{
			// No settings.yaml in project
			"capabilities.yaml": `capabilities: {}`,
		},
	)
	cfg, err := LoadMerged(pwd)
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	if cfg.DefaultScene != "global-default" {
		t.Errorf("DefaultScene = %q, want global-default (inherited)", cfg.DefaultScene)
	}
}

// TestLoadMerged_LegacyAndV02Coexist_Error: coexistence is rejected.
func TestLoadMerged_LegacyAndV02Coexist_Error(t *testing.T) {
	pwd, globalDir, _ := setupGlobalAndProject(t,
		map[string]string{
			"capabilities.yaml": `capabilities: {}`,
		},
		nil,
	)
	// Also place a legacy config.yaml in the global dir.
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(`capabilities: {}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadMerged(pwd)
	if err == nil {
		t.Fatal("expected coexistence error, got nil")
	}
	if !strings.Contains(err.Error(), "capx migrate") {
		t.Errorf("error should mention 'capx migrate', got: %v", err)
	}
}

// TestLoadMerged_LegacyOnly: only legacy config.yaml exists in global.
func TestLoadMerged_LegacyOnly(t *testing.T) {
	pwd, globalDir, _ := setupGlobalAndProject(t, nil, nil)
	// Remove all v0.2 structure we set up.
	for _, f := range []string{"capabilities.yaml", "scenes", "settings.yaml"} {
		os.RemoveAll(filepath.Join(globalDir, f))
	}
	// Write a v0.1 single-file.
	legacy := `
capabilities:
  legacy-cap:
    type: cli
    command: legacy
scenes:
  default:
    auto_enable: [legacy-cap]
default_scene: default
`
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadMerged(pwd)
	if err != nil {
		t.Fatalf("LoadMerged legacy: %v", err)
	}
	c := cfg.Capabilities["legacy-cap"]
	if c == nil {
		t.Fatal("legacy-cap not loaded")
	}
	if c.Source != SourceLegacy {
		t.Errorf("Source = %q, want SourceLegacy", c.Source)
	}
	if cfg.DefaultScene != "default" {
		t.Errorf("DefaultScene = %q", cfg.DefaultScene)
	}
}

// TestLoadMerged_CAPXHomeReplacesGlobalKeepsProject verifies the new
// CAPX_HOME semantics: CAPX_HOME replaces the global scope directory, but
// project .capx/ discovery still applies. global-only (from the default
// global) is ignored because CAPX_HOME overrides it; home-only (from the
// CAPX_HOME dir) and project-only both appear.
func TestLoadMerged_CAPXHomeReplacesGlobalKeepsProject(t *testing.T) {
	pwd, _, _ := setupGlobalAndProject(t,
		map[string]string{
			"capabilities.yaml": `
capabilities:
  global-only:
    type: cli
    command: g
`,
		},
		map[string]string{
			"capabilities.yaml": `
capabilities:
  project-only:
    type: cli
    command: p
`,
		},
	)
	// Separate CAPX_HOME with its own content.
	home, err := os.MkdirTemp("", "capx-override-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	if err := os.WriteFile(filepath.Join(home, "capabilities.yaml"), []byte(`
capabilities:
  home-only:
    type: cli
    command: h
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAPX_HOME", home)
	t.Setenv("CAPX_ISOLATE", "")
	cfg, err := LoadMerged(pwd)
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	if cfg.Capabilities["home-only"] == nil {
		t.Error("home-only (from CAPX_HOME) missing")
	}
	if cfg.Capabilities["project-only"] == nil {
		t.Error("project-only (from project .capx/) should remain; CAPX_HOME no longer short-circuits")
	}
	if cfg.Capabilities["global-only"] != nil {
		t.Error("global-only should be ignored (CAPX_HOME replaced the global dir)")
	}
	if root := cfg.ScopeRoots[ScopeKindGlobal]; root == "" {
		t.Error("ScopeRoots[Global] should record CAPX_HOME path")
	}
	if root := cfg.ScopeRoots[ScopeKindProject]; root == "" {
		t.Error("ScopeRoots[Project] should record project .capx/ path")
	}
}

// TestLoadMerged_CAPXHomeWithIsolate verifies CAPX_ISOLATE=1 restores the
// single-scope behavior (project discovery skipped).
func TestLoadMerged_CAPXHomeWithIsolate(t *testing.T) {
	pwd, _, _ := setupGlobalAndProject(t,
		nil,
		map[string]string{
			"capabilities.yaml": `
capabilities:
  project-only:
    type: cli
    command: p
`,
		},
	)
	home, err := os.MkdirTemp("", "capx-override-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	if err := os.WriteFile(filepath.Join(home, "capabilities.yaml"), []byte(`
capabilities:
  home-only:
    type: cli
    command: h
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAPX_HOME", home)
	t.Setenv("CAPX_ISOLATE", "1")
	cfg, err := LoadMerged(pwd)
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	if cfg.Capabilities["home-only"] == nil {
		t.Error("home-only (from CAPX_HOME) missing")
	}
	if cfg.Capabilities["project-only"] != nil {
		t.Error("project-only should be skipped under CAPX_ISOLATE=1")
	}
}

// TestMergeSettings_Nil: both nil → nil; one nil → copy of the other.
func TestMergeSettings_Nil(t *testing.T) {
	if got := mergeSettings(nil, nil); got != nil {
		t.Errorf("both nil → got %+v, want nil", got)
	}
	g := &Settings{DefaultScene: "g"}
	if got := mergeSettings(g, nil); got == nil || got.DefaultScene != "g" {
		t.Errorf("project nil → got %+v, want %+v", got, g)
	}
	p := &Settings{DefaultScene: "p"}
	if got := mergeSettings(nil, p); got == nil || got.DefaultScene != "p" {
		t.Errorf("global nil → got %+v, want %+v", got, p)
	}
}
