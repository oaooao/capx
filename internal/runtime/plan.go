package runtime

import (
	"fmt"
	"sort"
	"time"

	"github.com/oaooao/capx/internal/config"
)

// Action is the planned operation for a single capability during a scene switch.
// The five-class taxonomy is fixed by §A.12 "Diff 算法".
type Action string

const (
	ActionEnable       Action = "enable"        // new cap (not currently active)
	ActionRestart      Action = "restart"       // process_hash changed → stop old + start new
	ActionRefreshTools Action = "refresh_tools" // process_hash same but tools_hash changed → re-register schema
	ActionDisable      Action = "disable"       // cap is in current scene but not in target
	ActionKeep         Action = "keep"          // both hashes unchanged → no-op
)

// SwitchPlan is the intended operation for one capability.
type SwitchPlan struct {
	Name     string
	Action   Action
	Required bool // target scene's required classification (for enable/restart/refresh/keep);
	//              for disable, inherits the old entry's required-ness so the Agent can
	//              understand "we're dropping a required cap" vs an optional one
	OldCap *config.Capability // nil for enable
	NewCap *config.Capability // nil for disable
}

// SwitchStatus is the outer result of a set_scene call.
// See §A.12 "原子性语义的诚实表述".
type SwitchStatus string

const (
	StatusOK              SwitchStatus = "ok"
	StatusRejected        SwitchStatus = "rejected"         // Phase 1 refused; old scene untouched
	StatusPartialFailure  SwitchStatus = "partial_failure"  // Phase 2 commit had required restart fail + rollback fail
)

// SetSceneResult is the structured response returned to MCP clients.
// Mirrors the JSON schema in §A.12 "mcp__capx__set_scene 响应 schema".
type SetSceneResult struct {
	Status         SwitchStatus   `json:"status"`
	ActiveScene    string         `json:"active_scene"`
	RequestedScene string         `json:"requested_scene"`
	// FromScene records the scene active before this switch was attempted —
	// empty string if the runtime had no prior scene. Needed by scene_info
	// (§A.11) so the Agent can decide whether to rollback.
	FromScene string         `json:"from_scene"`
	At        time.Time      `json:"at"`
	Applied   []AppliedEntry `json:"applied"`
	Failed    []FailedEntry  `json:"failed"`
	Reason    *string        `json:"reason"` // null on ok
}

// AppliedEntry records a successfully applied plan step.
type AppliedEntry struct {
	Name   string `json:"name"`
	Action Action `json:"action"`
}

// RollbackStatus describes whether and how a failed restart was rolled back.
type RollbackStatus string

const (
	RollbackNotAttempted RollbackStatus = "not_attempted" // optional restart, or Phase 1 reject
	RollbackSucceeded    RollbackStatus = "succeeded"     // required restart failed → old config restored
	RollbackFailed       RollbackStatus = "failed"        // required restart failed AND restart-old also failed
)

// FailedEntry is one step that did not fully succeed.
type FailedEntry struct {
	Name     string         `json:"name"`
	Action   Action         `json:"action"`
	Reason   string         `json:"reason"`
	Required bool           `json:"required"`
	Rollback RollbackStatus `json:"rollback"`
}

// currentCapState tracks the runtime state for one active capability.
// Kept separately from Adapter so we can diff against it without having to
// re-read from the adapter (which may have internal locks).
type currentCapState struct {
	Capability  *config.Capability
	Fingerprint config.Fingerprint
	Required    bool // required-ness from the scene that enabled it
}

// ComputePlan diffs the current active set against the target effective scene
// and returns a stable-ordered list of per-cap plans.
//
// Ordering: lexicographic by name. Phase 2 execution reorders by action per
// §A.12 — that happens in executePlan, not here.
func ComputePlan(current map[string]currentCapState, target []config.EffectiveCap) ([]SwitchPlan, error) {
	// Index target by name to detect membership and get required-ness.
	targetIdx := make(map[string]config.EffectiveCap, len(target))
	for _, ec := range target {
		targetIdx[ec.Name] = ec
	}

	names := make(map[string]struct{}, len(current)+len(target))
	for n := range current {
		names[n] = struct{}{}
	}
	for n := range targetIdx {
		names[n] = struct{}{}
	}

	plans := make([]SwitchPlan, 0, len(names))
	for name := range names {
		oldState, inOld := current[name]
		newEC, inNew := targetIdx[name]

		switch {
		case !inOld && inNew:
			plans = append(plans, SwitchPlan{
				Name:     name,
				Action:   ActionEnable,
				Required: newEC.Required,
				NewCap:   newEC.Capability,
			})
		case inOld && !inNew:
			plans = append(plans, SwitchPlan{
				Name:     name,
				Action:   ActionDisable,
				Required: oldState.Required,
				OldCap:   oldState.Capability,
			})
		default:
			// Both sides present — hash the new cap to compare.
			newFp, err := config.Fingerprints(newEC.Capability)
			if err != nil {
				return nil, fmt.Errorf("fingerprint %q: %w", name, err)
			}
			action := diffAction(oldState.Fingerprint, newFp)
			plans = append(plans, SwitchPlan{
				Name:     name,
				Action:   action,
				Required: newEC.Required,
				OldCap:   oldState.Capability,
				NewCap:   newEC.Capability,
			})
		}
	}

	sort.Slice(plans, func(i, j int) bool { return plans[i].Name < plans[j].Name })
	return plans, nil
}

// diffAction encodes the §A.12 truth table: process_hash decides restart vs
// non-restart; tools_hash alone decides refresh_tools vs keep.
func diffAction(oldFp, newFp config.Fingerprint) Action {
	if oldFp.ProcessHash != newFp.ProcessHash {
		return ActionRestart
	}
	if oldFp.ToolsHash != newFp.ToolsHash {
		return ActionRefreshTools
	}
	return ActionKeep
}
