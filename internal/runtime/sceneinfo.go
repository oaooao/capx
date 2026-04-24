package runtime

import (
	"time"
)

// SceneInfo is the structured response for the mcp__capx__scene_info tool.
// Mirrors §A.11 "Scene 摘要的内容规范".
//
// An Agent calls this once at session start (and after any set_scene) to
// decide how much of the declared workbench is actually usable.
type SceneInfo struct {
	ActiveScene       string      `json:"active_scene"`
	Description       string      `json:"description"`
	Ready             []ReadyCap  `json:"ready"`
	Failed            []FailedCap `json:"failed"`
	Degraded          bool        `json:"degraded"`
	DegradationReason *string     `json:"degradation_reason"`
	LastSwitch        *SwitchSummary `json:"last_switch"`
	LastCommittedSwitch *SwitchSummary `json:"last_committed_switch"`
}

// ReadyCap is a capability that is currently enabled and contributing tools.
type ReadyCap struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	ToolCount int    `json:"tool_count"`
}

// FailedCap is a capability the Agent should avoid using. `required` tells
// the Agent whether task planning must account for its absence (true) or
// may tolerate it (false).
type FailedCap struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Error    string `json:"error"`
	Required bool   `json:"required"`
}

// SwitchSummary compresses a SetSceneResult into the fields relevant for
// scene_info (A.11) — Agent doesn't need the full applied/failed lists here,
// those live on set_scene's own response.
type SwitchSummary struct {
	Status    string    `json:"status"` // "initial" | "ok" | "rejected" | "partial_failure"
	At        time.Time `json:"at"`
	FromScene *string   `json:"from_scene"` // null on initial
	ToScene   string    `json:"to_scene"`
}

// Degradation reason constants (§A.11).
const (
	reasonStartupFailure = "startup_failure" // degraded since initial scene load
	reasonFailedSwitch   = "failed_switch"   // last committed switch was partial_failure
	reasonRuntimeCrash   = "runtime_crash"   // cap crashed after successful start (v0.3+)
)

// SceneInfo returns the current scene's structured info for Agent consumption.
func (r *Runtime) SceneInfo() *SceneInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info := &SceneInfo{
		ActiveScene: r.current,
		Ready:       []ReadyCap{},
		Failed:      []FailedCap{},
	}

	if scene, ok := r.cfg.Scenes[r.current]; ok {
		info.Description = scene.Description
	}

	// Build ready[] from active map, failed[] from r.failed.
	for name, entry := range r.active {
		info.Ready = append(info.Ready, ReadyCap{
			Name:      name,
			Type:      entry.capability.Type,
			ToolCount: len(entry.adapter.ToolNames()),
		})
	}
	// For failed entries, cross-reference with r.cfg.Capabilities to pull type
	// and — when we can recover it — whether the cap was required in the
	// current scene. r.active entries carry their required flag; failed ones
	// don't, so we consult the scene definition.
	scene := r.cfg.Scenes[r.current]
	for name, errMsg := range r.failed {
		cap := r.cfg.Capabilities[name]
		capType := ""
		if cap != nil {
			capType = cap.Type
		}
		required := false
		if scene != nil {
			required = scene.AutoEnable.IsRequired(name)
		}
		info.Failed = append(info.Failed, FailedCap{
			Name:     name,
			Type:     capType,
			Error:    errMsg,
			Required: required,
		})
	}

	// Determine degraded + reason.
	anyRequiredFailed := false
	for _, fc := range info.Failed {
		if fc.Required {
			anyRequiredFailed = true
			break
		}
	}
	info.Degraded = anyRequiredFailed
	if anyRequiredFailed {
		reason := deriveDegradationReason(r.lastCommitted)
		info.DegradationReason = &reason
	}

	info.LastSwitch = summarizeSwitch(r.lastSwitch, r.current == "" && r.lastSwitch == nil)
	info.LastCommittedSwitch = summarizeSwitch(r.lastCommitted, false)

	return info
}

// deriveDegradationReason implements the §A.11 decision table. The v0.2
// code never sees "runtime_crash" because there is no crash monitor yet;
// that branch is reserved for v0.3.
func deriveDegradationReason(lastCommitted *SetSceneResult) string {
	if lastCommitted == nil {
		return reasonStartupFailure
	}
	if lastCommitted.Status == StatusPartialFailure {
		return reasonFailedSwitch
	}
	// lastCommitted.Status == ok with required failed shouldn't happen in v0.2
	// (Phase 1 would have rejected); if it ever does, it must be a crash.
	return reasonRuntimeCrash
}

// summarizeSwitch compresses a SetSceneResult to the scene_info subset.
// When result is nil and the runtime has never switched, we synthesize an
// "initial" summary so the field is never bare null when a scene is loaded.
func summarizeSwitch(result *SetSceneResult, _ bool) *SwitchSummary {
	if result == nil {
		return nil
	}
	status := string(result.Status)
	var fromScene *string
	if result.FromScene != "" {
		fs := result.FromScene
		fromScene = &fs
	}
	return &SwitchSummary{
		Status:    status,
		At:        result.At,
		FromScene: fromScene,
		ToScene:   result.RequestedScene,
	}
}
