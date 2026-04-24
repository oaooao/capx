package runtime

import (
	"testing"

	"github.com/oaooao/capx/internal/config"
)

// Helper: fingerprint a capability or panic. Tests deal with well-formed caps.
func fpOf(t *testing.T, c *config.Capability) config.Fingerprint {
	t.Helper()
	fp, err := config.Fingerprints(c)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	return fp
}

// Each case exercises one of the five diff outcomes (enable / restart /
// refresh_tools / disable / keep).

func TestComputePlan_EnableOnly(t *testing.T) {
	// current empty, target has one cap → single enable.
	newCap := &config.Capability{Type: "cli", Command: "echo"}
	target := []config.EffectiveCap{{Name: "a", Capability: newCap, Required: true}}

	plans, err := ComputePlan(nil, target)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(plans) != 1 || plans[0].Action != ActionEnable || !plans[0].Required {
		t.Fatalf("expected single required enable, got %+v", plans)
	}
}

func TestComputePlan_DisableOnly(t *testing.T) {
	// current has one, target empty → single disable.
	oldCap := &config.Capability{Type: "cli", Command: "echo"}
	current := map[string]currentCapState{
		"a": {Capability: oldCap, Fingerprint: fpOf(t, oldCap), Required: true},
	}

	plans, err := ComputePlan(current, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(plans) != 1 || plans[0].Action != ActionDisable {
		t.Fatalf("expected single disable, got %+v", plans)
	}
	// Disable carries forward the old entry's required-ness so Agent knows
	// what kind of cap is being dropped.
	if !plans[0].Required {
		t.Errorf("disable should carry old required=true")
	}
}

func TestComputePlan_KeepWhenIdentical(t *testing.T) {
	cap := &config.Capability{Type: "cli", Command: "echo", Args: []string{"hi"}}
	current := map[string]currentCapState{
		"a": {Capability: cap, Fingerprint: fpOf(t, cap)},
	}
	// New Capability object with identical config → same hashes → keep.
	same := &config.Capability{Type: "cli", Command: "echo", Args: []string{"hi"}}
	target := []config.EffectiveCap{{Name: "a", Capability: same}}

	plans, err := ComputePlan(current, target)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(plans) != 1 || plans[0].Action != ActionKeep {
		t.Fatalf("expected keep, got %+v", plans)
	}
}

func TestComputePlan_RestartWhenProcessChanged(t *testing.T) {
	oldCap := &config.Capability{Type: "cli", Command: "echo", Args: []string{"a"}}
	newCap := &config.Capability{Type: "cli", Command: "echo", Args: []string{"b"}}
	current := map[string]currentCapState{
		"x": {Capability: oldCap, Fingerprint: fpOf(t, oldCap)},
	}
	target := []config.EffectiveCap{{Name: "x", Capability: newCap}}

	plans, err := ComputePlan(current, target)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(plans) != 1 || plans[0].Action != ActionRestart {
		t.Fatalf("expected restart, got %+v", plans)
	}
}

func TestComputePlan_RefreshToolsWhenOnlyToolsChanged(t *testing.T) {
	oldCap := &config.Capability{Type: "cli", Command: "echo",
		Tools: map[string]*config.CLITool{"run": {Description: "v1"}},
	}
	newCap := &config.Capability{Type: "cli", Command: "echo",
		Tools: map[string]*config.CLITool{"run": {Description: "v2"}},
	}
	current := map[string]currentCapState{
		"t": {Capability: oldCap, Fingerprint: fpOf(t, oldCap)},
	}
	target := []config.EffectiveCap{{Name: "t", Capability: newCap}}

	plans, err := ComputePlan(current, target)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(plans) != 1 || plans[0].Action != ActionRefreshTools {
		t.Fatalf("expected refresh_tools, got %+v", plans)
	}
}

func TestComputePlan_MixedOrderingByName(t *testing.T) {
	// Checks deterministic ordering (lexicographic by name) regardless of
	// insertion order. Critical for stable set_scene responses.
	capA := &config.Capability{Type: "cli", Command: "a"}
	capB := &config.Capability{Type: "cli", Command: "b"}
	capC := &config.Capability{Type: "cli", Command: "c"}
	capD := &config.Capability{Type: "cli", Command: "d"}

	current := map[string]currentCapState{
		"keep":   {Capability: capA, Fingerprint: fpOf(t, capA)},
		"drop":   {Capability: capB, Fingerprint: fpOf(t, capB)},
	}
	target := []config.EffectiveCap{
		{Name: "keep", Capability: capA},
		{Name: "new", Capability: capC},
		{Name: "bump", Capability: capD},
	}

	plans, err := ComputePlan(current, target)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	wantNames := []string{"bump", "drop", "keep", "new"}
	if len(plans) != len(wantNames) {
		t.Fatalf("expected %d plans, got %d", len(wantNames), len(plans))
	}
	for i, p := range plans {
		if p.Name != wantNames[i] {
			t.Errorf("order[%d]: want %q, got %q", i, wantNames[i], p.Name)
		}
	}
}
