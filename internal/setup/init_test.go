package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// initTempDir creates a scratch directory with a pinned XDG_CONFIG_HOME so
// global-dir checks resolve deterministically (and safely — we never touch
// the real ~/.config/capx).
func initTempDir(t *testing.T) (pwd, xdg string) {
	t.Helper()
	base := t.TempDir()
	pwd = filepath.Join(base, "work")
	xdg = filepath.Join(base, "xdg")
	if err := os.MkdirAll(pwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(xdg, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	return pwd, xdg
}

func TestInit_CreatesCapabilitiesYAML(t *testing.T) {
	pwd, _ := initTempDir(t)

	var out bytes.Buffer
	if err := Init(InitOptions{Cwd: pwd, Stdout: &out}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	path := filepath.Join(pwd, ".capx", "capabilities.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s, got %v", path, err)
	}
	if !strings.Contains(out.String(), "created") {
		t.Errorf("expected 'created' in output, got %q", out.String())
	}
}

func TestInit_RejectsInsideExistingProjectScope(t *testing.T) {
	pwd, _ := initTempDir(t)
	// Set up an existing .capx/ directory.
	if err := os.MkdirAll(filepath.Join(pwd, ".capx"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Running Init in a subdirectory of pwd should detect the scope and reject.
	sub := filepath.Join(pwd, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	err := Init(InitOptions{Cwd: sub})
	if err == nil || !strings.Contains(err.Error(), "already inside") {
		t.Errorf("expected in-scope rejection, got %v", err)
	}
}

func TestInit_ForceBypassesInScope(t *testing.T) {
	pwd, _ := initTempDir(t)
	// Make pwd itself already a scope (has .capx/).
	if err := os.MkdirAll(filepath.Join(pwd, ".capx"), 0o755); err != nil {
		t.Fatal(err)
	}

	// In a subdirectory with --force, nested scope creation is allowed.
	sub := filepath.Join(pwd, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	err := Init(InitOptions{Cwd: sub, Force: true, Stdout: &bytes.Buffer{}})
	if err != nil {
		t.Errorf("--force should bypass in-scope check, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(sub, ".capx", "capabilities.yaml")); err != nil {
		t.Errorf("expected nested capabilities.yaml, got %v", err)
	}
}

func TestInit_RejectsInsideGlobalConfigDirEvenWithForce(t *testing.T) {
	_, xdg := initTempDir(t)
	globalCapx := filepath.Join(xdg, "capx")
	if err := os.MkdirAll(globalCapx, 0o755); err != nil {
		t.Fatal(err)
	}

	// --force must NOT bypass this one (§A.9).
	err := Init(InitOptions{Cwd: globalCapx, Force: true})
	if err == nil || !strings.Contains(err.Error(), "global config directory") {
		t.Errorf("expected global-dir rejection even with --force, got %v", err)
	}
}

func TestInit_DoesNotOverwriteExistingCapabilities(t *testing.T) {
	pwd, _ := initTempDir(t)
	capsPath := filepath.Join(pwd, ".capx", "capabilities.yaml")
	if err := os.MkdirAll(filepath.Dir(capsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(capsPath, []byte("# preexisting user content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Note: existing .capx/ also trips the in-scope check; pass --force to
	// get past that and exercise the "no overwrite" rule specifically.
	err := Init(InitOptions{Cwd: pwd, Force: true})
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("expected overwrite refusal, got %v", err)
	}

	// File content must be preserved.
	data, _ := os.ReadFile(capsPath)
	if string(data) != "# preexisting user content" {
		t.Errorf("file was modified: %q", data)
	}
}

func TestInit_AddScenesRequiresExistingScope(t *testing.T) {
	pwd, _ := initTempDir(t)
	err := Init(InitOptions{Cwd: pwd, AddScenes: true})
	if err == nil || !strings.Contains(err.Error(), "no project scope") {
		t.Errorf("expected 'no project scope' err, got %v", err)
	}
}

func TestInit_AddScenesCreatesDefaultSample(t *testing.T) {
	pwd, _ := initTempDir(t)
	// Create a valid project scope first.
	if err := os.MkdirAll(filepath.Join(pwd, ".capx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Init(InitOptions{Cwd: pwd, AddScenes: true, Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatalf("AddScenes: %v", err)
	}
	sample := filepath.Join(pwd, ".capx", "scenes", "default.yaml")
	if _, err := os.Stat(sample); err != nil {
		t.Errorf("expected %s, got %v", sample, err)
	}
}

func TestInit_AddScenesIsIdempotent(t *testing.T) {
	pwd, _ := initTempDir(t)
	if err := os.MkdirAll(filepath.Join(pwd, ".capx", "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	preexisting := filepath.Join(pwd, ".capx", "scenes", "default.yaml")
	const userContent = "# user customized"
	if err := os.WriteFile(preexisting, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Init(InitOptions{Cwd: pwd, AddScenes: true, Stdout: &out}); err != nil {
		t.Fatalf("AddScenes: %v", err)
	}
	data, _ := os.ReadFile(preexisting)
	if string(data) != userContent {
		t.Errorf("existing scene was overwritten: %q", data)
	}
}

func TestInit_GlobalTargetsGlobalDir(t *testing.T) {
	_, xdg := initTempDir(t)
	if err := Init(InitOptions{Global: true, Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatalf("Init --global: %v", err)
	}
	capsPath := filepath.Join(xdg, "capx", "capabilities.yaml")
	if _, err := os.Stat(capsPath); err != nil {
		t.Errorf("expected %s, got %v", capsPath, err)
	}
}
