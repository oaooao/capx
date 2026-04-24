package runtime

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oaooao/capx/internal/config"
)

// -----------------------------------------------------------------------------
// controllableAdapter — a deterministic Adapter that lets each test choose
// whether a start succeeds, fails, or produces a specific tool set. Start
// invocations are counted so we can distinguish "original" from "rollback"
// instances created by the factory.
// -----------------------------------------------------------------------------

type controllableAdapter struct {
	name      string
	toolNames []string
	// behavior on each Start() call; values produced in FIFO order.
	// If the list runs out, subsequent calls succeed with no tools.
	startResults []startResult
	calls        atomic.Int32
	stopped      atomic.Int32
}

type startResult struct {
	err   error
	tools []server.ServerTool
}

func (a *controllableAdapter) Start(_ context.Context) ([]server.ServerTool, error) {
	idx := int(a.calls.Add(1)) - 1
	if idx < len(a.startResults) {
		r := a.startResults[idx]
		if r.err != nil {
			return nil, r.err
		}
		if len(r.tools) > 0 {
			a.toolNames = toolNamesOf(r.tools)
			return r.tools, nil
		}
		// Fall through to default success path.
	}
	// Default: succeed with a single synthetic tool named after the cap, so
	// subsequent diffs look like "has tools" and Phase 1 shadow checks pass.
	tool := server.ServerTool{
		Tool: mcp.NewTool(a.name+"__run", mcp.WithDescription(a.name)),
	}
	a.toolNames = []string{a.name + "__run"}
	return []server.ServerTool{tool}, nil
}

func toolNamesOf(tools []server.ServerTool) []string {
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i] = t.Tool.Name
	}
	return out
}

func (a *controllableAdapter) Stop() error     { a.stopped.Add(1); return nil }
func (a *controllableAdapter) ToolNames() []string { return a.toolNames }

// -----------------------------------------------------------------------------
// Test scaffolding: a runtime with a fully deterministic adapter factory.
// Each (name, signature) tuple gets its own controllableAdapter instance so
// rollback paths can be asserted by counting Start() calls.
// -----------------------------------------------------------------------------

type testRuntimeBuilder struct {
	t        *testing.T
	caps     map[string]*config.Capability
	scenes   map[string]*config.Scene
	// behaviors keyed by (name, processHash). If multiple starts happen with
	// the same key (e.g. rollback), each gets the next entry from the slice.
	behaviors map[string][]startResult
	// failCounts[name] = number of consecutive Start() failures to inject
	// before succeeding. Useful for "required restart fails once → rollback".
}

func newBuilder(t *testing.T) *testRuntimeBuilder {
	return &testRuntimeBuilder{
		t:         t,
		caps:      map[string]*config.Capability{},
		scenes:    map[string]*config.Scene{},
		behaviors: map[string][]startResult{},
	}
}

func (b *testRuntimeBuilder) cap(name string, c *config.Capability) *testRuntimeBuilder {
	b.caps[name] = c
	return b
}

func (b *testRuntimeBuilder) scene(name string, s *config.Scene) *testRuntimeBuilder {
	b.scenes[name] = s
	return b
}

// behaveByProcessHash registers a start-behavior sequence keyed by the
// capability's *current* process_hash. This lets a test say "when this specific
// cap config is started, fail the first attempt" — restarts with new config
// get their own key.
func (b *testRuntimeBuilder) behaveByProcessHash(name string, procHash string, results ...startResult) *testRuntimeBuilder {
	b.behaviors[name+"|"+procHash] = append(b.behaviors[name+"|"+procHash], results...)
	return b
}

func (b *testRuntimeBuilder) build() *Runtime {
	cfg := &config.Config{
		Capabilities: b.caps,
		Scenes:       b.scenes,
		DefaultScene: "default",
	}
	mcpServer := server.NewMCPServer("test", "0.1.0", server.WithToolCapabilities(true))
	logger := log.New(os.Stderr, "[test] ", 0)
	rt := New(cfg, mcpServer, logger)

	// The factory pops behaviors off the shared queue for each (name, proc_hash)
	// key. This way rollback (which re-factories with the old cap) consumes the
	// next behavior in the old cap's queue, not restart from the head.
	var mu sync.Mutex
	rt.adapterFactoryOverride = func(name string, c *config.Capability) (Adapter, error) {
		fp, err := config.Fingerprints(c)
		if err != nil {
			return nil, err
		}
		key := name + "|" + fp.ProcessHash
		adapter := &controllableAdapter{name: name}
		mu.Lock()
		if queue := b.behaviors[key]; len(queue) > 0 {
			adapter.startResults = []startResult{queue[0]}
			b.behaviors[key] = queue[1:]
		}
		mu.Unlock()
		return adapter, nil
	}
	return rt
}

// -----------------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------------

func TestSetScene_FromEmpty_EnablesAll(t *testing.T) {
	capA := &config.Capability{Type: "cli", Command: "a"}
	capB := &config.Capability{Type: "cli", Command: "b"}
	rt := newBuilder(t).
		cap("a", capA).cap("b", capB).
		scene("s", &config.Scene{AutoEnable: config.AutoEnable{
			Required: []string{"a"}, Optional: []string{"b"},
		}}).
		build()

	result, err := rt.SetScene(context.Background(), "s")
	if err != nil {
		t.Fatalf("SetScene: %v", err)
	}
	if result.Status != StatusOK {
		t.Errorf("expected ok, got %s (reason=%v)", result.Status, result.Reason)
	}
	if len(result.Applied) != 2 {
		t.Errorf("expected 2 applied, got %d", len(result.Applied))
	}
	if len(result.Failed) != 0 {
		t.Errorf("expected 0 failed, got %+v", result.Failed)
	}
	if rt.CurrentScene() != "s" {
		t.Errorf("current scene should be 's', got %q", rt.CurrentScene())
	}
}

func TestSetScene_RequiredEnableFailure_RejectsLeavingOldSceneIntact(t *testing.T) {
	capA := &config.Capability{Type: "cli", Command: "a"}
	capBad := &config.Capability{Type: "cli", Command: "bad"}

	fpBad, _ := config.Fingerprints(capBad)

	rt := newBuilder(t).
		cap("a", capA).cap("bad", capBad).
		scene("s1", &config.Scene{AutoEnable: config.AutoEnable{Required: []string{"a"}}}).
		scene("s2", &config.Scene{AutoEnable: config.AutoEnable{Required: []string{"bad"}}}).
		behaveByProcessHash("bad", fpBad.ProcessHash,
			startResult{err: errors.New("boom")},
		).
		build()

	// Land in s1 successfully.
	if _, err := rt.SetScene(context.Background(), "s1"); err != nil {
		t.Fatalf("setup SetScene s1: %v", err)
	}

	// Attempt s2 — required cap fails in shadow → rejected.
	result, err := rt.SetScene(context.Background(), "s2")
	if err == nil {
		t.Fatal("expected error for rejected switch")
	}
	if result.Status != StatusRejected {
		t.Errorf("expected rejected, got %s", result.Status)
	}
	if rt.CurrentScene() != "s1" {
		t.Errorf("old scene must be preserved, got %q", rt.CurrentScene())
	}
	// 'a' should still be active.
	infos := rt.List()
	var aStatus CapabilityStatus
	for _, info := range infos {
		if info.Name == "a" {
			aStatus = info.Status
		}
	}
	if aStatus != StatusEnabled {
		t.Errorf("old scene's cap 'a' should still be enabled after reject, got %s", aStatus)
	}
}

func TestSetScene_OptionalEnableFailure_YieldsOKWithFailedList(t *testing.T) {
	capGood := &config.Capability{Type: "cli", Command: "good"}
	capFlaky := &config.Capability{Type: "cli", Command: "flaky"}
	fpFlaky, _ := config.Fingerprints(capFlaky)

	rt := newBuilder(t).
		cap("good", capGood).cap("flaky", capFlaky).
		scene("s", &config.Scene{AutoEnable: config.AutoEnable{
			Required: []string{"good"}, Optional: []string{"flaky"},
		}}).
		behaveByProcessHash("flaky", fpFlaky.ProcessHash,
			startResult{err: errors.New("network blip")},
		).
		build()

	result, err := rt.SetScene(context.Background(), "s")
	if err != nil {
		t.Fatalf("SetScene: %v", err)
	}
	if result.Status != StatusOK {
		t.Errorf("expected ok, got %s", result.Status)
	}
	if len(result.Failed) != 1 || result.Failed[0].Name != "flaky" {
		t.Errorf("expected one failed (flaky), got %+v", result.Failed)
	}
	if result.Failed[0].Required {
		t.Error("flaky should be marked optional")
	}
}

func TestSetScene_SameSceneTwice_AllKeep(t *testing.T) {
	capA := &config.Capability{Type: "cli", Command: "a"}
	rt := newBuilder(t).
		cap("a", capA).
		scene("s", &config.Scene{AutoEnable: config.AutoEnable{Required: []string{"a"}}}).
		build()

	ctx := context.Background()
	if _, err := rt.SetScene(ctx, "s"); err != nil {
		t.Fatalf("first switch: %v", err)
	}
	result, err := rt.SetScene(ctx, "s")
	if err != nil {
		t.Fatalf("second switch: %v", err)
	}
	// All entries for the second switch should be keep.
	for _, a := range result.Applied {
		if a.Action != ActionKeep {
			t.Errorf("expected all keep on idempotent switch, got %s for %s", a.Action, a.Name)
		}
	}
}

func TestSetScene_ProcessChange_TriggersRestart(t *testing.T) {
	capV1 := &config.Capability{Type: "cli", Command: "v", Args: []string{"1"}}
	capV2 := &config.Capability{Type: "cli", Command: "v", Args: []string{"2"}}

	rt := newBuilder(t).
		cap("v", capV1). // initial config for scene s1
		scene("s1", &config.Scene{AutoEnable: config.AutoEnable{Required: []string{"v"}}}).
		scene("s2", &config.Scene{
			AutoEnable: config.AutoEnable{Required: []string{"v"}},
			// inline override so the same name resolves to a different cap when
			// s2 is active.
			Capabilities: map[string]*config.Capability{"v": capV2},
		}).
		build()

	ctx := context.Background()
	if _, err := rt.SetScene(ctx, "s1"); err != nil {
		t.Fatalf("s1: %v", err)
	}
	result, err := rt.SetScene(ctx, "s2")
	if err != nil {
		t.Fatalf("s2: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("expected ok, got %s", result.Status)
	}
	if len(result.Applied) != 1 || result.Applied[0].Action != ActionRestart {
		t.Errorf("expected single restart, got %+v", result.Applied)
	}
}

func TestSetScene_RequiredRestartFailsAndRollbackSucceeds_OKWithFailedEntry(t *testing.T) {
	capV1 := &config.Capability{Type: "cli", Command: "v", Args: []string{"1"}}
	capV2 := &config.Capability{Type: "cli", Command: "v", Args: []string{"2"}}

	fp2, _ := config.Fingerprints(capV2)

	rt := newBuilder(t).
		cap("v", capV1).
		scene("s1", &config.Scene{AutoEnable: config.AutoEnable{Required: []string{"v"}}}).
		scene("s2", &config.Scene{
			AutoEnable:   config.AutoEnable{Required: []string{"v"}},
			Capabilities: map[string]*config.Capability{"v": capV2},
		}).
		behaveByProcessHash("v", fp2.ProcessHash,
			startResult{err: errors.New("port in use")},
		).
		build()

	ctx := context.Background()
	if _, err := rt.SetScene(ctx, "s1"); err != nil {
		t.Fatalf("s1: %v", err)
	}
	// Attempt s2: v2 restart fails, rollback to v1 succeeds (default behavior).
	result, err := rt.SetScene(ctx, "s2")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if result.Status != StatusOK {
		t.Errorf("expected ok after successful rollback, got %s (reason=%v)",
			result.Status, result.Reason)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("expected 1 failed entry, got %+v", result.Failed)
	}
	fe := result.Failed[0]
	if fe.Rollback != RollbackSucceeded {
		t.Errorf("rollback status: want succeeded, got %s", fe.Rollback)
	}
	// Scene name commits to s2 even though v is running old config — §A.12's
	// current behavior is to set the scene name on any Phase-2-reaching outcome.
	if rt.CurrentScene() != "s2" {
		t.Errorf("current scene should reflect commit, got %q", rt.CurrentScene())
	}
}

func TestSetScene_RequiredRestartFailsAndRollbackFails_PartialFailureAndSkipsDisable(t *testing.T) {
	// Setup: s1 has {v1, extra}; s2 has {v2}. Restart of v fails, rollback also
	// fails (we inject failures for BOTH the v2 start AND the rollback-to-v1
	// start). In that case §A.12 says: skip the disable of `extra` to preserve
	// the remaining workbench surface.
	capV1 := &config.Capability{Type: "cli", Command: "v", Args: []string{"1"}}
	capV2 := &config.Capability{Type: "cli", Command: "v", Args: []string{"2"}}
	capExtra := &config.Capability{Type: "cli", Command: "extra"}

	fp1, _ := config.Fingerprints(capV1)
	fp2, _ := config.Fingerprints(capV2)

	rt := newBuilder(t).
		cap("v", capV1).cap("extra", capExtra).
		scene("s1", &config.Scene{AutoEnable: config.AutoEnable{Required: []string{"v", "extra"}}}).
		scene("s2", &config.Scene{
			AutoEnable:   config.AutoEnable{Required: []string{"v"}},
			Capabilities: map[string]*config.Capability{"v": capV2},
		}).
		// v2 fails.
		behaveByProcessHash("v", fp2.ProcessHash, startResult{err: errors.New("v2 boom")}).
		// v1 rollback attempt also fails. Note: the original v1 start in s1
		// uses this same key (default success), so the first behavior in this
		// slice must be success; the SECOND start (rollback) fails.
		behaveByProcessHash("v", fp1.ProcessHash,
			startResult{}, // initial s1 start succeeds (no error, no tools → default path)
			startResult{err: errors.New("v1 also gone")}, // rollback fails
		).
		build()

	ctx := context.Background()
	if _, err := rt.SetScene(ctx, "s1"); err != nil {
		t.Fatalf("s1: %v", err)
	}
	result, err := rt.SetScene(ctx, "s2")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if result.Status != StatusPartialFailure {
		t.Errorf("expected partial_failure, got %s", result.Status)
	}
	// `extra` should still be in active — disable was skipped because v's
	// required rollback failed.
	infos := rt.List()
	extraFound := false
	for _, info := range infos {
		if info.Name == "extra" && info.Status == StatusEnabled {
			extraFound = true
		}
	}
	if !extraFound {
		t.Error("extra cap should remain enabled after partial_failure (skipDisable)")
	}
}
