package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/oaooao/capx/internal/config"
)

func TestSceneInfo_InitialEmpty(t *testing.T) {
	// No scene ever loaded — SceneInfo returns zero-ish struct with nil switches.
	rt := newBuilder(t).build()
	info := rt.SceneInfo()
	if info.ActiveScene != "" {
		t.Errorf("active_scene should be empty, got %q", info.ActiveScene)
	}
	if info.Degraded {
		t.Error("degraded should be false on empty runtime")
	}
	if info.LastSwitch != nil || info.LastCommittedSwitch != nil {
		t.Error("switch summaries should be nil before any set_scene")
	}
}

func TestSceneInfo_HappyPath(t *testing.T) {
	capA := &config.Capability{Type: "cli", Command: "a", Description: "cap a"}
	capB := &config.Capability{Type: "mcp", URL: "http://b", Description: "cap b"}
	rt := newBuilder(t).
		cap("a", capA).cap("b", capB).
		scene("s", &config.Scene{
			Description: "test scene",
			AutoEnable:  config.AutoEnable{Required: []string{"a", "b"}},
		}).
		build()

	if _, err := rt.SetScene(context.Background(), "s"); err != nil {
		t.Fatalf("SetScene: %v", err)
	}
	info := rt.SceneInfo()

	if info.ActiveScene != "s" {
		t.Errorf("active_scene: want s, got %q", info.ActiveScene)
	}
	if info.Description != "test scene" {
		t.Errorf("description: want 'test scene', got %q", info.Description)
	}
	if len(info.Ready) != 2 {
		t.Errorf("ready: want 2, got %d", len(info.Ready))
	}
	if len(info.Failed) != 0 {
		t.Errorf("failed should be empty, got %+v", info.Failed)
	}
	if info.Degraded {
		t.Error("degraded should be false in happy path")
	}
	if info.DegradationReason != nil {
		t.Errorf("degradation_reason should be nil, got %v", *info.DegradationReason)
	}
	if info.LastSwitch == nil || info.LastSwitch.Status != "ok" {
		t.Errorf("last_switch: want status=ok, got %+v", info.LastSwitch)
	}
	if info.LastCommittedSwitch == nil || info.LastCommittedSwitch.Status != "ok" {
		t.Errorf("last_committed_switch: want status=ok, got %+v", info.LastCommittedSwitch)
	}
	// From_scene on first switch should be nil (empty pre-state).
	if info.LastSwitch.FromScene != nil {
		t.Errorf("last_switch.from_scene: want nil on first switch, got %v", *info.LastSwitch.FromScene)
	}
}

func TestSceneInfo_StartupFailureWithRequiredOptional(t *testing.T) {
	// A required cap fails at first load → rejected; current stays empty.
	// scene_info should still render, with switch status=rejected and
	// no active scene.
	capBad := &config.Capability{Type: "cli", Command: "bad"}
	fpBad, _ := config.Fingerprints(capBad)
	rt := newBuilder(t).
		cap("bad", capBad).
		scene("s", &config.Scene{AutoEnable: config.AutoEnable{Required: []string{"bad"}}}).
		behaveByProcessHash("bad", fpBad.ProcessHash,
			startResult{err: errors.New("boom")},
		).
		build()

	_, _ = rt.SetScene(context.Background(), "s")
	info := rt.SceneInfo()

	if info.ActiveScene != "" {
		t.Errorf("active_scene should stay empty after rejected startup, got %q", info.ActiveScene)
	}
	// degraded only true if there's an active scene with required failed.
	// Here active stays empty; the degradation condition (required failed in
	// ACTIVE scene) is a distinct signal from "startup was rejected".
	if info.LastSwitch == nil || info.LastSwitch.Status != "rejected" {
		t.Errorf("last_switch: want rejected, got %+v", info.LastSwitch)
	}
	// lastCommittedSwitch stays nil — nothing committed yet.
	if info.LastCommittedSwitch != nil {
		t.Errorf("last_committed_switch should remain nil on rejected-only history, got %+v",
			info.LastCommittedSwitch)
	}
}

func TestSceneInfo_DegradedAfterPartialFailure(t *testing.T) {
	// Setup from the B2.2 partial_failure scenario, then check scene_info.
	capV1 := &config.Capability{Type: "cli", Command: "v", Args: []string{"1"}}
	capV2 := &config.Capability{Type: "cli", Command: "v", Args: []string{"2"}}
	fp1, _ := config.Fingerprints(capV1)
	fp2, _ := config.Fingerprints(capV2)

	rt := newBuilder(t).
		cap("v", capV1).
		scene("s1", &config.Scene{AutoEnable: config.AutoEnable{Required: []string{"v"}}}).
		scene("s2", &config.Scene{
			AutoEnable:   config.AutoEnable{Required: []string{"v"}},
			Capabilities: map[string]*config.Capability{"v": capV2},
		}).
		behaveByProcessHash("v", fp2.ProcessHash, startResult{err: errors.New("v2 boom")}).
		behaveByProcessHash("v", fp1.ProcessHash,
			startResult{}, // initial start succeeds
			startResult{err: errors.New("v1 rollback fails")},
		).
		build()

	ctx := context.Background()
	_, _ = rt.SetScene(ctx, "s1")
	_, _ = rt.SetScene(ctx, "s2")

	info := rt.SceneInfo()
	if !info.Degraded {
		t.Error("degraded should be true after partial_failure with required cap failed")
	}
	if info.DegradationReason == nil || *info.DegradationReason != "failed_switch" {
		got := "<nil>"
		if info.DegradationReason != nil {
			got = *info.DegradationReason
		}
		t.Errorf("degradation_reason: want failed_switch, got %s", got)
	}
	if info.LastCommittedSwitch == nil || info.LastCommittedSwitch.Status != "partial_failure" {
		t.Errorf("last_committed_switch: want partial_failure, got %+v", info.LastCommittedSwitch)
	}
	// Subsequent rejected switch should NOT overwrite last_committed_switch.
	_, _ = rt.SetScene(ctx, "nonexistent")
	info2 := rt.SceneInfo()
	if info2.LastCommittedSwitch == nil || info2.LastCommittedSwitch.Status != "partial_failure" {
		t.Errorf("last_committed_switch must survive subsequent rejected, got %+v",
			info2.LastCommittedSwitch)
	}
	if info2.LastSwitch == nil || info2.LastSwitch.Status != "rejected" {
		t.Errorf("last_switch should reflect the most recent rejection, got %+v", info2.LastSwitch)
	}
}
