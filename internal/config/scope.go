package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ScopeKind identifies the kind of a configuration scope.
type ScopeKind string

const (
	ScopeKindGlobal  ScopeKind = "global"
	ScopeKindProject ScopeKind = "project"
)

// Scope is the loaded content of a single configuration directory
// (e.g. ~/.config/capx/ or $PROJECT/.capx/). Cross-scope merge happens
// in LoadMerged (B1.3).
type Scope struct {
	Kind         ScopeKind
	RootDir      string                 // canonical absolute path
	Capabilities map[string]*Capability // with SourceLayer stamped
	Scenes       map[string]*Scene      // with SourceLayer stamped
	Settings     *Settings              // nil if settings.yaml absent
	Warnings     []Warning              // intra-scope override warnings (from `.d`)

	// AllowOverride collected from scene files: filename → capability names
	// that may override without warning. Used by B1.3 cross-scope merge.
	AllowOverride map[string][]string
}

// Warning is a non-fatal issue discovered during load.
type Warning struct {
	Kind    string
	Path    string
	Message string
}

// DiscoverResult lists all candidate config paths for the current invocation,
// ordered from lowest to highest priority.
type DiscoverResult struct {
	// CAPXHome, if non-empty, overrides everything else (explicit escape hatch).
	CAPXHome string

	// Global is the XDG/global scope directory. Empty if neither the directory
	// nor the legacy single-file exists.
	Global string

	// Project is the nearest `.capx/` directory walking up from PWD. Empty if
	// none found before reaching the filesystem root.
	Project string

	// LegacyGlobalSingleFile is the path to ~/.config/capx/config.yaml if it
	// exists (v0.1 single-file compat). Mutually exclusive with a v0.2 new
	// directory structure in the global scope (enforced by LoadMerged in B1.3).
	LegacyGlobalSingleFile string
}

// DiscoverConfig resolves which scopes to load based on environment and $PWD.
//
// Priority (high → low):
//  1. $CAPX_HOME — explicit override; if set and points to a valid directory,
//     it becomes the ONLY scope (no merge).
//  2. Project — nearest `.capx/` walking up from pwd.
//  3. Global — $XDG_CONFIG_HOME/capx/ or fallback $HOME/.config/capx/.
//
// Legacy v0.1 single-file (~/.config/capx/config.yaml) is reported separately
// in LegacyGlobalSingleFile and combined with Global by LoadMerged as the
// legacy global scope when the new directory structure is absent.
func DiscoverConfig(pwd string) (*DiscoverResult, error) {
	res := &DiscoverResult{}

	// 1. CAPX_HOME
	if home := strings.TrimSpace(os.Getenv("CAPX_HOME")); home != "" {
		abs, err := filepath.Abs(home)
		if err != nil {
			return nil, fmt.Errorf("CAPX_HOME: %w", err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("CAPX_HOME %q: %w", abs, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("CAPX_HOME %q is not a directory", abs)
		}
		res.CAPXHome = abs
		return res, nil
	}

	// 2. Project
	if projectDir := FindProjectScope(pwd); projectDir != "" {
		res.Project = projectDir
	}

	// 3. Global
	globalDir := GlobalConfigDir()
	if _, err := os.Stat(globalDir); err == nil {
		res.Global = globalDir
	}
	legacy := filepath.Join(globalDir, "config.yaml")
	if info, err := os.Stat(legacy); err == nil && !info.IsDir() {
		res.LegacyGlobalSingleFile = legacy
	}

	return res, nil
}

// GlobalConfigDir returns the global config directory: $XDG_CONFIG_HOME/capx
// or $HOME/.config/capx. The directory is NOT guaranteed to exist.
func GlobalConfigDir() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "capx")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "capx")
}

// FindProjectScope walks up from start looking for a `.capx/` directory.
// Returns the canonical absolute path of the first match, or "" if none.
//
// This is the scope-DISCOVERY variant used at runtime. For `capx init` scope
// checks, see InitScopeCheck (A.9 Step 1) which only cares about project
// scope (not global fallback).
func FindProjectScope(start string) string {
	if start == "" {
		return ""
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for dir := abs; ; {
		candidate := filepath.Join(dir, ".capx")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			// Resolve any symlinks on the .capx/ itself for stable identity.
			real, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return candidate
			}
			return real
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// LoadScope reads all YAML files in the given directory and returns a Scope.
//
// Layout:
//
//	<dir>/
//	├── capabilities.yaml        # main capabilities
//	├── capabilities.d/          # additional cap files (lexicographic scan)
//	│   └── *.yaml
//	├── scenes/                  # scene files (one per file)
//	│   └── *.yaml
//	└── settings.yaml            # global settings (default_scene, …)
//
// Non-existent files are treated as empty; missing directory returns error.
//
// Source stamps:
//   - capabilities.yaml    → SourceGlobal | SourceProject (per kind)
//   - capabilities.d/*.yaml → SourceGlobalD | SourceProjectD
//   - scene inline caps    → SourceSceneInline
//   - scenes themselves    → source set to kind (global/project)
//
// Same-name conflicts within the scope (between capabilities.yaml and .d/, or
// between two .d/ files) produce warnings; the later one (lexicographic for
// .d, capabilities.d/* > capabilities.yaml) wins. Cross-scope conflicts are
// resolved by LoadMerged in B1.3.
func LoadScope(kind ScopeKind, dir string) (*Scope, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("abs path %s: %w", dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}

	scope := &Scope{
		Kind:          kind,
		RootDir:       abs,
		Capabilities:  make(map[string]*Capability),
		Scenes:        make(map[string]*Scene),
		AllowOverride: make(map[string][]string),
	}

	primaryLayer, dLayer := kindLayers(kind)

	// 1. capabilities.yaml (primary)
	primary := filepath.Join(abs, "capabilities.yaml")
	if err := loadCapsFile(primary, primaryLayer, scope); err != nil {
		// Missing primary is OK (scope may only have scenes).
		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	// 2. capabilities.d/*.yaml (lexicographic)
	dDir := filepath.Join(abs, "capabilities.d")
	if files, err := sortedYAMLs(dDir); err == nil {
		for _, f := range files {
			if err := loadCapsFile(f, dLayer, scope); err != nil {
				return nil, err
			}
		}
	}

	// 3. scenes/*.yaml
	sceneDir := filepath.Join(abs, "scenes")
	if files, err := sortedYAMLs(sceneDir); err == nil {
		for _, f := range files {
			if err := loadSceneFile(f, kind, scope); err != nil {
				return nil, err
			}
		}
	}

	// 4. settings.yaml
	settingsPath := filepath.Join(abs, "settings.yaml")
	if data, err := os.ReadFile(settingsPath); err == nil {
		var s Settings
		if err := yaml.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", settingsPath, err)
		}
		scope.Settings = &s
	}

	return scope, nil
}

// kindLayers maps scope kind to (primary capability layer, .d/ layer).
func kindLayers(kind ScopeKind) (SourceLayer, SourceLayer) {
	switch kind {
	case ScopeKindGlobal:
		return SourceGlobal, SourceGlobalD
	case ScopeKindProject:
		return SourceProject, SourceProjectD
	default:
		return SourceLayer(kind), SourceLayer(kind) + ".d"
	}
}

// sortedYAMLs lists *.yaml files in dir, sorted by filename lexicographically.
// Returns an error if dir does not exist.
func sortedYAMLs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	return files, nil
}

// capsFileShape is the YAML shape of a capabilities.yaml / capabilities.d/*.yaml file.
type capsFileShape struct {
	Capabilities map[string]*Capability `yaml:"capabilities"`
}

// loadCapsFile reads a capabilities file, stamps each cap with the given layer,
// and merges into scope.Capabilities (later files override earlier with warnings).
func loadCapsFile(path string, layer SourceLayer, scope *Scope) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var shape capsFileShape
	if err := yaml.Unmarshal(data, &shape); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	// Parse allow-override directives from leading comments (used by B1.3 cross-scope).
	overrides := parseAllowOverride(data)
	if len(overrides) > 0 {
		scope.AllowOverride[path] = overrides
	}
	for name, cap := range shape.Capabilities {
		if cap == nil {
			continue
		}
		cap.Source = layer
		if existing, ok := scope.Capabilities[name]; ok {
			// Intra-scope conflict: same name in capabilities.yaml + .d/, or
			// between two .d/ files. Later wins (we're in lex order); warn.
			scope.Warnings = append(scope.Warnings, Warning{
				Kind: "intra_scope_capability_override",
				Path: path,
				Message: fmt.Sprintf(
					"capability %q from %s (%s) overrides earlier definition from %s",
					name, path, layer, existing.Source,
				),
			})
		}
		scope.Capabilities[name] = cap
	}
	return nil
}

// sceneFileShape is the YAML shape of a single scene file.
// Note: scene file IS the scene (not nested under a "scenes" key).
// Scene name = filename without .yaml extension.
type sceneFileShape struct {
	Description  string                 `yaml:"description,omitempty"`
	Extends      []string               `yaml:"extends,omitempty"`
	Capabilities map[string]*Capability `yaml:"capabilities,omitempty"`
	Aliases      []string               `yaml:"aliases,omitempty"`
	Tags         []string               `yaml:"tags,omitempty"`
	AutoEnable   AutoEnable             `yaml:"auto_enable"`
}

// loadSceneFile reads a single scene file and adds it to scope.Scenes.
// Scene name is derived from the filename (without .yaml/.yml extension).
func loadSceneFile(path string, kind ScopeKind, scope *Scope) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var shape sceneFileShape
	if err := yaml.Unmarshal(data, &shape); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	// Parse allow-override directives from leading comments.
	overrides := parseAllowOverride(data)
	if len(overrides) > 0 {
		scope.AllowOverride[path] = overrides
	}
	name := sceneNameFromPath(path)

	// Stamp inline capability Source = SourceSceneInline.
	if shape.Capabilities != nil {
		for _, c := range shape.Capabilities {
			if c != nil {
				c.Source = SourceSceneInline
			}
		}
	}

	scope.Scenes[name] = &Scene{
		Description:  shape.Description,
		Extends:      shape.Extends,
		Capabilities: shape.Capabilities,
		Aliases:      shape.Aliases,
		Tags:         shape.Tags,
		AutoEnable:   shape.AutoEnable,
		Source:       sourceFromKind(kind),
	}
	return nil
}

// sourceFromKind converts a ScopeKind to its SourceLayer for scene stamping.
func sourceFromKind(kind ScopeKind) SourceLayer {
	switch kind {
	case ScopeKindGlobal:
		return SourceGlobal
	case ScopeKindProject:
		return SourceProject
	default:
		return SourceLayer(kind)
	}
}

// sceneNameFromPath extracts the scene name from a file path (strip dir + ext).
func sceneNameFromPath(path string) string {
	base := filepath.Base(path)
	for _, ext := range []string{".yaml", ".yml"} {
		if strings.HasSuffix(base, ext) {
			return strings.TrimSuffix(base, ext)
		}
	}
	return base
}

// parseAllowOverride scans leading YAML comments for the directive:
//
//	# capx-allow-override: name1, name2, …
//
// Multiple directives are concatenated. Whitespace around names is trimmed.
// Returns the list of capability names whose override will be silenced.
func parseAllowOverride(data []byte) []string {
	var out []string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		// Stop scanning once we hit the first non-comment, non-blank line.
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			break
		}
		// Strip leading # and optional whitespace.
		body := strings.TrimSpace(strings.TrimPrefix(line, "#"))
		const prefix = "capx-allow-override:"
		if !strings.HasPrefix(body, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(body, prefix))
		for _, name := range strings.Split(rest, ",") {
			n := strings.TrimSpace(name)
			if n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}
