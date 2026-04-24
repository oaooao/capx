package runtime

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

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

// activeEntry tracks everything the Runtime needs to know about a currently-
// enabled capability to diff future scene switches against it.
type activeEntry struct {
	capability  *config.Capability
	fingerprint config.Fingerprint
	required    bool
	adapter     Adapter
}

// Runtime manages capability lifecycle.
type Runtime struct {
	cfg       *config.Config
	mcpServer *server.MCPServer
	logger    *log.Logger

	mu           sync.RWMutex
	active       map[string]*activeEntry // name → currently enabled state
	failed       map[string]string       // name → error message (for the "failed" status surface)
	placeholders map[string]string       // capability name → placeholder tool name
	current      string                  // name of the currently active scene ("" before any SetScene)

	// Both fields mirror §A.11 scene_info semantics:
	//   - lastSwitch records the most recent set_scene attempt (incl. rejected)
	//   - lastCommitted records the most recent set_scene that actually landed
	//     state changes (ok OR partial_failure). Used to report the "root cause"
	//     when subsequent rejected switches overlay on top of a degraded state.
	lastSwitch    *SetSceneResult
	lastCommitted *SetSceneResult

	// adapterFactoryOverride lets tests inject a fake Adapter. nil in production.
	adapterFactoryOverride func(name string, cap *config.Capability) (Adapter, error)
}

// Adapter is the interface for capability backends (MCP, CLI).
type Adapter interface {
	// Start connects/spawns the capability and returns tools to register.
	Start(ctx context.Context) ([]server.ServerTool, error)
	// Stop terminates the capability.
	Stop() error
	// ToolNames returns currently registered tool names.
	ToolNames() []string
}

// New creates a new Runtime.
func New(cfg *config.Config, mcpServer *server.MCPServer, logger *log.Logger) *Runtime {
	return &Runtime{
		cfg:          cfg,
		mcpServer:    mcpServer,
		logger:       logger,
		active:       make(map[string]*activeEntry),
		failed:       make(map[string]string),
		placeholders: make(map[string]string),
	}
}

// newAdapterFor is the single adapter factory. Keeping it here means every
// lifecycle path — Enable, restart, rollback, shadow — builds the same object
// graph for the same capability config, which is what makes rollback semantics
// meaningful.
func (r *Runtime) newAdapterFor(name string, cap *config.Capability) (Adapter, error) {
	if r.adapterFactoryOverride != nil {
		return r.adapterFactoryOverride(name, cap)
	}
	switch cap.Type {
	case "mcp":
		return NewMCPAdapter(name, cap, r.logger), nil
	case "cli":
		return NewCLIAdapter(name, cap), nil
	default:
		return nil, fmt.Errorf("unknown capability type %q for %s", cap.Type, name)
	}
}

// EnableByScene enables all capabilities for a scene by name. This is the
// startup path — it performs a fresh enable rather than a diff, because the
// Runtime starts with an empty active set.
//
// Post-condition on success: r.current is set, r.lastSwitch reflects the outcome.
func (r *Runtime) EnableByScene(ctx context.Context, sceneName string) error {
	// The diff-based SetScene already handles the "from empty" case correctly
	// and populates lastSwitch / lastCommitted, so we just delegate.
	_, err := r.SetScene(ctx, sceneName)
	return err
}

// Enable activates a capability by name (standalone, outside a scene switch).
// Used by the `enable` MCP tool for the "runtime enable as an escape hatch"
// path (§A.1) and indirectly by SetScene's enable-class actions.
func (r *Runtime) Enable(ctx context.Context, name string) error {
	cap, ok := r.cfg.Capabilities[name]
	if !ok {
		return fmt.Errorf("capability %q not found in config", name)
	}
	if cap.Disabled {
		return fmt.Errorf("capability %q is disabled", name)
	}

	r.mu.Lock()
	if _, exists := r.active[name]; exists {
		r.mu.Unlock()
		return nil // already enabled — idempotent
	}
	r.mu.Unlock()

	fp, err := config.Fingerprints(cap)
	if err != nil {
		return fmt.Errorf("fingerprinting %s: %w", name, err)
	}

	adapter, err := r.newAdapterFor(name, cap)
	if err != nil {
		return err
	}

	tools, err := adapter.Start(ctx)
	if err != nil {
		r.mu.Lock()
		r.failed[name] = err.Error()
		r.mu.Unlock()
		return fmt.Errorf("enabling %s: %w", name, err)
	}

	r.mu.Lock()
	r.active[name] = &activeEntry{
		capability:  cap,
		fingerprint: fp,
		adapter:     adapter,
	}
	delete(r.failed, name)
	r.mu.Unlock()

	if len(tools) > 0 {
		r.mcpServer.AddTools(tools...)
	}
	r.removePlaceholder(name)

	r.logger.Printf("enabled: %s (%d tools)", name, len(tools))
	return nil
}

// Disable deactivates a capability by name (standalone, outside a scene switch).
func (r *Runtime) Disable(name string) error {
	r.mu.Lock()
	entry, ok := r.active[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("capability %q is not enabled", name)
	}
	toolNames := entry.adapter.ToolNames()
	delete(r.active, name)
	r.mu.Unlock()

	if len(toolNames) > 0 {
		r.mcpServer.DeleteTools(toolNames...)
	}
	if err := entry.adapter.Stop(); err != nil {
		r.logger.Printf("warning: error stopping %s: %v", name, err)
	}
	r.registerPlaceholder(name)

	r.logger.Printf("disabled: %s", name)
	return nil
}

// SetScene performs a diff-based scene switch with best-effort atomicity.
// The return value is always non-nil (even on error) and mirrors §A.12's
// structured response — consumers must check Status, not just err.
//
// Phase 1 (pre-flight): for every enable-class required cap, start a shadow
// adapter and verify it produces non-empty tools. Any required failure here
// → StatusRejected, old scene untouched. For restart-class required caps,
// run static checks (required_env) without touching the process.
//
// Phase 2 (commit): in order, restart → refresh_tools → commit shadow enables
// → disable. Restart failures for required caps attempt to roll back to the
// old config; a rollback failure leaves the cap in the failed state but skips
// the subsequent disable step, preserving any remaining old-scene-unique
// capability surface per §A.12 "最小破坏" rule.
func (r *Runtime) SetScene(ctx context.Context, sceneName string) (*SetSceneResult, error) {
	started := time.Now()
	r.mu.RLock()
	entrySceneName := r.current
	r.mu.RUnlock()

	result := &SetSceneResult{
		RequestedScene: sceneName,
		ActiveScene:    entrySceneName,
		FromScene:      entrySceneName,
		At:             started,
		Applied:        []AppliedEntry{},
		Failed:         []FailedEntry{},
	}

	target, err := r.cfg.EffectiveScene(sceneName)
	if err != nil {
		reason := err.Error()
		result.Status = StatusRejected
		result.Reason = &reason
		result.At = time.Now()
		r.recordSwitch(result)
		return result, err
	}

	// Snapshot current active state for diff computation. The snapshot is held
	// under a short RLock; the actual Phase 2 mutation reacquires the Lock.
	r.mu.RLock()
	snapshot := make(map[string]currentCapState, len(r.active))
	for name, entry := range r.active {
		snapshot[name] = currentCapState{
			Capability:  entry.capability,
			Fingerprint: entry.fingerprint,
			Required:    entry.required,
		}
	}
	r.mu.RUnlock()

	plans, err := ComputePlan(snapshot, target)
	if err != nil {
		reason := err.Error()
		result.Status = StatusRejected
		result.Reason = &reason
		result.At = time.Now()
		r.recordSwitch(result)
		return result, err
	}

	// --- Phase 1: pre-flight validate ---
	//
	// shadow holds enable-class adapters already started but not yet registered
	// on the MCP server. Anything in here needs to be either committed (Phase 2)
	// or cleanly stopped (on rejection).
	type shadowRec struct {
		plan    SwitchPlan
		adapter Adapter
		tools   []server.ServerTool
	}
	var shadows []shadowRec

	discardShadows := func() {
		for _, s := range shadows {
			if err := s.adapter.Stop(); err != nil {
				r.logger.Printf("shadow cleanup: stop %s failed: %v", s.plan.Name, err)
			}
		}
		shadows = nil
	}

	for _, p := range plans {
		switch p.Action {
		case ActionEnable:
			adapter, err := r.newAdapterFor(p.Name, p.NewCap)
			if err != nil {
				if p.Required {
					discardShadows()
					return r.rejectWith(result, plans, fmt.Sprintf("adapter factory for required %q: %v", p.Name, err))
				}
				result.Failed = append(result.Failed, FailedEntry{
					Name: p.Name, Action: p.Action, Reason: err.Error(), Required: false, Rollback: RollbackNotAttempted,
				})
				continue
			}
			tools, err := adapter.Start(ctx)
			if err != nil {
				if p.Required {
					discardShadows()
					return r.rejectWith(result, plans, fmt.Sprintf("shadow start for required %q: %v", p.Name, err))
				}
				result.Failed = append(result.Failed, FailedEntry{
					Name: p.Name, Action: p.Action, Reason: err.Error(), Required: false, Rollback: RollbackNotAttempted,
				})
				continue
			}
			if len(tools) == 0 {
				if p.Required {
					_ = adapter.Stop()
					discardShadows()
					return r.rejectWith(result, plans, fmt.Sprintf("shadow start for required %q produced no tools", p.Name))
				}
				_ = adapter.Stop()
				result.Failed = append(result.Failed, FailedEntry{
					Name: p.Name, Action: p.Action, Reason: "adapter exposed no tools",
					Required: false, Rollback: RollbackNotAttempted,
				})
				continue
			}
			shadows = append(shadows, shadowRec{plan: p, adapter: adapter, tools: tools})

		case ActionRestart:
			// Static check: required_env present in the environment.
			// Command existence on PATH is intentionally NOT checked here —
			// PATH at the capx process start may differ from what the child
			// sees, and a false-positive here would reject usable configs.
			if missing := missingRequiredEnv(p.NewCap); len(missing) > 0 {
				reason := fmt.Sprintf("required_env not set: %s", strings.Join(missing, ", "))
				if p.Required {
					discardShadows()
					return r.rejectWith(result, plans, fmt.Sprintf("required %q: %s", p.Name, reason))
				}
				result.Failed = append(result.Failed, FailedEntry{
					Name: p.Name, Action: p.Action, Reason: reason, Required: false, Rollback: RollbackNotAttempted,
				})
			}
		}
	}

	// --- Phase 2: commit ---
	//
	// Order is deliberate: restart/refresh_tools first (they mutate existing
	// caps, and their failure is what drives the best-effort atomicity story),
	// then enable (commit shadows), and disable LAST so that a required restart
	// rollback failure doesn't also strip old-scene-unique caps (§A.12).
	skipDisable := false

	r.mu.Lock()
	// Group plans by action for the prescribed ordering.
	var (
		restarts       []SwitchPlan
		refreshes      []SwitchPlan
		enables        []SwitchPlan
		disables       []SwitchPlan
		keeps          []SwitchPlan
		failedOptional = make(map[string]bool) // Phase 1 optional failures — don't re-attempt in Phase 2
	)
	for _, fe := range result.Failed {
		failedOptional[fe.Name+"|"+string(fe.Action)] = true
	}
	for _, p := range plans {
		switch p.Action {
		case ActionRestart:
			if failedOptional[p.Name+"|"+string(ActionRestart)] {
				continue // static check already recorded it as failed
			}
			restarts = append(restarts, p)
		case ActionRefreshTools:
			refreshes = append(refreshes, p)
		case ActionEnable:
			enables = append(enables, p)
		case ActionDisable:
			disables = append(disables, p)
		case ActionKeep:
			keeps = append(keeps, p)
		}
	}

	// Restarts: stop old → start new → on failure, rollback to old config.
	for _, p := range restarts {
		oldEntry := r.active[p.Name]
		oldTools := oldEntry.adapter.ToolNames()

		if len(oldTools) > 0 {
			r.mcpServer.DeleteTools(oldTools...)
		}
		if err := oldEntry.adapter.Stop(); err != nil {
			r.logger.Printf("restart %s: stop-old warning: %v", p.Name, err)
		}
		delete(r.active, p.Name)

		newAdapter, err := r.newAdapterFor(p.Name, p.NewCap)
		if err != nil {
			r.handleRestartFailure(result, p, oldEntry, err, &skipDisable)
			continue
		}
		newTools, err := newAdapter.Start(ctx)
		if err != nil {
			r.handleRestartFailure(result, p, oldEntry, err, &skipDisable)
			continue
		}

		newFp, _ := config.Fingerprints(p.NewCap)
		r.active[p.Name] = &activeEntry{
			capability:  p.NewCap,
			fingerprint: newFp,
			required:    p.Required,
			adapter:     newAdapter,
		}
		if len(newTools) > 0 {
			r.mcpServer.AddTools(newTools...)
		}
		result.Applied = append(result.Applied, AppliedEntry{Name: p.Name, Action: ActionRestart})
	}

	// refresh_tools: tools schema changed but process config is unchanged.
	// For CLI adapters this is equivalent to a quick re-register; for MCP
	// adapters it's a noop against the actual process (tools live on the
	// upstream server, not in capx YAML) — in practice diffAction will rarely
	// produce ActionRefreshTools for MCP types. We treat it uniformly.
	for _, p := range refreshes {
		oldEntry := r.active[p.Name]
		oldTools := oldEntry.adapter.ToolNames()
		if len(oldTools) > 0 {
			r.mcpServer.DeleteTools(oldTools...)
		}
		if err := oldEntry.adapter.Stop(); err != nil {
			r.logger.Printf("refresh_tools %s: stop-old warning: %v", p.Name, err)
		}
		delete(r.active, p.Name)

		newAdapter, err := r.newAdapterFor(p.Name, p.NewCap)
		if err != nil {
			// Treat as restart failure semantics — very unlikely since factory
			// doesn't do I/O, but keep the path uniform.
			r.handleRestartFailure(result, p, oldEntry, err, &skipDisable)
			continue
		}
		newTools, err := newAdapter.Start(ctx)
		if err != nil {
			r.handleRestartFailure(result, p, oldEntry, err, &skipDisable)
			continue
		}
		newFp, _ := config.Fingerprints(p.NewCap)
		r.active[p.Name] = &activeEntry{
			capability:  p.NewCap,
			fingerprint: newFp,
			required:    p.Required,
			adapter:     newAdapter,
		}
		if len(newTools) > 0 {
			r.mcpServer.AddTools(newTools...)
		}
		result.Applied = append(result.Applied, AppliedEntry{Name: p.Name, Action: ActionRefreshTools})
	}

	// Commit shadows (enables).
	for _, s := range shadows {
		r.removePlaceholderLocked(s.plan.Name)
		newFp, _ := config.Fingerprints(s.plan.NewCap)
		r.active[s.plan.Name] = &activeEntry{
			capability:  s.plan.NewCap,
			fingerprint: newFp,
			required:    s.plan.Required,
			adapter:     s.adapter,
		}
		if len(s.tools) > 0 {
			r.mcpServer.AddTools(s.tools...)
		}
		result.Applied = append(result.Applied, AppliedEntry{Name: s.plan.Name, Action: ActionEnable})
	}

	// Disables — skipped on required-restart + rollback-failed combos.
	var disabledNames []string
	if !skipDisable {
		for _, p := range disables {
			entry := r.active[p.Name]
			if entry == nil {
				continue
			}
			toolNames := entry.adapter.ToolNames()
			if len(toolNames) > 0 {
				r.mcpServer.DeleteTools(toolNames...)
			}
			if err := entry.adapter.Stop(); err != nil {
				r.logger.Printf("disable %s warning: %v", p.Name, err)
			}
			delete(r.active, p.Name)
			disabledNames = append(disabledNames, p.Name)
			result.Applied = append(result.Applied, AppliedEntry{Name: p.Name, Action: ActionDisable})
		}
	} else {
		r.logger.Printf("skipping %d disable(s) due to required-restart rollback failure", len(disables))
	}

	// Keep entries are unchanged but still reported for transparency.
	for _, p := range keeps {
		result.Applied = append(result.Applied, AppliedEntry{Name: p.Name, Action: ActionKeep})
	}

	// Reconcile r.failed so List() / enable-info surfaces the set_scene outcome.
	// A cap is "failed" iff it's not active but carries a Failed entry.
	r.failed = make(map[string]string, len(result.Failed))
	for _, fe := range result.Failed {
		if _, active := r.active[fe.Name]; active {
			continue
		}
		r.failed[fe.Name] = fe.Reason
	}

	// Finalize scene name and Phase 2 status.
	partialFailure := anyRequiredRollbackFailed(result)
	if partialFailure {
		// The new scene is partially active; some required cap(s) left in
		// failed state. Still report the requested scene as active — §A.12
		// rejects only in Phase 1; Phase 2 mutation commits the name even
		// when something inside didn't make it.
		result.Status = StatusPartialFailure
		reason := fmt.Sprintf("required restart failed and rollback did not recover; see failed[]")
		result.Reason = &reason
		r.current = sceneName
	} else {
		result.Status = StatusOK
		r.current = sceneName
	}
	result.ActiveScene = r.current
	r.mu.Unlock()

	for _, name := range disabledNames {
		r.registerPlaceholder(name)
	}

	// Stable ordering for caller consumption.
	sort.Slice(result.Applied, func(i, j int) bool {
		if result.Applied[i].Name == result.Applied[j].Name {
			return result.Applied[i].Action < result.Applied[j].Action
		}
		return result.Applied[i].Name < result.Applied[j].Name
	})
	sort.Slice(result.Failed, func(i, j int) bool { return result.Failed[i].Name < result.Failed[j].Name })

	result.At = time.Now()
	r.recordSwitch(result)
	return result, nil
}

// rejectWith converts a Phase-1 error path into a StatusRejected result.
// shadows must already be discarded by the caller.
func (r *Runtime) rejectWith(result *SetSceneResult, plans []SwitchPlan, reason string) (*SetSceneResult, error) {
	result.Status = StatusRejected
	result.Reason = &reason
	result.Applied = nil
	// Failed[] retains any optional pre-flight failures already captured;
	// also emit an entry for the required cap that actually triggered rejection,
	// if the reason string identifies one. Keeping this lossy (string-parsed)
	// would be fragile, so the caller-provided reason lives only in result.Reason;
	// per-cap diagnostics come from the loop that appended to Failed[].
	result.At = time.Now()
	// ActiveScene stays as entry-time snapshot (r.current).
	r.recordSwitch(result)
	return result, fmt.Errorf("set_scene rejected: %s", reason)
}

// handleRestartFailure runs the rollback path for a required restart and
// records the outcome. On rollback failure, it sets *skipDisable so the caller
// preserves old-scene-unique capabilities (§A.12 "最小破坏" rule).
//
// Called with r.mu held.
func (r *Runtime) handleRestartFailure(
	result *SetSceneResult,
	p SwitchPlan,
	oldEntry *activeEntry,
	cause error,
	skipDisable *bool,
) {
	if !p.Required {
		// Optional restart: don't attempt rollback, just record and move on.
		result.Failed = append(result.Failed, FailedEntry{
			Name: p.Name, Action: ActionRestart, Reason: cause.Error(),
			Required: false, Rollback: RollbackNotAttempted,
		})
		return
	}

	// Required: try to rebuild the old adapter with the old cap config.
	rollbackAdapter, err := r.newAdapterFor(p.Name, oldEntry.capability)
	if err != nil {
		result.Failed = append(result.Failed, FailedEntry{
			Name: p.Name, Action: ActionRestart,
			Reason:   fmt.Sprintf("%v; rollback factory error: %v", cause, err),
			Required: true, Rollback: RollbackFailed,
		})
		*skipDisable = true
		return
	}
	rollbackTools, err := rollbackAdapter.Start(context.Background())
	if err != nil {
		result.Failed = append(result.Failed, FailedEntry{
			Name: p.Name, Action: ActionRestart,
			Reason:   fmt.Sprintf("%v; rollback start error: %v", cause, err),
			Required: true, Rollback: RollbackFailed,
		})
		*skipDisable = true
		return
	}
	// Rollback succeeded — restore the active entry with OLD fingerprint.
	r.active[p.Name] = &activeEntry{
		capability:  oldEntry.capability,
		fingerprint: oldEntry.fingerprint,
		required:    oldEntry.required,
		adapter:     rollbackAdapter,
	}
	if len(rollbackTools) > 0 {
		r.mcpServer.AddTools(rollbackTools...)
	}
	result.Failed = append(result.Failed, FailedEntry{
		Name: p.Name, Action: ActionRestart, Reason: cause.Error(),
		Required: true, Rollback: RollbackSucceeded,
	})
}

// missingRequiredEnv returns the subset of cap.RequiredEnv not set in os.Environ.
// "set" means os.LookupEnv returns ok — empty string is considered set.
func missingRequiredEnv(cap *config.Capability) []string {
	var missing []string
	for _, key := range cap.RequiredEnv {
		if _, ok := os.LookupEnv(key); !ok {
			missing = append(missing, key)
		}
	}
	return missing
}

// anyRequiredRollbackFailed reports whether any failed entry represents a
// required restart whose rollback also failed — the trigger for partial_failure.
func anyRequiredRollbackFailed(result *SetSceneResult) bool {
	for _, fe := range result.Failed {
		if fe.Required && fe.Rollback == RollbackFailed {
			return true
		}
	}
	return false
}

// recordSwitch updates lastSwitch (always) and lastCommitted (only for state-
// changing outcomes). Called with r.mu NOT held.
func (r *Runtime) recordSwitch(result *SetSceneResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Shallow copy to decouple subsequent mutations.
	snap := *result
	r.lastSwitch = &snap
	if result.Status == StatusOK || result.Status == StatusPartialFailure {
		r.lastCommitted = &snap
	}
}

// LastSwitch returns a copy of the most recent set_scene attempt, or nil if
// SetScene has never been called. Consumed by scene_info (B2.3).
func (r *Runtime) LastSwitch() *SetSceneResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.lastSwitch == nil {
		return nil
	}
	snap := *r.lastSwitch
	return &snap
}

// LastCommittedSwitch returns a copy of the most recent set_scene that committed
// state (ok or partial_failure), or nil. Consumed by scene_info (B2.3).
func (r *Runtime) LastCommittedSwitch() *SetSceneResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.lastCommitted == nil {
		return nil
	}
	snap := *r.lastCommitted
	return &snap
}

// CurrentScene returns the name of the active scene, or "".
func (r *Runtime) CurrentScene() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
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

		if _, ok := r.active[name]; ok {
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

	for name, entry := range r.active {
		if err := entry.adapter.Stop(); err != nil {
			r.logger.Printf("error stopping %s: %v", name, err)
		}
	}
	r.active = make(map[string]*activeEntry)
}

// GenerateStateSummary builds a dynamic summary of the current runtime state.
func (r *Runtime) GenerateStateSummary() string {
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
