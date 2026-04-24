package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the top-level capx configuration (v0.1 single-file layout).
//
// v0.2 introduces a directory layout (see scope.go). Config remains the
// in-memory shape produced by both paths after load-and-merge.
type Config struct {
	Capabilities map[string]*Capability `yaml:"capabilities"`
	Scenes       map[string]*Scene      `yaml:"scenes"`
	DefaultScene string                 `yaml:"default_scene"`

	// Settings holds additional global settings (v0.2). In the single-file
	// legacy layout this is populated from the top-level YAML; in the
	// directory layout it is populated from settings.yaml.
	Settings *Settings `yaml:"-"`

	// Warnings collected during load/merge (v0.2). Not serialized.
	Warnings []Warning `yaml:"-"`

	// ScopeRoots records the resolved absolute paths of each contributing
	// scope (global, project, CAPX_HOME, or legacy single-file). Used by
	// `capx where` to render the merge trace. Not serialized.
	ScopeRoots map[ScopeKind]string `yaml:"-"`
}

// Settings holds global settings stored in settings.yaml (v0.2).
// Cross-scope merge is field-level overlay (A.3 of v0.2 design).
type Settings struct {
	DefaultScene string `yaml:"default_scene,omitempty"`
	// Reserved for future: log_level, capability_timeout, etc.
}

// Capability defines a single capability (MCP server or CLI tool).
type Capability struct {
	Type        string              `yaml:"type"`                  // "mcp" or "cli"
	Transport   string              `yaml:"transport,omitempty"`   // "stdio" or "http" (for mcp type)
	Command     string              `yaml:"command,omitempty"`     // command to spawn (stdio mcp or cli)
	Args        []string            `yaml:"args,omitempty"`        // command args
	URL         string              `yaml:"url,omitempty"`         // URL for http transport
	Env         map[string]string   `yaml:"env,omitempty"`         // extra environment variables
	Description string              `yaml:"description,omitempty"` // human-readable description
	Tags        []string            `yaml:"tags,omitempty"`        // tags for categorization
	Disabled    bool                `yaml:"disabled,omitempty"`    // if true, completely invisible
	Tools       map[string]*CLITool `yaml:"tools,omitempty"`       // CLI tool definitions

	// v0.2 additions
	Aliases     []string `yaml:"aliases,omitempty"`      // hard-match aliases (typefree fast path)
	Keywords    []string `yaml:"keywords,omitempty"`     // soft-match keywords (capx search)
	RequiredEnv []string `yaml:"required_env,omitempty"` // pre-start env var checks

	// Source tracks the scope layer a capability was loaded from (v0.2).
	// Populated by scope merge (see merge.go); not serialized.
	Source SourceLayer `yaml:"-"`
}

// SourceLayer identifies which scope layer a capability (or other element)
// was loaded from. Used for trace display (capx where / dump).
type SourceLayer string

const (
	SourceGlobal      SourceLayer = "global"       // ~/.config/capx/capabilities.yaml
	SourceGlobalD     SourceLayer = "global.d"     // ~/.config/capx/capabilities.d/*.yaml
	SourceProject     SourceLayer = "project"      // $PROJECT/.capx/capabilities.yaml
	SourceProjectD    SourceLayer = "project.d"    // $PROJECT/.capx/capabilities.d/*.yaml
	SourceSceneInline SourceLayer = "scene.inline" // capability defined inline in a scene file
	SourceLegacy      SourceLayer = "legacy"       // v0.1 single-file config.yaml
)

// CLITool defines a single tool exposed by a CLI capability.
type CLITool struct {
	Description string               `yaml:"description"`
	Args        []string             `yaml:"args"`
	Params      map[string]*CLIParam `yaml:"params,omitempty"`
}

// CLIParam defines a parameter for a CLI tool.
type CLIParam struct {
	Type        string   `yaml:"type"`
	Required    bool     `yaml:"required,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Enum        []string `yaml:"enum,omitempty"`
}

// Scene defines a named set of auto-enabled capabilities.
//
// v0.2 extends this beyond a flat AutoEnable list with:
//   - Description / Aliases / Tags (metadata)
//   - Extends (inheritance chain)
//   - Capabilities (inline definitions, highest precedence)
//   - AutoEnable with optional required/optional split
type Scene struct {
	Description string                 `yaml:"description,omitempty"`
	Extends     []string               `yaml:"extends,omitempty"`
	Capabilities map[string]*Capability `yaml:"capabilities,omitempty"`
	Aliases     []string               `yaml:"aliases,omitempty"`
	Tags        []string               `yaml:"tags,omitempty"`

	// AutoEnable accepts two forms (decoded via UnmarshalYAML):
	//   - flat list: auto_enable: [a, b, c]                  (all optional)
	//   - split:     auto_enable: { required: [..], optional: [..] }
	AutoEnable AutoEnable `yaml:"auto_enable"`

	// Source tracks scope layer (global or project). Not serialized.
	Source SourceLayer `yaml:"-"`
}

// AutoEnable represents the effective auto-enable capability list for a scene,
// supporting the two YAML surface forms.
type AutoEnable struct {
	Required []string
	Optional []string
}

// UnmarshalYAML implements two surface forms:
//
//	auto_enable: [a, b, c]           → all optional
//	auto_enable: { required: [...], optional: [...] }
func (ae *AutoEnable) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		var list []string
		if err := node.Decode(&list); err != nil {
			return fmt.Errorf("auto_enable (list form): %w", err)
		}
		ae.Optional = list
		return nil
	case yaml.MappingNode:
		var split struct {
			Required []string `yaml:"required"`
			Optional []string `yaml:"optional"`
		}
		if err := node.Decode(&split); err != nil {
			return fmt.Errorf("auto_enable (split form): %w", err)
		}
		ae.Required = split.Required
		ae.Optional = split.Optional
		return nil
	default:
		return fmt.Errorf("auto_enable: unsupported YAML node kind %v (expected list or mapping)", node.Kind)
	}
}

// All returns required + optional combined (deduplicated, required first).
func (ae AutoEnable) All() []string {
	seen := make(map[string]bool, len(ae.Required)+len(ae.Optional))
	var out []string
	for _, n := range ae.Required {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, n := range ae.Optional {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// IsRequired returns true if the given capability name is marked required.
func (ae AutoEnable) IsRequired(name string) bool {
	for _, n := range ae.Required {
		if n == name {
			return true
		}
	}
	return false
}

// DefaultConfigPath returns the default v0.1 single-file config path.
// Retained for v0.1 compatibility; v0.2 uses DiscoverConfig (see scope.go).
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "capx", "config.yaml")
}

// Load reads and parses a v0.1 single-file config from the given path.
//
// v0.2 directory layout is loaded via LoadDirectory / LoadMerged (see scope.go).
// Load remains for v0.1 compatibility and for callers that hold an explicit
// path to a single YAML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if cfg.Capabilities == nil {
		cfg.Capabilities = make(map[string]*Capability)
	}
	if cfg.Scenes == nil {
		cfg.Scenes = make(map[string]*Scene)
	}
	if cfg.DefaultScene == "" {
		cfg.DefaultScene = "default"
	}
	for _, c := range cfg.Capabilities {
		c.Source = SourceLegacy
	}
	for _, s := range cfg.Scenes {
		s.Source = SourceLegacy
	}

	return &cfg, nil
}

// Save writes the config to the given path (v0.1 single-file format).
func Save(cfg *Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	return nil
}

// VisibleCapabilities returns capabilities that are not disabled.
func (c *Config) VisibleCapabilities() map[string]*Capability {
	result := make(map[string]*Capability)
	for name, cap := range c.Capabilities {
		if !cap.Disabled {
			result[name] = cap
		}
	}
	return result
}

// ResolveScene returns the list of capability names for a scene.
// The special value "all" means all visible capabilities.
//
// v0.2 extension: if the scene has Extends, the caller is expected to have
// pre-resolved extensions via ResolveSceneExtends (see scene_extends.go, B2).
// ResolveScene here only reads the scene's own AutoEnable field; it does not
// recursively walk extends. This keeps v0.1 behavior intact; v0.2 callers
// use the higher-level EffectiveScene.
func (c *Config) ResolveScene(sceneName string) ([]string, error) {
	scene, ok := c.Scenes[sceneName]
	if !ok {
		return nil, fmt.Errorf("scene %q not found", sceneName)
	}

	all := scene.AutoEnable.All()

	// Legacy "all" sentinel: a single-element list containing "all".
	if len(all) == 1 && all[0] == "all" {
		var names []string
		for name, cap := range c.Capabilities {
			if !cap.Disabled {
				names = append(names, name)
			}
		}
		return names, nil
	}

	return all, nil
}
