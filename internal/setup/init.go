package setup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// InitOptions configures `capx init` behavior (§A.9).
type InitOptions struct {
	// Target directory layer — "project" (default) or "global".
	Global bool
	// Sub-operations (any combination allowed with --global).
	AddScenes bool
	Agent     string // "", "claude-code", "codex"
	// Bypass the "already-in-scope" check for `capx init` without flags and
	// the legacy-config check for `capx init --global`. Per §A.9 --force is
	// a narrow escape hatch, NOT "overwrite everything".
	Force bool
	// Cwd is the effective working directory. Injected for testability; if
	// empty, os.Getwd() is used.
	Cwd string
	// Stdout collects human-facing output; defaults to os.Stdout.
	Stdout io.Writer
}

// Init dispatches to the appropriate init path. §A.9 semantics:
//
//   - no flags + no --global: create project-layer .capx/capabilities.yaml
//     (rejects if pwd is already inside any project scope, or inside the
//     global config dir)
//   - --add-scenes: in-scope increment; creates scenes/ + sample files
//   - --agent: register capx in the named agent's config (delegates to
//     existing SetupClaudeCode / SetupCodex)
//   - --global: operate on ~/.config/capx/ instead of pwd
func Init(opts InitOptions) error {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Cwd == "" {
		pwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
		opts.Cwd = pwd
	}

	// --agent runs standalone; other flags may be combined with it but do
	// not block it (agent registration is orthogonal to scope creation).
	if opts.Agent != "" {
		if err := runAgentSetup(opts.Agent); err != nil {
			return err
		}
	}

	switch {
	case opts.AddScenes:
		return initAddScenes(opts)
	case opts.Agent != "":
		// Only --agent was requested; we already handled it above.
		return nil
	default:
		return initCreateScope(opts)
	}
}

// initCreateScope is the "capx init" (no flags) path — create capabilities.yaml.
// Performs §A.9 Step 1 (in-scope check) and Step 1.5 (global-dir check).
func initCreateScope(opts InitOptions) error {
	targetDir, err := resolveInitTargetDir(opts)
	if err != nil {
		return err
	}

	// Step 1 — detect in-scope (project-layer only; global fallback does NOT
	// count per §A.9).
	if !opts.Global {
		scopeDir := findProjectScope(opts.Cwd)
		if scopeDir != "" && !opts.Force {
			return fmt.Errorf(
				"already inside an existing project scope at %s\n"+
					"  → either cd out of it, use `capx init --add-scenes` to extend, "+
					"or pass --force to create a nested scope intentionally",
				scopeDir,
			)
		}
	}

	// Step 1.5 — pwd-inside-global-dir check; --force does NOT bypass this.
	globalDir := globalConfigDir()
	globalReal, _ := filepath.EvalSymlinks(globalDir)
	pwdReal, _ := filepath.EvalSymlinks(opts.Cwd)
	if !opts.Global && globalReal != "" && pwdReal != "" {
		if pwdReal == globalReal || strings.HasPrefix(pwdReal, globalReal+string(os.PathSeparator)) {
			return fmt.Errorf(
				"pwd is inside the global config directory (%s)\n"+
					"  → creating a nested .capx here would be invisible to capx; "+
					"use `capx init --global` to manage the global layer",
				globalDir,
			)
		}
	}

	// Step 2 — never overwrite existing files.
	capsFile := filepath.Join(targetDir, "capabilities.yaml")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetDir, err)
	}
	if _, err := os.Stat(capsFile); err == nil {
		return fmt.Errorf("refusing to overwrite existing %s (remove it first if intentional)", capsFile)
	}

	const stub = `# capx capabilities
# Declare MCP servers and CLI tools here, then reference them from scenes.
# See https://capx.dev/docs/capabilities for the schema.

capabilities: {}
`
	if err := os.WriteFile(capsFile, []byte(stub), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", capsFile, err)
	}
	fmt.Fprintf(opts.Stdout, "created %s\n", capsFile)
	return nil
}

// initAddScenes is the "capx init --add-scenes" path. Allowed in-scope;
// creates scenes/ and a default sample if missing.
func initAddScenes(opts InitOptions) error {
	var baseDir string
	if opts.Global {
		baseDir = globalConfigDir()
	} else {
		// Attach to the nearest project scope if we're inside one; otherwise
		// require an explicit `capx init` first.
		scopeDir := findProjectScope(opts.Cwd)
		if scopeDir == "" {
			return fmt.Errorf(
				"no project scope found from %s\n"+
					"  → run `capx init` here first (or pass --global to target ~/.config/capx/)",
				opts.Cwd,
			)
		}
		baseDir = filepath.Join(scopeDir, ".capx")
	}

	scenesDir := filepath.Join(baseDir, "scenes")
	if err := os.MkdirAll(scenesDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", scenesDir, err)
	}

	defaultScene := filepath.Join(scenesDir, "default.yaml")
	if _, err := os.Stat(defaultScene); err == nil {
		// Don't overwrite; report no-op.
		fmt.Fprintf(opts.Stdout, "scenes/ present at %s; nothing to do\n", baseDir)
		return nil
	}
	const stub = `# Default scene — loaded when CAPX_SCENE is unset.
description: "Default workbench"
auto_enable: []
`
	if err := os.WriteFile(defaultScene, []byte(stub), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", defaultScene, err)
	}
	fmt.Fprintf(opts.Stdout, "created %s\n", defaultScene)
	return nil
}

// runAgentSetup delegates to the existing agent-specific setup functions.
func runAgentSetup(agent string) error {
	switch agent {
	case "claude-code", "claude":
		return SetupClaudeCode("")
	case "codex":
		return SetupCodex("")
	default:
		return fmt.Errorf("unknown agent %q (expected claude-code | codex)", agent)
	}
}

// ------------------------------------------------------------------
// helpers
// ------------------------------------------------------------------

// resolveInitTargetDir returns the directory under which capabilities.yaml
// will be created, honoring --global vs project-local.
func resolveInitTargetDir(opts InitOptions) (string, error) {
	if opts.Global {
		return globalConfigDir(), nil
	}
	return filepath.Join(opts.Cwd, ".capx"), nil
}

// findProjectScope walks up from dir looking for a .capx/ directory.
// Returns the PARENT directory containing the .capx/ (i.e. the "project root"),
// or "" if none found.
func findProjectScope(dir string) string {
	cur := dir
	for {
		candidate := filepath.Join(cur, ".capx")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// globalConfigDir mirrors the config package's discovery logic: prefer
// $XDG_CONFIG_HOME, else ~/.config/capx.
func globalConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "capx")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "capx")
}
