package setup

import (
	"testing"
)

func TestExtractTomlString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`command = "npx"`, "npx"},
		{`url = "https://example.com"`, "https://example.com"},
		{`command = 'single-quoted'`, "single-quoted"},
		{`key = value_no_quotes`, "value_no_quotes"},
		{`no_equals_sign`, ""},
	}

	for _, tt := range tests {
		got := extractTomlString(tt.input)
		if got != tt.expected {
			t.Errorf("extractTomlString(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExtractTomlStringArray(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{`args = ["mcp"]`, []string{"mcp"}},
		{`args = ["--flag", "value"]`, []string{"--flag", "value"}},
		{`args = []`, nil},
		{`no_equals`, nil},
	}

	for _, tt := range tests {
		got := extractTomlStringArray(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("extractTomlStringArray(%q) len = %d, want %d", tt.input, len(got), len(tt.expected))
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("extractTomlStringArray(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}
