package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// LoadMerged is the v0.2 entry point. It discovers all applicable scopes,
// loads each, then merges them into a single Config.
//
// Priority order (low → high):
//  1. Global scope (~/.config/capx/ or $XDG_CONFIG_HOME/capx/)
//  2. Project scope (nearest .capx/ walking up from pwd)
//
// If $CAPX_HOME is set, it replaces BOTH scopes (the directory it points to
// is treated as the single authoritative scope, kind=global).
//
// v0.1 legacy compatibility:
//   - If only a single-file ~/.config/capx/config.yaml exists (no new dir
//     structure), it is loaded as the global scope.
//   - If BOTH the legacy config.yaml AND a v0.2 new structure exist in the
//     global scope, LoadMerged returns an error (A.14 forbids coexistence).
func LoadMerged(pwd string) (*Config, error) {
	disc, err := DiscoverConfig(pwd)
	if err != nil {
		return nil, err
	}

	// CAPX_HOME short-circuits everything.
	if disc.CAPXHome != "" {
		scope, err := LoadScope(ScopeKindGlobal, disc.CAPXHome)
		if err != nil {
			return nil, fmt.Errorf("CAPX_HOME scope %q: %w", disc.CAPXHome, err)
		}
		cfg := mergeScopes(nil, scope)
		cfg.ScopeRoots = map[ScopeKind]string{ScopeKindGlobal: disc.CAPXHome}
		return cfg, nil
	}

	// Global scope handling: detect coexistence and load appropriately.
	var globalScope *Scope
	globalNewExists := disc.Global != "" && hasV02Structure(disc.Global)
	globalLegacyExists := disc.LegacyGlobalSingleFile != ""

	switch {
	case globalNewExists && globalLegacyExists:
		return nil, fmt.Errorf(
			"global scope has BOTH v0.2 directory structure and legacy v0.1 config.yaml at %s\n"+
				"  → run `capx migrate` or remove one of them",
			disc.Global,
		)
	case globalNewExists:
		scope, err := LoadScope(ScopeKindGlobal, disc.Global)
		if err != nil {
			return nil, fmt.Errorf("global scope: %w", err)
		}
		globalScope = scope
	case globalLegacyExists:
		legacyCfg, err := Load(disc.LegacyGlobalSingleFile)
		if err != nil {
			return nil, fmt.Errorf("legacy global config: %w", err)
		}
		globalScope = legacyConfigToScope(legacyCfg, disc.LegacyGlobalSingleFile)
	}

	// Project scope.
	var projectScope *Scope
	if disc.Project != "" {
		scope, err := LoadScope(ScopeKindProject, disc.Project)
		if err != nil {
			return nil, fmt.Errorf("project scope: %w", err)
		}
		projectScope = scope
	}

	// Merge. Priority: project overrides global.
	cfg := mergeScopes(globalScope, projectScope)
	cfg.ScopeRoots = make(map[ScopeKind]string)
	if globalScope != nil {
		cfg.ScopeRoots[ScopeKindGlobal] = globalScope.RootDir
	}
	if projectScope != nil {
		cfg.ScopeRoots[ScopeKindProject] = projectScope.RootDir
	}

	return cfg, nil
}

// hasV02Structure returns true if the directory contains any v0.2-style file
// (capabilities.yaml, scenes/, settings.yaml, capabilities.d/).
func hasV02Structure(dir string) bool {
	checks := []string{
		"capabilities.yaml",
		"scenes",
		"settings.yaml",
		"capabilities.d",
	}
	for _, name := range checks {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// legacyConfigToScope wraps a v0.1 Config into a Scope for uniform merge.
func legacyConfigToScope(cfg *Config, sourcePath string) *Scope {
	scope := &Scope{
		Kind:          ScopeKindGlobal,
		RootDir:       filepath.Dir(sourcePath),
		Capabilities:  make(map[string]*Capability, len(cfg.Capabilities)),
		Scenes:        make(map[string]*Scene, len(cfg.Scenes)),
		AllowOverride: make(map[string][]string),
	}
	for name, c := range cfg.Capabilities {
		// Keep SourceLegacy stamp already set by Load().
		scope.Capabilities[name] = c
	}
	for name, s := range cfg.Scenes {
		scope.Scenes[name] = s
	}
	if cfg.DefaultScene != "" {
		scope.Settings = &Settings{DefaultScene: cfg.DefaultScene}
	}
	return scope
}

// mergeScopes combines the global scope (may be nil) and project scope (may
// be nil) into a single Config. Project overrides global via integer-object
// replace semantics (A.8): high-priority layer COMPLETELY replaces
// low-priority layer for same-name capabilities.
//
// Override warnings are emitted unless the overriding scope file has a
// `# capx-allow-override: <name>` directive.
func mergeScopes(global, project *Scope) *Config {
	cfg := &Config{
		Capabilities: make(map[string]*Capability),
		Scenes:       make(map[string]*Scene),
		ScopeRoots:   make(map[ScopeKind]string),
	}

	// 1. Capabilities merge: global first, then project overrides.
	if global != nil {
		for name, c := range global.Capabilities {
			cfg.Capabilities[name] = c
		}
		cfg.Warnings = append(cfg.Warnings, global.Warnings...)
	}
	if project != nil {
		// Collect all allow-override directives across the scope (flat set).
		allowed := collectAllowOverrides(project)
		for name, c := range project.Capabilities {
			if existing, ok := cfg.Capabilities[name]; ok {
				// Cross-scope override.
				if !allowed[name] {
					cfg.Warnings = append(cfg.Warnings, Warning{
						Kind: "cross_scope_capability_override",
						Path: project.RootDir,
						Message: fmt.Sprintf(
							"capability %q from project (%s) overrides global definition (%s); "+
								"add `# capx-allow-override: %s` to a project .capx file to silence",
							name, c.Source, existing.Source, name,
						),
					})
				}
			}
			cfg.Capabilities[name] = c
		}
		cfg.Warnings = append(cfg.Warnings, project.Warnings...)
	}

	// 2. Scenes merge: project scenes override global scenes by name.
	if global != nil {
		for name, s := range global.Scenes {
			cfg.Scenes[name] = s
		}
	}
	if project != nil {
		for name, s := range project.Scenes {
			if _, ok := cfg.Scenes[name]; ok {
				cfg.Warnings = append(cfg.Warnings, Warning{
					Kind: "cross_scope_scene_override",
					Path: project.RootDir,
					Message: fmt.Sprintf(
						"scene %q from project overrides global definition", name,
					),
				})
			}
			cfg.Scenes[name] = s
		}
	}

	// 3. Settings merge: field-level overlay (project overrides global
	//    per-field; unset project fields inherit global).
	cfg.Settings = mergeSettings(
		settingsOrNil(global),
		settingsOrNil(project),
	)
	if cfg.Settings != nil && cfg.Settings.DefaultScene != "" {
		cfg.DefaultScene = cfg.Settings.DefaultScene
	} else {
		cfg.DefaultScene = "default"
	}

	return cfg
}

// collectAllowOverrides flattens the per-file allow-override map of a scope
// into a set of "names allowed to override".
func collectAllowOverrides(scope *Scope) map[string]bool {
	out := make(map[string]bool)
	for _, names := range scope.AllowOverride {
		for _, n := range names {
			out[n] = true
		}
	}
	return out
}

// settingsOrNil safely extracts *Settings from a possibly-nil Scope.
func settingsOrNil(s *Scope) *Settings {
	if s == nil {
		return nil
	}
	return s.Settings
}

// mergeSettings does field-level overlay: project fields override global
// fields; unset project fields inherit global. Returns nil if both inputs
// are nil.
//
// A.3 of v0.2 design: settings uses field-level overlay (not integer-object
// replace) because settings is a collection of orthogonal scalars.
func mergeSettings(global, project *Settings) *Settings {
	if global == nil && project == nil {
		return nil
	}
	out := &Settings{}
	if global != nil {
		*out = *global
	}
	if project != nil {
		if project.DefaultScene != "" {
			out.DefaultScene = project.DefaultScene
		}
		// Future fields: same pattern — overlay if non-zero.
	}
	return out
}
