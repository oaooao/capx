package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupLegacyGlobal creates a scratch XDG_CONFIG_HOME with a v0.1 config.yaml
// and returns (globalDir, realDir). globalDir is what callers pass to
// MigrateOptions; realDir is the resolved destination (identical here since
// no symlink is involved).
func setupLegacyGlobal(t *testing.T, legacyYAML string) (globalDir string) {
	t.Helper()
	base := t.TempDir()
	xdg := filepath.Join(base, "xdg")
	globalDir = filepath.Join(xdg, "capx")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(legacyYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	return globalDir
}

func TestMigrate_HappyPath(t *testing.T) {
	const yaml = `
default_scene: web
capabilities:
  context7:
    type: mcp
    url: https://mcp.context7.com/mcp
  webx:
    type: cli
    command: webx
    tools:
      read:
        description: read
scenes:
  default:
    auto_enable: []
  web:
    auto_enable: [context7, webx]
`
	globalDir := setupLegacyGlobal(t, yaml)
	report, err := Migrate(MigrateOptions{GlobalDir: globalDir, Stdout: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("migrate: %v (errors=%v)", err, report.Errors)
	}
	if report.Status != "ok" {
		t.Errorf("status: %s", report.Status)
	}
	for _, f := range []string{"capabilities.yaml", "settings.yaml",
		"scenes/default.yaml", "scenes/web.yaml", "config.yaml.v01.bak"} {
		if _, err := os.Stat(filepath.Join(globalDir, f)); err != nil {
			t.Errorf("expected %s, got %v", f, err)
		}
	}
	// Legacy config.yaml should be gone from its original name.
	if _, err := os.Stat(filepath.Join(globalDir, "config.yaml")); err == nil {
		t.Error("config.yaml should have been renamed to .v01.bak")
	}
}

func TestMigrate_DryRun(t *testing.T) {
	const yaml = `
default_scene: default
capabilities: {}
scenes:
  default:
    auto_enable: []
`
	globalDir := setupLegacyGlobal(t, yaml)
	report, err := Migrate(MigrateOptions{GlobalDir: globalDir, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run migrate: %v", err)
	}
	if report.Status != "ok" {
		t.Errorf("status: %s", report.Status)
	}
	// No mutations on real dir.
	if _, err := os.Stat(filepath.Join(globalDir, "capabilities.yaml")); err == nil {
		t.Error("dry-run must not write capabilities.yaml")
	}
	if _, err := os.Stat(filepath.Join(globalDir, "config.yaml")); err != nil {
		t.Error("dry-run must keep legacy config.yaml intact")
	}
	if len(report.V02Files) == 0 {
		t.Error("dry-run report should still list the prospective v02 files")
	}
}

func TestMigrate_RefusesIfV02Present(t *testing.T) {
	globalDir := setupLegacyGlobal(t, "capabilities: {}\nscenes: {}\n")
	if err := os.WriteFile(filepath.Join(globalDir, "capabilities.yaml"), []byte("capabilities: {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Migrate(MigrateOptions{GlobalDir: globalDir, Stdout: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("expected refusal when v0.2 structure already exists")
	}
	if report.Status != "aborted" {
		t.Errorf("status: %s", report.Status)
	}
}

func TestMigrate_FailFastOnTransportConflict(t *testing.T) {
	const yaml = `
capabilities:
  bad:
    type: mcp
    command: x
    url: https://y
scenes: {}
`
	globalDir := setupLegacyGlobal(t, yaml)
	report, err := Migrate(MigrateOptions{GlobalDir: globalDir, Stdout: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("expected fail-fast on command+url conflict")
	}
	if !strings.Contains(err.Error(), "BOTH command and url") {
		t.Errorf("unexpected error: %v", err)
	}
	if report.Status != "aborted" {
		t.Errorf("status: %s", report.Status)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "config.yaml")); err != nil {
		t.Error("legacy config.yaml should be untouched on abort")
	}
}

func TestMigrate_DisabledCapabilityWarning(t *testing.T) {
	const yaml = `
default_scene: default
capabilities:
  ghost:
    type: cli
    command: echo
    disabled: true
scenes:
  default:
    auto_enable: [ghost]
`
	globalDir := setupLegacyGlobal(t, yaml)
	report, _ := Migrate(MigrateOptions{GlobalDir: globalDir, Stdout: &bytes.Buffer{}})
	var found bool
	for _, w := range report.Warnings {
		if w.Kind == "scene_references_disabled_capability" && w.Scene == "default" && w.Capability == "ghost" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected disabled-cap warning, got %+v", report.Warnings)
	}
}

func TestMigrate_MissingCapabilityWarning(t *testing.T) {
	const yaml = `
capabilities: {}
scenes:
  default:
    auto_enable: [not-there]
`
	globalDir := setupLegacyGlobal(t, yaml)
	report, _ := Migrate(MigrateOptions{GlobalDir: globalDir, Stdout: &bytes.Buffer{}})
	var found bool
	for _, w := range report.Warnings {
		if w.Kind == "scene_references_missing_capability" && w.Capability == "not-there" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing-cap warning, got %+v", report.Warnings)
	}
}

func TestMigrate_EnvStringification(t *testing.T) {
	const yaml = `
capabilities:
  e:
    type: cli
    command: echo
    env:
      N: 42
      B: true
      Z:
scenes: {}
`
	globalDir := setupLegacyGlobal(t, yaml)
	report, err := Migrate(MigrateOptions{GlobalDir: globalDir, Stdout: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	kinds := map[string]int{}
	for _, w := range report.Warnings {
		kinds[w.Kind]++
	}
	if kinds["env_value_stringified"] < 2 {
		t.Errorf("want 2+ stringification warnings (42, true), got %d", kinds["env_value_stringified"])
	}
	if kinds["env_value_null_to_empty"] < 1 {
		t.Errorf("want 1+ null_to_empty warning, got %d", kinds["env_value_null_to_empty"])
	}
	// Verify written yaml actually has string values.
	written, err := os.ReadFile(filepath.Join(globalDir, "capabilities.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(written)
	// YAML marshaller may quote the key too; just confirm 42 appears as a
	// string (quoted) not as a bare integer.
	if !strings.Contains(content, `"42"`) && !strings.Contains(content, "'42'") {
		t.Errorf("expected 42 quoted as string in output, got:\n%s", content)
	}
	if !strings.Contains(content, `"true"`) && !strings.Contains(content, "'true'") {
		t.Errorf("expected true quoted as string in output, got:\n%s", content)
	}
}

func TestMigrate_DefaultSceneInferred(t *testing.T) {
	const yaml = `
capabilities: {}
scenes:
  whatever:
    auto_enable: []
`
	globalDir := setupLegacyGlobal(t, yaml)
	report, err := Migrate(MigrateOptions{GlobalDir: globalDir, Stdout: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	settings, _ := os.ReadFile(filepath.Join(globalDir, "settings.yaml"))
	if !strings.Contains(string(settings), "default_scene: default") {
		t.Errorf("expected default_scene: default, got:\n%s", settings)
	}
	var foundWarn bool
	for _, w := range report.Warnings {
		if w.Kind == "default_scene_missing" {
			foundWarn = true
		}
	}
	if !foundWarn {
		t.Error("expected default_scene_missing warning")
	}
}

func TestMigrate_LegacyConfigNotFound(t *testing.T) {
	base := t.TempDir()
	xdg := filepath.Join(base, "xdg")
	globalDir := filepath.Join(xdg, "capx")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)

	_, err := Migrate(MigrateOptions{GlobalDir: globalDir, Stdout: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("expected error when config.yaml is missing")
	}
	if !strings.Contains(err.Error(), "legacy config not found") {
		t.Errorf("unexpected error: %v", err)
	}
}
