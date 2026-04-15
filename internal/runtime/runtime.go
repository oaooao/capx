package runtime

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/server"
	"github.com/oaooao/capx/internal/config"
)

// CapabilityStatus represents the runtime status of a capability.
type CapabilityStatus string

const (
	StatusEnabled  CapabilityStatus = "enabled"
	StatusDisabled CapabilityStatus = "disabled"
	StatusFailed   CapabilityStatus = "failed"
)

// CapabilityInfo holds runtime info about a capability.
type CapabilityInfo struct {
	Name        string           `json:"name"`
	Type        string           `json:"type"`
	Status      CapabilityStatus `json:"status"`
	Description string           `json:"description"`
	Error       string           `json:"error,omitempty"`
}

// Runtime manages capability lifecycle.
type Runtime struct {
	cfg       *config.Config
	mcpServer *server.MCPServer
	logger    *log.Logger

	mu       sync.RWMutex
	adapters map[string]Adapter // name -> active adapter
	failed   map[string]string  // name -> error message
}

// Adapter is the interface for capability backends (MCP, CLI).
type Adapter interface {
	// Start connects/spawns the capability and returns tools to register.
	Start(ctx context.Context) ([]server.ServerTool, error)
	// Stop terminates the capability.
	Stop() error
	// Tools returns currently registered tool names.
	ToolNames() []string
}

// New creates a new Runtime.
func New(cfg *config.Config, mcpServer *server.MCPServer, logger *log.Logger) *Runtime {
	return &Runtime{
		cfg:       cfg,
		mcpServer: mcpServer,
		logger:    logger,
		adapters:  make(map[string]Adapter),
		failed:    make(map[string]string),
	}
}

// EnableByScene enables all capabilities for a scene.
func (r *Runtime) EnableByScene(ctx context.Context, sceneName string) error {
	names, err := r.cfg.ResolveScene(sceneName)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := r.Enable(ctx, name); err != nil {
			r.logger.Printf("warning: failed to enable %s: %v", name, err)
		}
	}
	return nil
}

// Enable activates a capability by name.
func (r *Runtime) Enable(ctx context.Context, name string) error {
	cap, ok := r.cfg.Capabilities[name]
	if !ok {
		return fmt.Errorf("capability %q not found in config", name)
	}
	if cap.Disabled {
		return fmt.Errorf("capability %q is disabled", name)
	}

	r.mu.Lock()
	if _, exists := r.adapters[name]; exists {
		r.mu.Unlock()
		return nil // already enabled
	}
	r.mu.Unlock()

	var adapter Adapter
	switch cap.Type {
	case "mcp":
		adapter = NewMCPAdapter(name, cap, r.logger)
	case "cli":
		adapter = NewCLIAdapter(name, cap)
	default:
		return fmt.Errorf("unknown capability type %q for %s", cap.Type, name)
	}

	tools, err := adapter.Start(ctx)
	if err != nil {
		r.mu.Lock()
		r.failed[name] = err.Error()
		r.mu.Unlock()
		return fmt.Errorf("enabling %s: %w", name, err)
	}

	r.mu.Lock()
	r.adapters[name] = adapter
	delete(r.failed, name)
	r.mu.Unlock()

	// Register tools with the MCP server (this triggers list_changed notification).
	if len(tools) > 0 {
		r.mcpServer.AddTools(tools...)
	}

	r.logger.Printf("enabled: %s (%d tools)", name, len(tools))
	return nil
}

// Disable deactivates a capability by name.
func (r *Runtime) Disable(name string) error {
	r.mu.Lock()
	adapter, ok := r.adapters[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("capability %q is not enabled", name)
	}

	toolNames := adapter.ToolNames()
	delete(r.adapters, name)
	r.mu.Unlock()

	// Remove tools first (triggers list_changed).
	if len(toolNames) > 0 {
		r.mcpServer.DeleteTools(toolNames...)
	}

	if err := adapter.Stop(); err != nil {
		r.logger.Printf("warning: error stopping %s: %v", name, err)
	}

	r.logger.Printf("disabled: %s", name)
	return nil
}

// SetScene switches to a different scene: disable everything, then enable scene caps.
func (r *Runtime) SetScene(ctx context.Context, sceneName string) error {
	// Disable all currently enabled capabilities.
	r.mu.RLock()
	var enabled []string
	for name := range r.adapters {
		enabled = append(enabled, name)
	}
	r.mu.RUnlock()

	for _, name := range enabled {
		_ = r.Disable(name)
	}

	return r.EnableByScene(ctx, sceneName)
}

// List returns info about all visible capabilities.
func (r *Runtime) List() []CapabilityInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var infos []CapabilityInfo
	for name, cap := range r.cfg.VisibleCapabilities() {
		info := CapabilityInfo{
			Name:        name,
			Type:        cap.Type,
			Description: cap.Description,
		}

		if _, ok := r.adapters[name]; ok {
			info.Status = StatusEnabled
		} else if errMsg, ok := r.failed[name]; ok {
			info.Status = StatusFailed
			info.Error = errMsg
		} else {
			info.Status = StatusDisabled
		}

		infos = append(infos, info)
	}

	sort.Slice(infos, func(i, j int) bool {
		if infos[i].Status != infos[j].Status {
			order := map[CapabilityStatus]int{StatusEnabled: 0, StatusFailed: 1, StatusDisabled: 2}
			return order[infos[i].Status] < order[infos[j].Status]
		}
		return infos[i].Name < infos[j].Name
	})

	return infos
}

// Shutdown stops all active capabilities.
func (r *Runtime) Shutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, adapter := range r.adapters {
		if err := adapter.Stop(); err != nil {
			r.logger.Printf("error stopping %s: %v", name, err)
		}
	}
	r.adapters = make(map[string]Adapter)
}

// GenerateDescription builds the dynamic server description.
func (r *Runtime) GenerateDescription() string {
	infos := r.List()

	var sb strings.Builder
	sb.WriteString("Agent Capability Runtime (capx)\n")

	var enabled, available []CapabilityInfo
	for _, info := range infos {
		switch info.Status {
		case StatusEnabled:
			enabled = append(enabled, info)
		default:
			available = append(available, info)
		}
	}

	if len(enabled) > 0 {
		sb.WriteString("\n✦ Enabled:\n")
		for _, info := range enabled {
			fmt.Fprintf(&sb, "  %s — %s\n", info.Name, info.Description)
		}
	}

	if len(available) > 0 {
		sb.WriteString("\n✦ Available (use enable tool to activate):\n")
		for _, info := range available {
			fmt.Fprintf(&sb, "  %s — %s\n", info.Name, info.Description)
		}
	}

	sb.WriteString("\nUse the 'list' tool for full details, 'enable'/'disable' to manage capabilities.")
	return sb.String()
}
