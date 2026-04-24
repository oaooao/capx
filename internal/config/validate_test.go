package config

import (
	"strings"
	"testing"
)

func TestValidateTransport_InferStdioFromCommand(t *testing.T) {
	c := &Capability{Type: "mcp", Command: "npx"}
	if err := ValidateAndInferTransport("x", c); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if c.Transport != "stdio" {
		t.Errorf("transport: want stdio, got %q", c.Transport)
	}
}

func TestValidateTransport_InferHTTPFromURL(t *testing.T) {
	c := &Capability{Type: "mcp", URL: "https://x"}
	if err := ValidateAndInferTransport("x", c); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if c.Transport != "http" {
		t.Errorf("transport: want http, got %q", c.Transport)
	}
}

func TestValidateTransport_ExplicitStdioMatchesCommand(t *testing.T) {
	c := &Capability{Type: "mcp", Transport: "stdio", Command: "npx"}
	if err := ValidateAndInferTransport("x", c); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestValidateTransport_ExplicitHTTPMismatchCommand(t *testing.T) {
	c := &Capability{Type: "mcp", Transport: "http", Command: "npx"}
	err := ValidateAndInferTransport("x", c)
	if err == nil || !strings.Contains(err.Error(), "transport: http") {
		t.Errorf("expected transport mismatch error, got %v", err)
	}
}

func TestValidateTransport_MutuallyExclusive(t *testing.T) {
	c := &Capability{Type: "mcp", Command: "npx", URL: "https://x"}
	err := ValidateAndInferTransport("x", c)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutex error, got %v", err)
	}
}

func TestValidateTransport_Missing(t *testing.T) {
	c := &Capability{Type: "mcp"}
	err := ValidateAndInferTransport("x", c)
	if err == nil {
		t.Error("expected error when both command and url are missing")
	}
}

func TestValidateTransport_CLIRejectsURL(t *testing.T) {
	c := &Capability{Type: "cli", Command: "echo", URL: "https://x"}
	err := ValidateAndInferTransport("x", c)
	if err == nil || !strings.Contains(err.Error(), "rejects url") {
		t.Errorf("expected cli-rejects-url error, got %v", err)
	}
}

func TestValidateTransport_UnknownType(t *testing.T) {
	c := &Capability{Type: "weird"}
	err := ValidateAndInferTransport("x", c)
	if err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Errorf("expected unknown-type error, got %v", err)
	}
}

func TestValidateAllCapabilities_AliasConflict(t *testing.T) {
	cfg := &Config{
		Capabilities: map[string]*Capability{
			"playwright": {Type: "mcp", Command: "npx", Aliases: []string{"browser"}},
			"puppeteer":  {Type: "mcp", Command: "npx", Aliases: []string{"browser"}},
		},
	}
	err := ValidateAllCapabilities(cfg)
	if err == nil || !strings.Contains(err.Error(), "alias conflicts") {
		t.Errorf("expected alias conflict, got %v", err)
	}
}

func TestValidateAllCapabilities_AliasCollidesWithOtherName(t *testing.T) {
	cfg := &Config{
		Capabilities: map[string]*Capability{
			"playwright": {Type: "mcp", Command: "npx", Aliases: []string{"puppeteer"}},
			"puppeteer":  {Type: "mcp", Command: "npx"},
		},
	}
	err := ValidateAllCapabilities(cfg)
	if err == nil || !strings.Contains(err.Error(), "alias") {
		t.Errorf("expected alias-vs-name collision, got %v", err)
	}
}

func TestValidateAllCapabilities_DisabledCapsExcludedFromAliasCheck(t *testing.T) {
	cfg := &Config{
		Capabilities: map[string]*Capability{
			"playwright":    {Type: "mcp", Command: "npx", Aliases: []string{"browser"}},
			"puppeteer-old": {Type: "mcp", Command: "npx", Disabled: true, Aliases: []string{"browser"}},
		},
	}
	if err := ValidateAllCapabilities(cfg); err != nil {
		t.Errorf("disabled cap's alias should not conflict, got %v", err)
	}
}

func TestValidateAllCapabilities_SelfAliasAllowed(t *testing.T) {
	// A cap declaring its own name as alias is useless but not a conflict.
	cfg := &Config{
		Capabilities: map[string]*Capability{
			"playwright": {Type: "mcp", Command: "npx", Aliases: []string{"playwright"}},
		},
	}
	if err := ValidateAllCapabilities(cfg); err != nil {
		t.Errorf("self-alias should not conflict, got %v", err)
	}
}
