package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oaooao/capx/internal/config"
)

// SetupCodex migrates Codex CLI's MCP config to capx.
func SetupCodex(configPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home dir: %w", err)
	}

	codexPath := filepath.Join(home, ".codex", "config.toml")

	data, err := os.ReadFile(codexPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", codexPath, err)
	}

	// Simple TOML parsing for MCP server entries.
	// Codex uses [mcp_servers.<name>] sections.
	lines := strings.Split(string(data), "\n")

	type codexServer struct {
		command string
		args    []string
		url     string
	}

	servers := make(map[string]*codexServer)
	var currentSection string
	var currentServer *codexServer

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[mcp_servers.") && strings.HasSuffix(line, "]") {
			name := strings.TrimPrefix(line, "[mcp_servers.")
			name = strings.TrimSuffix(name, "]")
			currentSection = name
			currentServer = &codexServer{}
			servers[currentSection] = currentServer
		} else if currentServer != nil {
			if strings.HasPrefix(line, "command") {
				val := extractTomlString(line)
				currentServer.command = val
			} else if strings.HasPrefix(line, "args") {
				currentServer.args = extractTomlStringArray(line)
			} else if strings.HasPrefix(line, "url") {
				currentServer.url = extractTomlString(line)
			} else if strings.HasPrefix(line, "[") {
				currentSection = ""
				currentServer = nil
			}
		}
	}

	if len(servers) == 0 {
		fmt.Println("No MCP servers found in ~/.codex/config.toml, nothing to migrate.")
		return nil
	}

	// Load or create capx config.
	cfg, err := config.Load(configPath)
	if err != nil {
		cfg = &config.Config{
			Capabilities: make(map[string]*config.Capability),
			Scenes:       map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{}}},
			DefaultScene: "default",
		}
	}

	var migrated []string
	for name, srv := range servers {
		if name == "capx" {
			continue
		}

		cap := &config.Capability{Type: "mcp"}
		if srv.url != "" {
			cap.Transport = "http"
			cap.URL = srv.url
		} else {
			cap.Transport = "stdio"
			cap.Command = srv.command
			cap.Args = srv.args
		}

		if _, exists := cfg.Capabilities[name]; !exists {
			cfg.Capabilities[name] = cap
			migrated = append(migrated, name)
		}
	}

	if err := config.Save(cfg, configPath); err != nil {
		return fmt.Errorf("saving capx config: %w", err)
	}

	// Backup original config.
	backupDir := filepath.Join(filepath.Dir(configPath), "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return fmt.Errorf("creating backup dir: %w", err)
	}
	backupPath := filepath.Join(backupDir, fmt.Sprintf("codex-config.toml.%s", time.Now().Format("20060102-150405")))
	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return fmt.Errorf("backing up codex config: %w", err)
	}

	// Rewrite the codex config to use capx.
	var newLines []string
	inMCPSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[mcp_servers.") {
			inMCPSection = true
			continue
		}
		if inMCPSection && strings.HasPrefix(trimmed, "[") {
			inMCPSection = false
		}
		if !inMCPSection {
			newLines = append(newLines, line)
		}
	}

	// Add capx as the only MCP server.
	newLines = append(newLines, "", "[mcp_servers.capx]", `command = "capx"`, `args = ["serve"]`)

	if err := os.WriteFile(codexPath, []byte(strings.Join(newLines, "\n")), 0o644); err != nil {
		return fmt.Errorf("writing codex config: %w", err)
	}

	fmt.Printf("Migrated %d MCP servers to capx config:\n", len(migrated))
	for _, name := range migrated {
		fmt.Printf("  + %s\n", name)
	}
	fmt.Println("\n~/.codex/config.toml now points to capx as the single MCP server.")

	return nil
}

func extractTomlString(line string) string {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) < 2 {
		return ""
	}
	val := strings.TrimSpace(parts[1])
	val = strings.Trim(val, `"'`)
	return val
}

func extractTomlStringArray(line string) []string {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) < 2 {
		return nil
	}
	val := strings.TrimSpace(parts[1])
	val = strings.Trim(val, "[]")
	items := strings.Split(val, ",")
	var result []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		item = strings.Trim(item, `"'`)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}
