package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
			Scenes:       map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{}}},
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

	// Scan for project-level .mcp.json files that may conflict with capx.
	scanProjectMCPFiles(cfg)

	return nil
}

// scanProjectMCPFiles finds .mcp.json files that contain servers already managed by capx
// and warns the user about potential duplicates.
func scanProjectMCPFiles(cfg *config.Config) {
	home, _ := os.UserHomeDir()
	claudePath := filepath.Join(home, ".claude.json")

	// Read claude.json to find project paths.
	data, err := os.ReadFile(claudePath)
	if err != nil {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}

	// Collect project paths from claude.json "projects" key.
	var projectPaths []string
	if projectsRaw, ok := raw["projects"]; ok {
		var projects map[string]json.RawMessage
		if err := json.Unmarshal(projectsRaw, &projects); err == nil {
			for p := range projects {
				projectPaths = append(projectPaths, p)
			}
		}
	}

	// Also check current working directory.
	if cwd, err := os.Getwd(); err == nil {
		projectPaths = append(projectPaths, cwd)
	}

	// Deduplicate.
	seen := make(map[string]bool)
	var unique []string
	for _, p := range projectPaths {
		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}

	// Scan each project for .mcp.json with conflicting servers.
	type conflict struct {
		path    string
		servers []string
	}
	var conflicts []conflict

	for _, projectPath := range unique {
		mcpPath := filepath.Join(projectPath, ".mcp.json")
		mcpData, err := os.ReadFile(mcpPath)
		if err != nil {
			continue
		}

		var mcpFile struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		if err := json.Unmarshal(mcpData, &mcpFile); err != nil {
			continue
		}
		if len(mcpFile.MCPServers) == 0 {
			continue
		}

		// Check for overlaps with capx capabilities.
		var overlapping []string
		for serverName := range mcpFile.MCPServers {
			// Check exact match or prefix match (e.g., "XcodeBuildMCP" matches "XcodeBuildMCP/ios").
			for capName := range cfg.Capabilities {
				baseName := capName
				if idx := strings.Index(capName, "/"); idx > 0 {
					baseName = capName[:idx]
				}
				if serverName == capName || serverName == baseName {
					overlapping = append(overlapping, serverName)
					break
				}
			}
		}

		if len(overlapping) > 0 {
			conflicts = append(conflicts, conflict{path: mcpPath, servers: overlapping})
		}
	}

	if len(conflicts) == 0 {
		return
	}

	fmt.Println("\n⚠ Found project-level .mcp.json files with servers that capx already manages:")
	for _, c := range conflicts {
		fmt.Printf("\n  %s:\n", c.path)
		for _, s := range c.servers {
			fmt.Printf("    - %s\n", s)
		}
	}
	fmt.Println("\n  These will load alongside capx, causing duplicate tools.")
	fmt.Println("  To fix: remove these entries from the .mcp.json files above,")
	fmt.Println("  or clear them with: echo '{\"mcpServers\":{}}' > <path>")
}
