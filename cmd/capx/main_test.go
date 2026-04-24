package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain lets this test binary double as the capx CLI: when invoked with
// BE_CAPX_CLI=1, it runs main() against the forwarded os.Args and exits.
// This gives us end-to-end coverage of the actual CLI dispatch (including
// loadConfig routing) without a separate build step.
func TestMain(m *testing.M) {
	if os.Getenv("BE_CAPX_CLI") == "1" {
		// os.Args[0] is the test binary; the rest are the capx args the test
		// wanted to run. Rewrite os.Args so main() sees the expected shape.
		args := []string{"capx"}
		args = append(args, os.Args[1:]...)
		os.Args = args
		main()
		return
	}
	os.Exit(m.Run())
}

// writeV02Scope builds a minimal but valid v0.2 directory layout in dir:
// capabilities.yaml + scenes/<name>.yaml + settings.yaml. Returns the dir
// so callers can pass it via CAPX_HOME.
func writeV02Scope(t *testing.T, caps, scenes, settings string) string {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "capabilities.yaml"), []byte(caps), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	scenesDir := filepath.Join(dir, "scenes")
	if err := os.Mkdir(scenesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// scenes is the full content of scenes/default.yaml for simplicity.
	if err := os.WriteFile(filepath.Join(scenesDir, "default.yaml"), []byte(scenes), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runCLI executes this test binary as the capx CLI with the given args.
// It isolates the environment via CAPX_HOME so global/project scopes don't
// leak into the test.
func runCLI(t *testing.T, capxHome string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(),
		"BE_CAPX_CLI=1",
		"CAPX_HOME="+capxHome,
		// Defensively unset CAPX_CONFIG so a developer-shell value never
		// flips loadConfig into legacy single-file mode mid-test.
		"CAPX_CONFIG=",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

const v02Capabilities = `
capabilities:
  alpha:
    type: cli
    command: echo
    description: "alpha capability"
    tags: [test]
  beta:
    type: cli
    command: echo
    description: "beta capability"
    tags: [test]
`

const v02SceneDefault = `
auto_enable:
  optional: [alpha]
`

const v02Settings = `
default_scene: default
`

// TestList_ReadsV02ScopeViaLoadMerged is the regression test for the bug
// where cmdList/cmdScenes/cmdScene called config.Load (legacy single-file)
// instead of the scope-aware loader. After migrating to v0.2 there is no
// config.yaml at all, so the legacy path fails with "no such file or
// directory". This test would catch a revert.
func TestList_ReadsV02ScopeViaLoadMerged(t *testing.T) {
	dir := writeV02Scope(t, v02Capabilities, v02SceneDefault, v02Settings)

	out, err := runCLI(t, dir, "list")
	if err != nil {
		t.Fatalf("capx list failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("expected list to include alpha and beta, got:\n%s", out)
	}
}

func TestSceneList_ReadsV02ScopeViaLoadMerged(t *testing.T) {
	dir := writeV02Scope(t, v02Capabilities, v02SceneDefault, v02Settings)

	out, err := runCLI(t, dir, "scene", "list")
	if err != nil {
		t.Fatalf("capx scene list failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "default") {
		t.Errorf("expected scene list to include 'default', got:\n%s", out)
	}
}

func TestScenes_ReadsV02ScopeViaLoadMerged(t *testing.T) {
	dir := writeV02Scope(t, v02Capabilities, v02SceneDefault, v02Settings)

	out, err := runCLI(t, dir, "scenes")
	if err != nil {
		t.Fatalf("capx scenes failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "default") {
		t.Errorf("expected scenes output to include 'default', got:\n%s", out)
	}
}

// TestList_LegacyConfigPathRespected ensures that when --config is explicitly
// passed, loadConfig falls back to the v0.1 single-file loader (the backwards
// compat path). Without this, old users pinning a file via --config would
// silently switch to scope discovery.
func TestList_LegacyConfigPathRespected(t *testing.T) {
	tmp := t.TempDir()
	// Write a v0.1 single-file config with a capability named "gamma".
	legacy := `
capabilities:
  gamma:
    type: cli
    command: echo
    description: "gamma from legacy file"
scenes:
  default:
    auto_enable:
      optional: [gamma]
default_scene: default
`
	legacyPath := filepath.Join(tmp, "legacy.yaml")
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	// Build a v0.2 scope in a different directory that contains "alpha" —
	// if --config is honored, we should see gamma (from legacyPath), not alpha.
	scopeDir := writeV02Scope(t, v02Capabilities, v02SceneDefault, v02Settings)

	// Use CAPX_CONFIG env (avoids arg-order ambiguity since capx parses
	// subcommands positionally). Deliberately also set CAPX_HOME to a v0.2
	// scope with DIFFERENT capabilities; CAPX_CONFIG should take precedence.
	cmd := exec.Command(os.Args[0], "list")
	cmd.Env = append(os.Environ(),
		"BE_CAPX_CLI=1",
		"CAPX_HOME="+scopeDir,
		"CAPX_CONFIG="+legacyPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("capx list (CAPX_CONFIG=legacy) failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gamma") {
		t.Errorf("expected legacy --config path to win, output missing 'gamma':\n%s", out)
	}
	if strings.Contains(string(out), "alpha") {
		t.Errorf("v0.2 scope leaked through despite --config, output:\n%s", out)
	}
}
