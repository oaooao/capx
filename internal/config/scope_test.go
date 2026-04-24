package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// setupScopeDir creates a temp directory with the given files (relative paths
// to contents). Returns the absolute path of the scope root.
func setupScopeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "capx-scope-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestLoadScope_Basic loads a scope with capabilities.yaml + one scene.
func TestLoadScope_Basic(t *testing.T) {
	dir := setupScopeDir(t, map[string]string{
		"capabilities.yaml": `
capabilities:
  context7:
    type: mcp
    url: https://mcp.context7.com/mcp
    description: "Docs lookup"
`,
		"scenes/default.yaml": `
description: "Default scene"
auto_enable: [context7]
`,
	})
	scope, err := LoadScope(ScopeKindGlobal, dir)
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}
	if scope.Kind != ScopeKindGlobal {
		t.Errorf("Kind = %q", scope.Kind)
	}
	// Verify capability loaded with correct source.
	c := scope.Capabilities["context7"]
	if c == nil {
		t.Fatal("context7 not loaded")
	}
	if c.Source != SourceGlobal {
		t.Errorf("context7 Source = %q, want %q", c.Source, SourceGlobal)
	}
	// Verify scene loaded with name derived from filename.
	s := scope.Scenes["default"]
	if s == nil {
		t.Fatal("default scene not loaded")
	}
	if s.Description != "Default scene" {
		t.Errorf("Description = %q", s.Description)
	}
	if s.Source != SourceGlobal {
		t.Errorf("scene Source = %q", s.Source)
	}
	if !reflect.DeepEqual(s.AutoEnable.Optional, []string{"context7"}) {
		t.Errorf("AutoEnable.Optional = %v", s.AutoEnable.Optional)
	}
}

// TestLoadScope_CapabilitiesD verifies capabilities.d/*.yaml scan and lex order.
func TestLoadScope_CapabilitiesD(t *testing.T) {
	dir := setupScopeDir(t, map[string]string{
		"capabilities.yaml": `
capabilities:
  a:
    type: cli
    command: a
`,
		"capabilities.d/10-second.yaml": `
capabilities:
  b:
    type: cli
    command: b
`,
		"capabilities.d/01-first.yaml": `
capabilities:
  c:
    type: cli
    command: c
`,
	})
	scope, err := LoadScope(ScopeKindProject, dir)
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}
	if scope.Capabilities["a"].Source != SourceProject {
		t.Errorf("a Source = %q", scope.Capabilities["a"].Source)
	}
	if scope.Capabilities["b"].Source != SourceProjectD {
		t.Errorf("b Source = %q", scope.Capabilities["b"].Source)
	}
	if scope.Capabilities["c"].Source != SourceProjectD {
		t.Errorf("c Source = %q", scope.Capabilities["c"].Source)
	}
}

// TestLoadScope_IntraDOverride verifies that two .d files defining the same cap
// produce a warning and the lex-later file wins.
func TestLoadScope_IntraDOverride(t *testing.T) {
	dir := setupScopeDir(t, map[string]string{
		"capabilities.d/01-first.yaml": `
capabilities:
  shared:
    type: cli
    command: first
`,
		"capabilities.d/99-last.yaml": `
capabilities:
  shared:
    type: cli
    command: last
`,
	})
	scope, err := LoadScope(ScopeKindGlobal, dir)
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}
	if got := scope.Capabilities["shared"].Command; got != "last" {
		t.Errorf("override order wrong: shared.command = %q, want %q", got, "last")
	}
	if len(scope.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %+v", len(scope.Warnings), scope.Warnings)
	}
	if scope.Warnings[0].Kind != "intra_scope_capability_override" {
		t.Errorf("warning kind = %q", scope.Warnings[0].Kind)
	}
}

// TestLoadScope_SceneInlineCapability verifies inline caps get SourceSceneInline.
func TestLoadScope_SceneInlineCapability(t *testing.T) {
	dir := setupScopeDir(t, map[string]string{
		"scenes/web.yaml": `
description: "Web, self-contained"
capabilities:
  playwright:
    type: mcp
    command: npx
    args: ["@playwright/mcp@latest"]
auto_enable: [playwright]
`,
	})
	scope, err := LoadScope(ScopeKindProject, dir)
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}
	s := scope.Scenes["web"]
	if s == nil {
		t.Fatal("web scene not loaded")
	}
	inline := s.Capabilities["playwright"]
	if inline == nil {
		t.Fatal("inline playwright not loaded")
	}
	if inline.Source != SourceSceneInline {
		t.Errorf("inline cap Source = %q, want %q", inline.Source, SourceSceneInline)
	}
}

// TestLoadScope_Settings verifies settings.yaml parses to the Settings struct.
func TestLoadScope_Settings(t *testing.T) {
	dir := setupScopeDir(t, map[string]string{
		"settings.yaml": `
default_scene: web-dev
`,
	})
	scope, err := LoadScope(ScopeKindGlobal, dir)
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}
	if scope.Settings == nil {
		t.Fatal("Settings not loaded")
	}
	if scope.Settings.DefaultScene != "web-dev" {
		t.Errorf("DefaultScene = %q", scope.Settings.DefaultScene)
	}
}

// TestLoadScope_AllowOverrideComment verifies the # capx-allow-override
// directive is captured for later merge use.
func TestLoadScope_AllowOverrideComment(t *testing.T) {
	dir := setupScopeDir(t, map[string]string{
		"scenes/web-custom.yaml": `# capx-allow-override: playwright, chrome
description: "Custom web scene"
capabilities:
  playwright:
    type: mcp
    command: npx
auto_enable: [playwright]
`,
	})
	scope, err := LoadScope(ScopeKindProject, dir)
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}
	path := filepath.Join(scope.RootDir, "scenes/web-custom.yaml")
	want := []string{"playwright", "chrome"}
	got := scope.AllowOverride[path]
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AllowOverride = %v, want %v", got, want)
	}
}

// TestParseAllowOverride covers multiple directive forms.
func TestParseAllowOverride(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{
			"single name",
			"# capx-allow-override: foo\n\ndescription: x\n",
			[]string{"foo"},
		},
		{
			"comma list",
			"# capx-allow-override: a, b, c\n",
			[]string{"a", "b", "c"},
		},
		{
			"multiple directives",
			"# capx-allow-override: a\n# capx-allow-override: b\n",
			[]string{"a", "b"},
		},
		{
			"stops at first non-comment",
			"# capx-allow-override: a\n\ndescription: x\n# capx-allow-override: too_late\n",
			[]string{"a"},
		},
		{
			"whitespace trim",
			"#   capx-allow-override:   foo  ,  bar  \n",
			[]string{"foo", "bar"},
		},
		{
			"no directive",
			"description: x\n",
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAllowOverride([]byte(tc.input))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFindProjectScope walks up from a deep directory to locate .capx/.
func TestFindProjectScope(t *testing.T) {
	root, err := os.MkdirTemp("", "capx-find-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })

	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create .capx/ at root.
	capxDir := filepath.Join(root, ".capx")
	if err := os.Mkdir(capxDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := FindProjectScope(deep)
	// On macOS, temp dirs resolve to /private/var/… via EvalSymlinks.
	wantReal, _ := filepath.EvalSymlinks(capxDir)
	if got != wantReal {
		t.Errorf("FindProjectScope(%q) = %q, want %q", deep, got, wantReal)
	}
}

// TestFindProjectScope_NotFound returns empty when no .capx exists.
func TestFindProjectScope_NotFound(t *testing.T) {
	root, err := os.MkdirTemp("", "capx-noscope-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })

	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	// No .capx anywhere on this path above temp. Walking up will hit /tmp
	// (which may actually contain a .capx from another test run). We need
	// isolation: create a marker file at root and only walk up to it.
	// FindProjectScope walks all the way to /; this test only guarantees
	// that in the absence of .capx under root, it doesn't report root/.capx.
	got := FindProjectScope(deep)
	// The function walks up; we only assert that it doesn't invent a path
	// inside our test root.
	rootCapx := filepath.Join(root, ".capx")
	realRootCapx, _ := filepath.EvalSymlinks(rootCapx)
	if got == rootCapx || (realRootCapx != "" && got == realRootCapx) {
		t.Errorf("unexpectedly found .capx at root: %q", got)
	}
}

// TestDiscoverConfig_CAPXHome_ReplacesGlobal verifies CAPX_HOME relocates the
// global scope directory but does NOT short-circuit project discovery.
func TestDiscoverConfig_CAPXHome_ReplacesGlobal(t *testing.T) {
	dir, err := os.MkdirTemp("", "capx-home-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	// pwd under a clean temp dir so there's no .capx/ anywhere above it.
	pwdDir, err := os.MkdirTemp("", "capx-pwd-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(pwdDir) })

	t.Setenv("CAPX_HOME", dir)
	t.Setenv("CAPX_ISOLATE", "")

	res, err := DiscoverConfig(pwdDir)
	if err != nil {
		t.Fatalf("DiscoverConfig: %v", err)
	}
	wantReal, _ := filepath.EvalSymlinks(dir)

	// CAPXHome flag is set (informational).
	if resReal, _ := filepath.EvalSymlinks(res.CAPXHome); resReal != wantReal && res.CAPXHome != dir {
		t.Errorf("CAPXHome = %q, want %q", res.CAPXHome, dir)
	}
	// Global field is now populated with the CAPX_HOME path (authoritative).
	if resReal, _ := filepath.EvalSymlinks(res.Global); resReal != wantReal && res.Global != dir {
		t.Errorf("Global = %q, want CAPX_HOME = %q", res.Global, dir)
	}
	// Project should be empty when pwd has no .capx/ ancestor.
	if res.Project != "" {
		t.Errorf("Project should be empty, got %q", res.Project)
	}
}

// TestDiscoverConfig_CAPXHome_KeepsProject verifies project .capx/ is still
// discovered when CAPX_HOME is set (no CAPX_ISOLATE).
func TestDiscoverConfig_CAPXHome_KeepsProject(t *testing.T) {
	home, err := os.MkdirTemp("", "capx-home-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })

	pwd, err := os.MkdirTemp("", "capx-pwd-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(pwd) })
	projectCapx := filepath.Join(pwd, ".capx")
	if err := os.Mkdir(projectCapx, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CAPX_HOME", home)
	t.Setenv("CAPX_ISOLATE", "")

	res, err := DiscoverConfig(pwd)
	if err != nil {
		t.Fatalf("DiscoverConfig: %v", err)
	}
	if res.Project == "" {
		t.Errorf("Project should be discovered alongside CAPX_HOME; got empty")
	}
}

// TestDiscoverConfig_IsolateSkipsProject: CAPX_ISOLATE=1 forces single-scope.
func TestDiscoverConfig_IsolateSkipsProject(t *testing.T) {
	home, err := os.MkdirTemp("", "capx-home-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })

	pwd, err := os.MkdirTemp("", "capx-pwd-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(pwd) })
	if err := os.Mkdir(filepath.Join(pwd, ".capx"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CAPX_HOME", home)
	t.Setenv("CAPX_ISOLATE", "1")

	res, err := DiscoverConfig(pwd)
	if err != nil {
		t.Fatalf("DiscoverConfig: %v", err)
	}
	if res.Project != "" {
		t.Errorf("CAPX_ISOLATE=1 should skip project discovery; got Project=%q", res.Project)
	}
}

// TestDiscoverConfig_CAPXHomeInvalid errors on non-existent dir.
func TestDiscoverConfig_CAPXHomeInvalid(t *testing.T) {
	t.Setenv("CAPX_HOME", "/this/path/does/not/exist/xyz123")
	_, err := DiscoverConfig("/tmp")
	if err == nil {
		t.Fatal("expected error for invalid CAPX_HOME, got nil")
	}
}

// TestDiscoverConfig_Project finds a project scope from PWD.
func TestDiscoverConfig_Project(t *testing.T) {
	// Clear env so we exercise the project/global path.
	t.Setenv("CAPX_HOME", "")
	// Put global somewhere that does not exist so we isolate the project branch.
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent-capx-test")

	root, err := os.MkdirTemp("", "capx-proj-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err := os.Mkdir(filepath.Join(root, ".capx"), 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "sub", "deep")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := DiscoverConfig(deep)
	if err != nil {
		t.Fatalf("DiscoverConfig: %v", err)
	}
	wantReal, _ := filepath.EvalSymlinks(filepath.Join(root, ".capx"))
	if res.Project != wantReal {
		t.Errorf("Project = %q, want %q", res.Project, wantReal)
	}
}
