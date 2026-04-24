package config

import (
	"fmt"
	"sort"
	"strings"
)

// SearchResult is one entry in the Level-1 search response (§A.10).
type SearchResult struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
}

// SearchQuery bundles the optional filter dimensions (§A.10).
// Any field may be empty; an all-empty query returns every visible cap so
// prompt-easy / typefree can pull a full dictionary.
type SearchQuery struct {
	Query string // substring match against name, description, aliases, keywords
	Type  string // "mcp" | "cli" exact match
	Tag   string // tag membership
	Scene string // if set, limit to caps visible in that scene
}

// Search returns matching capabilities per §A.10 Level 1. Matching is lenient:
// lowercase substring over {name, description, each alias, each keyword}, AND
// the type/tag/scene filters when present.
//
// Results are sorted by name for stable output.
func (c *Config) Search(q SearchQuery) ([]SearchResult, error) {
	// Scene-scoped search needs the expanded scene; resolve it up front.
	var sceneAllowed map[string]bool
	if q.Scene != "" {
		effective, err := c.EffectiveScene(q.Scene)
		if err != nil {
			return nil, err
		}
		sceneAllowed = make(map[string]bool, len(effective))
		for _, ec := range effective {
			sceneAllowed[ec.Name] = true
		}
	}

	needle := strings.ToLower(q.Query)
	var out []SearchResult
	for name, cap := range c.Capabilities {
		if cap.Disabled {
			continue
		}
		if q.Type != "" && cap.Type != q.Type {
			continue
		}
		if q.Tag != "" && !containsString(cap.Tags, q.Tag) {
			continue
		}
		if sceneAllowed != nil && !sceneAllowed[name] {
			continue
		}
		if needle != "" && !matchesNeedle(name, cap, needle) {
			continue
		}
		out = append(out, SearchResult{
			Name:    name,
			Type:    cap.Type,
			Summary: cap.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func matchesNeedle(name string, cap *Capability, needle string) bool {
	if strings.Contains(strings.ToLower(name), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(cap.Description), needle) {
		return true
	}
	for _, a := range cap.Aliases {
		if strings.Contains(strings.ToLower(a), needle) {
			return true
		}
	}
	for _, k := range cap.Keywords {
		if strings.Contains(strings.ToLower(k), needle) {
			return true
		}
	}
	return false
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------
// Describe — Level 2 (§A.10)
// ------------------------------------------------------------------

// DescribeResult mirrors the §A.10 Level-2 response. `Tools` is only
// populated for CLI caps and MCP caps whose tool list was declared in YAML;
// MCP tools that live on the upstream server are discoverable via ToolSearch
// and not listed here.
type DescribeResult struct {
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	Transport         string   `json:"transport,omitempty"`
	Description       string   `json:"description"`
	Source            string   `json:"source"`
	Aliases           []string `json:"aliases,omitempty"`
	Keywords          []string `json:"keywords,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	Tools             []string `json:"tools,omitempty"`
	ExampleInvocation string   `json:"example_invocation"`
}

// Describe returns the Level-2 details for a capability, optionally resolved
// against a specific scene (to disambiguate inline overrides, §A.10).
//
// If scene is "", the global-merged Capability is returned. Otherwise the
// scene's extends-expanded inline map takes precedence over the global one.
func (c *Config) Describe(name, scene string) (*DescribeResult, error) {
	var cap *Capability
	source := string(SourceGlobal) // default before we know better

	if scene != "" {
		expanded, err := c.ExpandScene(scene)
		if err != nil {
			return nil, err
		}
		if inline, ok := expanded.Capabilities[name]; ok {
			cap = inline
			source = fmt.Sprintf("scene:%s (inline)", scene)
		}
	}
	if cap == nil {
		if c.Capabilities == nil {
			return nil, fmt.Errorf("capability %q not found", name)
		}
		target, ok := c.Capabilities[name]
		if !ok {
			if scene != "" {
				return nil, fmt.Errorf("capability %q not found in scene %q or in global config", name, scene)
			}
			return nil, fmt.Errorf("capability %q not found", name)
		}
		cap = target
		source = string(target.Source)
		if source == "" {
			source = "unknown"
		}
	}

	result := &DescribeResult{
		Name:        name,
		Type:        cap.Type,
		Transport:   cap.Transport,
		Description: cap.Description,
		Source:      source,
		Aliases:     cap.Aliases,
		Keywords:    cap.Keywords,
		Tags:        cap.Tags,
	}
	// Tool list: for CLI, use declared tool names. For MCP stdio/http, the
	// upstream server owns the schema — we emit nothing and the consumer
	// falls back to ToolSearch.
	if cap.Type == "cli" && len(cap.Tools) > 0 {
		names := make([]string, 0, len(cap.Tools))
		for n := range cap.Tools {
			names = append(names, n)
		}
		sort.Strings(names)
		result.Tools = names
	}
	result.ExampleInvocation = fmt.Sprintf("mcp__capx__enable %s", name)
	return result, nil
}
