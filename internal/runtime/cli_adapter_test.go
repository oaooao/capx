package runtime

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oaooao/capx/internal/config"
)

func TestCLIAdapterStart(t *testing.T) {
	cap := &config.Capability{
		Type:    "cli",
		Command: "echo",
		Tools: map[string]*config.CLITool{
			"hello": {
				Description: "say hello",
				Args:        []string{"hello", "{{name}}"},
				Params: map[string]*config.CLIParam{
					"name": {Type: "string", Required: true, Description: "who to greet"},
				},
			},
			"version": {
				Description: "print version",
				Args:        []string{"--version"},
			},
		},
	}

	adapter := NewCLIAdapter("test-cli", cap)
	tools, err := adapter.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}

	// Check tool names are prefixed.
	names := adapter.ToolNames()
	if len(names) != 2 {
		t.Errorf("expected 2 tool names, got %d", len(names))
	}

	expectedPrefix := "test_cli_"
	for _, name := range names {
		if len(name) < len(expectedPrefix) || name[:len(expectedPrefix)] != expectedPrefix {
			t.Errorf("tool name %q should have prefix %q", name, expectedPrefix)
		}
	}
}

func TestCLIAdapterNoTools(t *testing.T) {
	cap := &config.Capability{
		Type:    "cli",
		Command: "echo",
		Tools:   map[string]*config.CLITool{},
	}

	adapter := NewCLIAdapter("empty", cap)
	_, err := adapter.Start(context.Background())
	if err == nil {
		t.Error("expected error for CLI with no tools")
	}
}

func TestCLIAdapterStop(t *testing.T) {
	adapter := NewCLIAdapter("test", &config.Capability{})
	if err := adapter.Stop(); err != nil {
		t.Errorf("Stop should be no-op, got: %v", err)
	}
}

func TestCLIAdapterPrefixedName(t *testing.T) {
	tests := []struct {
		adapterName string
		toolName    string
		expected    string
	}{
		{"webx", "read", "webx_read"},
		{"my-tool", "run", "my_tool_run"},
		{"a/b", "test", "a_b_test"},
	}

	for _, tt := range tests {
		adapter := NewCLIAdapter(tt.adapterName, &config.Capability{})
		got := adapter.prefixedName(tt.toolName)
		if got != tt.expected {
			t.Errorf("prefixedName(%q, %q) = %q, want %q", tt.adapterName, tt.toolName, got, tt.expected)
		}
	}
}

func TestTemplateArgs(t *testing.T) {
	tests := []struct {
		name     string
		template []string
		params   map[string]any
		expected []string
	}{
		{
			name:     "simple replacement",
			template: []string{"read", "{{url}}", "--format", "markdown"},
			params:   map[string]any{"url": "https://example.com"},
			expected: []string{"read", "https://example.com", "--format", "markdown"},
		},
		{
			name:     "multiple params",
			template: []string{"search", "{{query}}", "--platform", "{{platform}}"},
			params:   map[string]any{"query": "go mcp", "platform": "hn"},
			expected: []string{"search", "go mcp", "--platform", "hn"},
		},
		{
			name:     "missing optional param skips arg",
			template: []string{"cmd", "{{required}}", "--opt", "{{optional}}"},
			params:   map[string]any{"required": "value"},
			expected: []string{"cmd", "value", "--opt"},
		},
		{
			name:     "no params no placeholders",
			template: []string{"echo", "hello"},
			params:   map[string]any{},
			expected: []string{"echo", "hello"},
		},
		{
			name:     "empty template",
			template: []string{},
			params:   map[string]any{"url": "test"},
			expected: []string{},
		},
		{
			name:     "number param",
			template: []string{"--count", "{{n}}"},
			params:   map[string]any{"n": 42},
			expected: []string{"--count", "42"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := templateArgs(tt.template, tt.params)
			if len(got) != len(tt.expected) {
				t.Errorf("len mismatch: got %v, want %v", got, tt.expected)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestCLIAdapterToolExecution(t *testing.T) {
	// Test that a CLI tool actually executes and returns output.
	cap := &config.Capability{
		Type:    "cli",
		Command: "echo",
		Tools: map[string]*config.CLITool{
			"say": {
				Description: "echo something",
				Args:        []string{"{{message}}"},
				Params: map[string]*config.CLIParam{
					"message": {Type: "string", Required: true},
				},
			},
		},
	}

	adapter := NewCLIAdapter("echo-test", cap)
	tools, err := adapter.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	// Call the tool handler.
	req := createToolCallRequest(tools[0].Tool.Name, map[string]any{"message": "hello world"})
	result, err := tools[0].Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Errorf("handler returned error result: %v", result.Content)
	}

	// Check output contains our message.
	if len(result.Content) == 0 {
		t.Fatal("no content in result")
	}
	// Extract text from content - it's a TextContent.
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	text := textContent.Text
	if !containsStr(text, "hello world") {
		t.Errorf("expected output containing 'hello world', got %q", text)
	}
}

func TestCLIAdapterToolExecutionFailure(t *testing.T) {
	cap := &config.Capability{
		Type:    "cli",
		Command: "false", // always exits with code 1
		Tools: map[string]*config.CLITool{
			"fail": {
				Description: "always fails",
				Args:        []string{},
			},
		},
	}

	adapter := NewCLIAdapter("fail-test", cap)
	tools, err := adapter.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	req := createToolCallRequest(tools[0].Tool.Name, map[string]any{})
	result, err := tools[0].Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler should not return Go error, got: %v", err)
	}
	if !result.IsError {
		t.Error("handler should return IsError=true for failed command")
	}
}
