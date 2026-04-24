package capxserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestBuildInstructions(t *testing.T) {
	instructions := BuildInstructions()
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
}

func TestInitializeIncludesInstructions(t *testing.T) {
	s := newCapxMCPServer()
	req := mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Request: mcp.Request{Method: "initialize"},
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "test-client",
				Version: "1.0.0",
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
	if result.Instructions != BuildInstructions() {
		t.Fatalf("instructions mismatch:\nwant:\n%s\n\ngot:\n%s", BuildInstructions(), result.Instructions)
	}
}
