package capxserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oaooao/capx/internal/config"
)

func TestBuildInstructions_FixedContent(t *testing.T) {
	instructions := BuildInstructions(nil)
	for _, want := range []string{
		"Agent Capability Runtime",
		"call scene_info",
		"Scenes shape the agent's default tool choices",
		"search to discover candidates",
		"If the user asks to use, call, invoke, run, or launch",
		"named MCP, CLI, command, tool, or capability",
		"Beyond named requests",
		"describes a capability need",
		"declared-but-inactive capabilities",
		"set_scene returns ok, rejected, or partial_failure",
	} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("instructions should contain %q, got:\n%s", want, instructions)
		}
	}

	for _, notWant := range []string{"capabilities.yaml", "command:", "args:", "env:"} {
		if strings.Contains(instructions, notWant) {
			t.Fatalf("instructions should not include dynamic config detail %q, got:\n%s", notWant, instructions)
		}
	}

	// With nil cfg, the scenes section is omitted.
	if strings.Contains(instructions, "Available scenes") {
		t.Error("nil cfg should omit the Available scenes section")
	}
}

func TestBuildInstructions_ListsScenesAlphabetically(t *testing.T) {
	cfg := &config.Config{
		Scenes: map[string]*config.Scene{
			"writing":   {},
			"default":   {},
			"macos-dev": {},
			"ios-dev":   {},
		},
	}
	instructions := BuildInstructions(cfg)

	if !strings.Contains(instructions, "Available scenes") {
		t.Fatal("instructions should include 'Available scenes' section with scene names")
	}
	for _, name := range []string{"default", "ios-dev", "macos-dev", "writing"} {
		if !strings.Contains(instructions, name) {
			t.Errorf("instructions should list scene %q, got:\n%s", name, instructions)
		}
	}

	// Order is deterministic (alphabetical) so that identical cfgs produce
	// identical instructions across invocations.
	want := "default, ios-dev, macos-dev, writing"
	if !strings.Contains(instructions, want) {
		t.Errorf("scenes should be alphabetically sorted; want contains %q, got:\n%s", want, instructions)
	}
}

func TestBuildInstructions_EmptyScenes(t *testing.T) {
	cfg := &config.Config{Scenes: map[string]*config.Scene{}}
	instructions := BuildInstructions(cfg)
	if strings.Contains(instructions, "Available scenes") {
		t.Error("empty scenes map should omit the Available scenes section")
	}
}

func TestInitializeIncludesInstructions(t *testing.T) {
	cfg := &config.Config{
		Scenes: map[string]*config.Scene{
			"default": {},
		},
	}
	s := newCapxMCPServer(cfg)
	req := mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Request: mcp.Request{Method: "initialize"},
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "test-client",
				Version: "1.0.1",
			},
		},
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	response := s.HandleMessage(context.Background(), payload)
	resp, ok := response.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected JSONRPCResponse, got %T", response)
	}
	result, ok := resp.Result.(mcp.InitializeResult)
	if !ok {
		t.Fatalf("expected InitializeResult, got %T", resp.Result)
	}
	if result.Instructions != BuildInstructions(cfg) {
		t.Fatalf("instructions mismatch:\nwant:\n%s\n\ngot:\n%s", BuildInstructions(cfg), result.Instructions)
	}
}
