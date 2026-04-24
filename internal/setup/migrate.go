package setup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// MigrateOptions controls `capx migrate` behavior (§A.14).
type MigrateOptions struct {
	// GlobalDir defaults to the XDG/~/.config/capx path. Tests override.
	GlobalDir string
	DryRun    bool
	Stdout    io.Writer
}

// MigrateReport mirrors the §A.14 JSON report.
type MigrateReport struct {
	Status    string            `json:"status"` // "ok" | "aborted"
	V01Config string            `json:"v01_config"`
	V02Files  []string          `json:"v02_files"`
	Warnings  []MigrateWarning  `json:"warnings"`
	Errors    []string          `json:"errors"`
}

// MigrateWarning is a single structured warning. Fields depend on Kind but
// the stringification preserves everything the user needs to track down
// the source.
type MigrateWarning struct {
	Kind       string `json:"kind"`
	Scene      string `json:"scene,omitempty"`
	Capability string `json:"capability,omitempty"`
	Key        string `json:"key,omitempty"`
	OldValue   string `json:"old_value,omitempty"`
	NewValue   string `json:"new_value,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// Migrate performs the v0.1 → v0.2 config migration per §A.14.
// Returns the report even on error; callers should render it for the user.
func Migrate(opts MigrateOptions) (*MigrateReport, error) {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.GlobalDir == "" {
		opts.GlobalDir = globalConfigDir()
	}
	report := &MigrateReport{Status: "aborted", V02Files: []string{}, Warnings: []MigrateWarning{}, Errors: []string{}}

	// ---- Step 1: resolve real dir + preflight ----
	realDir, err := filepath.EvalSymlinks(opts.GlobalDir)
	if err != nil {
		err = fmt.Errorf("resolve %s: %w", opts.GlobalDir, err)
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}
	legacyPath := filepath.Join(realDir, "config.yaml")
	report.V01Config = legacyPath

	if _, err := os.Stat(legacyPath); err != nil {
		err = fmt.Errorf("legacy config not found at %s", legacyPath)
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}
	// v0.2 structure already present → refuse.
	for _, marker := range []string{"capabilities.yaml", "scenes", "settings.yaml"} {
		if _, err := os.Stat(filepath.Join(realDir, marker)); err == nil {
			err = fmt.Errorf("refusing to migrate: v0.2 structure already present (%s)", marker)
			report.Errors = append(report.Errors, err.Error())
			return report, err
		}
	}

	// ---- Step 2: parse legacy + split + canonicalize ----
	raw, err := os.ReadFile(legacyPath)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}
	var legacy map[string]any
	if err := yaml.Unmarshal(raw, &legacy); err != nil {
		err = fmt.Errorf("parse legacy yaml: %w", err)
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}

	tempDir := filepath.Join(filepath.Dir(realDir),
		fmt.Sprintf("capx.migrating-%d", os.Getpid()))
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}
	// Clean up the temp dir on any non-commit path.
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(tempDir)
		}
	}()

	if err := splitLegacyConfig(legacy, tempDir, report); err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}

	// Enumerate produced files (deterministic ordering).
	var produced []string
	_ = filepath.WalkDir(tempDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		produced = append(produced, path)
		return nil
	})
	sort.Strings(produced)

	// Dry-run short-circuits before any real filesystem mutation of realDir.
	if opts.DryRun {
		report.Status = "ok"
		for _, p := range produced {
			// Translate tempDir paths back to their post-commit locations
			// so the user sees the eventual layout.
			rel, _ := filepath.Rel(tempDir, p)
			report.V02Files = append(report.V02Files, filepath.Join(realDir, rel))
		}
		return report, nil
	}

	// ---- Step 4: two-phase atomic rename ----
	backupDir := filepath.Join(filepath.Dir(realDir),
		fmt.Sprintf("capx.v01-backup-%d", time.Now().Unix()))
	if err := os.Rename(realDir, backupDir); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("rename-backup: %v", err))
		return report, err
	}
	if err := os.Rename(tempDir, realDir); err != nil {
		// Rollback: put the backup back in place so user is in full-v0.1 state.
		rollbackErr := os.Rename(backupDir, realDir)
		report.Errors = append(report.Errors,
			fmt.Sprintf("rename-commit failed: %v; rollback: %v", err, rollbackErr))
		return report, err
	}
	committed = true

	// ---- Step 5: post-commit — preserve config.yaml as .v01.bak next to new dir
	legacyBackup := filepath.Join(backupDir, "config.yaml")
	newLegacy := filepath.Join(realDir, "config.yaml.v01.bak")
	if _, err := os.Stat(legacyBackup); err == nil {
		// Best-effort move; failure here doesn't defeat the migration.
		if err := os.Rename(legacyBackup, newLegacy); err == nil {
			report.V01Config = newLegacy
		} else {
			fmt.Fprintf(opts.Stdout, "warning: could not move %s to %s: %v\n", legacyBackup, newLegacy, err)
		}
	}
	_ = os.RemoveAll(backupDir)

	// Final v02_files list points at the real locations.
	for _, p := range produced {
		rel, _ := filepath.Rel(tempDir, p)
		report.V02Files = append(report.V02Files, filepath.Join(realDir, rel))
	}
	report.Status = "ok"
	return report, nil
}

// splitLegacyConfig writes capabilities.yaml / scenes/*.yaml / settings.yaml
// under tempDir by transforming the legacy map. It performs the A.14
// canonicalization (auto-stringify env/args; default_scene; disabled ref
// detection) and populates report.Warnings.
//
// Returns error only for fail-fast conditions (transport illegal combos).
func splitLegacyConfig(legacy map[string]any, tempDir string, report *MigrateReport) error {
	capsRaw, _ := legacy["capabilities"].(map[string]any)
	scenesRaw, _ := legacy["scenes"].(map[string]any)
	defaultScene, _ := legacy["default_scene"].(string)

	// Canonicalize capabilities.
	capsOut := make(map[string]any, len(capsRaw))
	validNames := make(map[string]bool, len(capsRaw))
	for name, v := range capsRaw {
		cm, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("capability %q: expected map, got %T", name, v)
		}
		canonicalizeCapabilityMap(name, cm, report)
		// Transport vs command/url preflight (§A.14 edge case 3: fail fast).
		if err := legacyTransportPreflight(name, cm); err != nil {
			return err
		}
		capsOut[name] = cm
		validNames[name] = true
	}

	// Inspect scenes for disabled refs & missing refs (warnings only).
	scenesOut := make(map[string]any, len(scenesRaw))
	for name, v := range scenesRaw {
		sm, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("scene %q: expected map, got %T", name, v)
		}
		refs := extractSceneCapRefs(sm)
		for _, ref := range refs {
			if cap, exists := capsRaw[ref].(map[string]any); exists {
				if disabled, _ := cap["disabled"].(bool); disabled {
					report.Warnings = append(report.Warnings, MigrateWarning{
						Kind:       "scene_references_disabled_capability",
						Scene:      name,
						Capability: ref,
					})
				}
			} else {
				report.Warnings = append(report.Warnings, MigrateWarning{
					Kind:       "scene_references_missing_capability",
					Scene:      name,
					Capability: ref,
				})
			}
		}
		scenesOut[name] = sm
	}

	// default_scene handling (§A.14 edge cases 4/5).
	if defaultScene == "" {
		defaultScene = "default"
		report.Warnings = append(report.Warnings, MigrateWarning{
			Kind:   "default_scene_missing",
			Detail: "no default_scene in legacy config; writing default_scene: default to settings.yaml",
		})
	}

	// Write settings.yaml
	settingsPath := filepath.Join(tempDir, "settings.yaml")
	if err := writeYAML(settingsPath, map[string]any{
		"default_scene": defaultScene,
	}); err != nil {
		return err
	}
	// Write capabilities.yaml
	capsPath := filepath.Join(tempDir, "capabilities.yaml")
	if err := writeYAML(capsPath, map[string]any{"capabilities": capsOut}); err != nil {
		return err
	}
	// Write scenes/<name>.yaml — one file per scene.
	scenesDir := filepath.Join(tempDir, "scenes")
	if err := os.MkdirAll(scenesDir, 0o755); err != nil {
		return err
	}
	for name, v := range scenesOut {
		scenePath := filepath.Join(scenesDir, name+".yaml")
		if err := writeYAML(scenePath, v); err != nil {
			return err
		}
	}
	return nil
}

// canonicalizeCapabilityMap applies §A.14 edge cases 6 & 7: stringify non-string
// env values and args elements in-place, emitting one warning per mutation.
func canonicalizeCapabilityMap(name string, cap map[string]any, report *MigrateReport) {
	// env.
	if envRaw, ok := cap["env"].(map[string]any); ok {
		for k, v := range envRaw {
			newVal, mutated, kind := stringifyScalar(v)
			if mutated {
				report.Warnings = append(report.Warnings, MigrateWarning{
					Kind:       kind, // env_value_stringified | env_value_null_to_empty
					Capability: name,
					Key:        k,
					OldValue:   fmt.Sprintf("%v", v),
					NewValue:   newVal,
				})
				envRaw[k] = newVal
			}
		}
	}
	// args.
	if argsRaw, ok := cap["args"].([]any); ok {
		for i, v := range argsRaw {
			newVal, mutated, kind := stringifyScalar(v)
			if mutated {
				k := "args_element_stringified"
				if kind == "env_value_null_to_empty" {
					k = "args_element_null_to_empty"
				}
				report.Warnings = append(report.Warnings, MigrateWarning{
					Kind:       k,
					Capability: name,
					Key:        fmt.Sprintf("args[%d]", i),
					OldValue:   fmt.Sprintf("%v", v),
					NewValue:   newVal,
				})
				argsRaw[i] = newVal
			}
		}
	}
}

// stringifyScalar returns (newValue, wasMutated, warningKind). For null and
// already-string values it reports no mutation — except null maps to "" per
// §A.14 with the stronger "null_to_empty" warning kind.
func stringifyScalar(v any) (string, bool, string) {
	switch x := v.(type) {
	case nil:
		return "", true, "env_value_null_to_empty"
	case string:
		return x, false, ""
	case bool:
		return strconv.FormatBool(x), true, "env_value_stringified"
	case int:
		return strconv.Itoa(x), true, "env_value_stringified"
	case int64:
		return strconv.FormatInt(x, 10), true, "env_value_stringified"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true, "env_value_stringified"
	default:
		return fmt.Sprintf("%v", x), true, "env_value_stringified"
	}
}

// legacyTransportPreflight enforces §A.14 edge case 3: explicit transport vs
// command/url combos that would fail under A.5's strict rules in v0.2 must
// abort migration rather than be silently reshaped.
func legacyTransportPreflight(name string, cap map[string]any) error {
	capType, _ := cap["type"].(string)
	if capType != "mcp" {
		return nil
	}
	transport, _ := cap["transport"].(string)
	command, _ := cap["command"].(string)
	url, _ := cap["url"].(string)

	if command != "" && url != "" {
		return fmt.Errorf(
			"capability %q: legacy config has BOTH command and url for type: mcp; "+
				"fix in legacy config.yaml before migrating", name)
	}
	if command == "" && url == "" {
		return fmt.Errorf(
			"capability %q: type: mcp requires command or url; "+
				"fix in legacy config.yaml before migrating", name)
	}
	if transport == "stdio" && command == "" {
		return fmt.Errorf(
			"capability %q: transport: stdio conflicts with url field; "+
				"fix in legacy config.yaml before migrating", name)
	}
	if transport == "http" && url == "" {
		return fmt.Errorf(
			"capability %q: transport: http conflicts with command field; "+
				"fix in legacy config.yaml before migrating", name)
	}
	return nil
}

// extractSceneCapRefs returns every capability name a scene's auto_enable
// references, tolerating both the flat list and the required/optional split.
func extractSceneCapRefs(scene map[string]any) []string {
	var out []string
	collect := func(v any) {
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				if s, ok := e.(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	switch ae := scene["auto_enable"].(type) {
	case []any:
		collect(ae)
	case map[string]any:
		collect(ae["required"])
		collect(ae["optional"])
	}
	return out
}

func writeYAML(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	header := []byte(fmt.Sprintf("# generated by capx migrate at %s\n", time.Now().UTC().Format(time.RFC3339)))
	if strings.HasSuffix(path, "settings.yaml") {
		header = append(header, []byte("# field-level overlay at runtime; see A.3\n")...)
	}
	return os.WriteFile(path, append(header, data...), 0o644)
}
