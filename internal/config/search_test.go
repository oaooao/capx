package config

import "testing"

func setupSearchConfig() *Config {
	return &Config{
		Capabilities: map[string]*Capability{
			"playwright": {
				Type: "mcp", Command: "npx", Transport: "stdio",
				Description: "Playwright 浏览器自动化",
				Aliases:     []string{"browser"},
				Keywords:    []string{"browser", "automation", "e2e"},
				Tags:        []string{"browser"},
				Source:      SourceGlobal,
			},
			"webx": {
				Type: "cli", Command: "webx",
				Description: "Web 内容读取",
				Keywords:    []string{"web", "fetch", "scrape"},
				Tags:        []string{"web"},
				Source:      SourceProject,
				Tools: map[string]*CLITool{
					"read":   {Description: "read"},
					"search": {Description: "search"},
				},
			},
			"context7": {
				Type: "mcp", URL: "https://x", Transport: "http",
				Description: "Docs via context7",
				Tags:        []string{"docs"},
				Source:      SourceGlobal,
			},
			"hidden": {
				Type: "cli", Command: "secret",
				Disabled: true,
				Keywords: []string{"browser"},
			},
		},
		Scenes: map[string]*Scene{
			"web": {AutoEnable: AutoEnable{Optional: []string{"playwright", "webx"}}},
		},
	}
}

func TestSearch_AllWhenEmpty(t *testing.T) {
	cfg := setupSearchConfig()
	got, err := cfg.Search(SearchQuery{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// disabled excluded — 3 visible.
	if len(got) != 3 {
		t.Errorf("want 3, got %d: %+v", len(got), got)
	}
}

func TestSearch_QuerySubstring(t *testing.T) {
	cfg := setupSearchConfig()
	got, _ := cfg.Search(SearchQuery{Query: "browser"})
	// "browser" matches playwright's aliases/keywords/tags; webx has no
	// "browser" anywhere; context7 none. Hidden is disabled.
	if len(got) != 1 || got[0].Name != "playwright" {
		t.Errorf("want [playwright], got %+v", got)
	}
}

func TestSearch_TypeFilter(t *testing.T) {
	cfg := setupSearchConfig()
	got, _ := cfg.Search(SearchQuery{Type: "cli"})
	if len(got) != 1 || got[0].Name != "webx" {
		t.Errorf("want [webx], got %+v", got)
	}
}

func TestSearch_TagFilter(t *testing.T) {
	cfg := setupSearchConfig()
	got, _ := cfg.Search(SearchQuery{Tag: "docs"})
	if len(got) != 1 || got[0].Name != "context7" {
		t.Errorf("want [context7], got %+v", got)
	}
}

func TestSearch_SceneScope(t *testing.T) {
	cfg := setupSearchConfig()
	got, err := cfg.Search(SearchQuery{Scene: "web"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// scene "web" scopes to {playwright, webx}.
	if len(got) != 2 {
		t.Errorf("want 2, got %d: %+v", len(got), got)
	}
}

func TestSearch_ExcludesDisabled(t *testing.T) {
	cfg := setupSearchConfig()
	got, _ := cfg.Search(SearchQuery{Query: "hidden"})
	if len(got) != 0 {
		t.Errorf("disabled should be invisible to search, got %+v", got)
	}
}

// ------------------------------------------------------------------

func TestDescribe_Global(t *testing.T) {
	cfg := setupSearchConfig()
	d, err := cfg.Describe("webx", "")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if d.Type != "cli" {
		t.Errorf("type: %q", d.Type)
	}
	if d.Source != string(SourceProject) {
		t.Errorf("source: want project, got %q", d.Source)
	}
	// CLI tool names are surfaced.
	if len(d.Tools) != 2 {
		t.Errorf("tools: want 2, got %+v", d.Tools)
	}
	if d.ExampleInvocation == "" {
		t.Error("example_invocation should be populated")
	}
}

func TestDescribe_SceneInlineWins(t *testing.T) {
	inline := &Capability{
		Type: "mcp", Command: "npx", Transport: "stdio",
		Description: "playwright headless",
	}
	cfg := &Config{
		Capabilities: map[string]*Capability{
			"playwright": {Type: "mcp", Command: "npx", Description: "playwright default", Source: SourceGlobal},
		},
		Scenes: map[string]*Scene{
			"web-custom": {
				Capabilities: map[string]*Capability{"playwright": inline},
				AutoEnable:   AutoEnable{Optional: []string{"playwright"}},
			},
		},
	}
	d, err := cfg.Describe("playwright", "web-custom")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if d.Description != "playwright headless" {
		t.Errorf("description should reflect inline override, got %q", d.Description)
	}
	if d.Source != "scene:web-custom (inline)" {
		t.Errorf("source: got %q", d.Source)
	}
}

func TestDescribe_NotFound(t *testing.T) {
	cfg := setupSearchConfig()
	_, err := cfg.Describe("missing", "")
	if err == nil {
		t.Error("expected not-found error")
	}
}
