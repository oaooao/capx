package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the top-level capx configuration.
type Config struct {
	Capabilities map[string]*Capability `yaml:"capabilities"`
	Scenes       map[string]*Scene      `yaml:"scenes"`
	DefaultScene string                 `yaml:"default_scene"`
}

// Capability defines a single capability (MCP server or CLI tool).
type Capability struct {
	Type        string            `yaml:"type"`                  // "mcp" or "cli"
	Transport   string            `yaml:"transport,omitempty"`   // "stdio" or "http" (for mcp type)
	Command     string            `yaml:"command,omitempty"`     // command to spawn (stdio mcp or cli)
	Args        []string          `yaml:"args,omitempty"`        // command args
	URL         string            `yaml:"url,omitempty"`         // URL for http transport
	Env         map[string]string `yaml:"env,omitempty"`         // extra environment variables
	Description string            `yaml:"description,omitempty"` // human-readable description
	Tags        []string          `yaml:"tags,omitempty"`        // tags for categorization
	Disabled    bool              `yaml:"disabled,omitempty"`    // if true, completely invisible
	Tools       map[string]*CLITool `yaml:"tools,omitempty"`     // CLI tool definitions
}

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
type Scene struct {
	AutoEnable []string `yaml:"auto_enable"`
}

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "capx", "config.yaml")
}

// Load reads and parses a config file from the given path.
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

	return &cfg, nil
}

// Save writes the config to the given path.
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
func (c *Config) ResolveScene(sceneName string) ([]string, error) {
	scene, ok := c.Scenes[sceneName]
	if !ok {
		return nil, fmt.Errorf("scene %q not found", sceneName)
	}

	if len(scene.AutoEnable) == 1 && scene.AutoEnable[0] == "all" {
		var names []string
		for name, cap := range c.Capabilities {
			if !cap.Disabled {
				names = append(names, name)
			}
		}
		return names, nil
	}

	return scene.AutoEnable, nil
}
