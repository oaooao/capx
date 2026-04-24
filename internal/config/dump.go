package config

import (
	"fmt"
	"sort"
	"time"
)

// DumpSchemaVersion is the current supported schema version. Bumps are
// reserved for breaking changes (field removal, type change); additive
// changes do NOT bump the version per §C.2.
const DumpSchemaVersion = 1

// DumpResult is the full capx dump v1 payload (§C.2). Consumers (prompt-easy
// / typefree / CI validators) deserialize this as the authoritative view of
// merged configuration — they should not reimplement A.5/A.6/A.7/A.8 merge
// rules.
type DumpResult struct {
	SchemaVersion int             `json:"schema_version"`
	GeneratedAt   time.Time       `json:"generated_at"`
	CapxVersion   string          `json:"capx_version"`
	ConfigSources []SourceRef     `json:"config_sources"`
	DefaultScene  string          `json:"default_scene"`
	ActiveScene   *string         `json:"active_scene"`
	Capabilities  map[string]*DumpCapability `json:"capabilities"`
	Scenes        map[string]*DumpScene      `json:"scenes"`
	Warnings      []DumpWarning   `json:"warnings"`
}

// SourceRef ties a merged element back to the file it came from, listed in
// the order the scope merge walked them (lowest precedence first). Present
// only for sources that actually contributed.
type SourceRef struct {
	Layer string `json:"layer"`
	Path  string `json:"path"`
}

// DumpCapability is one merged cap's surface. Fields mirror the Capability
// struct but with normalized empty-vs-null semantics matching the hash spec
// (§A.12): absent/empty collections serialize as null for deterministic
// downstream consumption.
type DumpCapability struct {
	Type         string              `json:"type"`
	Transport    *string             `json:"transport"`
	Command      *string             `json:"command"`
	Args         []string            `json:"args"`
	URL          *string             `json:"url"`
	Env          map[string]string   `json:"env"`
	Tools        map[string]*CLITool `json:"tools"`
	Description  *string             `json:"description"`
	Aliases      []string            `json:"aliases"`
	Keywords     []string            `json:"keywords"`
	Tags         []string            `json:"tags"`
	Disabled     bool                `json:"disabled"`
	RequiredEnv  []string            `json:"required_env"`
	ProcessHash  string              `json:"process_hash"`
	ToolsHash    string              `json:"tools_hash"`
	Source       string              `json:"source"`
	OverriddenBy *OverrideRef        `json:"overridden_by"`
}

// OverrideRef is populated when a cap was shadowed by a higher-precedence
// definition — useful for UIs that want to show "this lower layer exists but
// project overrides it". Currently the merge is destructive (replace), so
// this field is reserved for a future non-destructive merge mode and stays
// null in v0.2.
type OverrideRef struct {
	Layer string `json:"layer"`
	Name  string `json:"name"`
}

// DumpScene surfaces a scene with extends pre-resolved into
// extends_resolved (the §A.6 linearization) so consumers don't have to
// recompute it.
type DumpScene struct {
	Description           string     `json:"description"`
	Extends               []string   `json:"extends"`
	ExtendsResolved       []string   `json:"extends_resolved"`
	AutoEnable            AutoEnable `json:"auto_enable"`
	InlineCapabilityNames []string   `json:"inline_capability_names"`
	Source                string     `json:"source"`
}

// DumpWarning is a structured form of the warnings capx prints to stderr at
// startup — alias conflicts, override events, etc.
type DumpWarning struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

// Dump produces the DumpResult for the given config.
//
// If sceneName is empty, Capabilities reflects the cross-scope merged view
// without scene inline overrides, and ActiveScene stays nil. If sceneName is
// non-empty, its extends are resolved and inline overrides applied — the
// returned Capabilities map for that scene's caps may differ from the
// scene-less view.
//
// capxVersion is threaded in from main so tests don't depend on go build info.
func Dump(cfg *Config, sceneName, capxVersion string) (*DumpResult, error) {
	out := &DumpResult{
		SchemaVersion: DumpSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		CapxVersion:   capxVersion,
		DefaultScene:  cfg.DefaultScene,
		Capabilities:  map[string]*DumpCapability{},
		Scenes:        map[string]*DumpScene{},
		Warnings:      []DumpWarning{},
	}

	// Config sources — emit each scope root that contributed.
	layerOrder := []ScopeKind{ScopeKindGlobal, ScopeKindProject}
	for _, layer := range layerOrder {
		if root, ok := cfg.ScopeRoots[layer]; ok && root != "" {
			out.ConfigSources = append(out.ConfigSources, SourceRef{
				Layer: string(layer),
				Path:  root,
			})
		}
	}

	// If scene requested, set ActiveScene and swap the cap map over to
	// scene-aware view.
	capView := cfg.Capabilities
	if sceneName != "" {
		expanded, err := cfg.ExpandScene(sceneName)
		if err != nil {
			return nil, err
		}
		out.ActiveScene = &sceneName
		// Merge global + scene inline into a fresh map without mutating cfg.
		capView = make(map[string]*Capability, len(cfg.Capabilities)+len(expanded.Capabilities))
		for n, c := range cfg.Capabilities {
			capView[n] = c
		}
		for n, c := range expanded.Capabilities {
			capView[n] = c
		}
	}

	for name, c := range capView {
		fp, err := Fingerprints(c)
		if err != nil {
			return nil, fmt.Errorf("fingerprint %s: %w", name, err)
		}
		dc := &DumpCapability{
			Type:        c.Type,
			Transport:   ptrIfNonEmpty(c.Transport),
			Command:     ptrIfNonEmpty(c.Command),
			Args:        normSlice(c.Args),
			URL:         ptrIfNonEmpty(c.URL),
			Env:         normMap(c.Env),
			Tools:       normToolMap(c.Tools),
			Description: ptrIfNonEmpty(c.Description),
			Aliases:     normSlice(c.Aliases),
			Keywords:    normSlice(c.Keywords),
			Tags:        normSlice(c.Tags),
			Disabled:    c.Disabled,
			RequiredEnv: normSlice(c.RequiredEnv),
			ProcessHash: "sha256:" + fp.ProcessHash,
			ToolsHash:   "sha256:" + fp.ToolsHash,
			Source:      string(c.Source),
		}
		// Inline scene capability gets the scene: prefix for clarity.
		if c.Source == SourceSceneInline && sceneName != "" {
			dc.Source = fmt.Sprintf("scene:%s (inline)", sceneName)
		}
		out.Capabilities[name] = dc
	}

	for name, s := range cfg.Scenes {
		inlineNames := make([]string, 0, len(s.Capabilities))
		for n := range s.Capabilities {
			inlineNames = append(inlineNames, n)
		}
		sort.Strings(inlineNames)

		expanded, err := cfg.ExpandScene(name)
		var resolved []string
		if err == nil {
			resolved = expanded.Lineage
		}
		out.Scenes[name] = &DumpScene{
			Description:           s.Description,
			Extends:               s.Extends,
			ExtendsResolved:       resolved,
			AutoEnable:            s.AutoEnable,
			InlineCapabilityNames: inlineNames,
			Source:                string(s.Source),
		}
	}

	for _, w := range cfg.Warnings {
		out.Warnings = append(out.Warnings, DumpWarning{
			Kind: w.Kind, Message: w.Message, Path: w.Path,
		})
	}
	return out, nil
}

// ----- canonicalization helpers shared with hash.go intent -----

func ptrIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func normSlice(v []string) []string {
	if len(v) == 0 {
		return nil
	}
	return v
}

func normMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

func normToolMap(m map[string]*CLITool) map[string]*CLITool {
	if len(m) == 0 {
		return nil
	}
	return m
}
