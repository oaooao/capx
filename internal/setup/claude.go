package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oaooao/capx/internal/config"
)

// ClaudeConfig represents the relevant parts of ~/.claude.json.
type ClaudeConfig struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers,omitempty"`
	Rest       map[string]json.RawMessage `json:"-"` // everything else
}

// SetupClaudeCode migrates Claude Code's MCP config to capx.
func SetupClaudeCode(configPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home dir: %w", err)
	}

	claudePath := filepath.Join(home, ".claude.json")

	// Read existing claude.json.
	data, err := os.ReadFile(claudePath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", claudePath, err)
	}

	// Parse as generic map to preserve unknown fields.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parsing %s: %w", claudePath, err)
	}

	// Extract mcpServers.
	var mcpServers map[string]json.RawMessage
	if servers, ok := raw["mcpServers"]; ok {
		if err := json.Unmarshal(servers, &mcpServers); err != nil {
			return fmt.Errorf("parsing mcpServers: %w", err)
		}
	}

	if len(mcpServers) == 0 {
		fmt.Println("No MCP servers found in ~/.claude.json, nothing to migrate.")
		return nil
	}

	// Load or create capx config.
	cfg, err := config.Load(configPath)
	if err != nil {
		// Create new config if it doesn't exist.
		cfg = &config.Config{
			Capabilities: make(map[string]*config.Capability),
			Scenes:       map[string]*config.Scene{"default": {AutoEnable: []string{}}},
			DefaultScene: "default",
		}
	}

	// Convert Claude MCP servers to capx capabilities.
	var migrated []string
	for name, serverRaw := range mcpServers {
		if name == "capx" {
			continue // skip if capx is already configured
		}

		var serverDef struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
			URL     string   `json:"url"`
			Type    string   `json:"type"` // "stdio" or "sse"
		}
		if err := json.Unmarshal(serverRaw, &serverDef); err != nil {
			fmt.Printf("  skipping %s: cannot parse config\n", name)
			continue
		}

		cap := &config.Capability{
			Type: "mcp",
		}

		if serverDef.URL != "" {
			cap.Transport = "http"
			cap.URL = serverDef.URL
		} else {
			cap.Transport = "stdio"
			cap.Command = serverDef.Command
			cap.Args = serverDef.Args
		}

		if _, exists := cfg.Capabilities[name]; !exists {
			cfg.Capabilities[name] = cap
			migrated = append(migrated, name)
		}
	}

	// Save capx config.
	if err := config.Save(cfg, configPath); err != nil {
		return fmt.Errorf("saving capx config: %w", err)
	}

	// Backup original claude.json.
	backupDir := filepath.Join(filepath.Dir(configPath), "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return fmt.Errorf("creating backup dir: %w", err)
	}
	backupPath := filepath.Join(backupDir, fmt.Sprintf("claude.json.%s", time.Now().Format("20060102-150405")))
	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return fmt.Errorf("backing up claude.json: %w", err)
	}
	fmt.Printf("Backed up ~/.claude.json to %s\n", backupPath)

	// Replace mcpServers with just capx.
	capxEntry := map[string]any{
		"command": "capx",
		"args":    []string{"serve"},
	}
	capxJSON, _ := json.Marshal(capxEntry)
	newServers := map[string]json.RawMessage{
		"capx": capxJSON,
	}
	newServersJSON, _ := json.Marshal(newServers)
	raw["mcpServers"] = newServersJSON

	// Write updated claude.json.
	output, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling claude.json: %w", err)
	}
	if err := os.WriteFile(claudePath, output, 0o644); err != nil {
		return fmt.Errorf("writing claude.json: %w", err)
	}

	fmt.Printf("Migrated %d MCP servers to capx config:\n", len(migrated))
	for _, name := range migrated {
		fmt.Printf("  + %s\n", name)
	}
	fmt.Println("\n~/.claude.json now points to capx as the single MCP server.")

	return nil
}
