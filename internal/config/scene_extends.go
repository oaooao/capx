package config

import (
	"fmt"
	"strings"
)

// ExpandedScene is a scene with its extends chain fully resolved into a
// single flat view — the shape EffectiveScene then walks per-name.
//
// Fields mirror Scene but are deterministic products of the §A.6 rules:
//   - DFS linearization, left-to-right, first-occurrence kept
//   - auto_enable: required wins (child cannot demote parent's required);
//     optional is preserved if no required override appears anywhere
//   - inline capabilities: child replaces parent's same-name entry
type ExpandedScene struct {
	Name         string
	Description  string
	Aliases      []string
	Tags         []string
	AutoEnable   AutoEnable
	Capabilities map[string]*Capability
	// Lineage is the linearization in application order (parents first,
	// self last). Useful for diagnostics and `capx where` output.
	Lineage []string
}

// ExpandScene resolves a scene's extends chain and returns the flat view.
// Returns an error on cycle detection, missing parent, or any contradiction
// the caller would otherwise silently paper over.
//
// The linearization algorithm (per §A.6):
//
//	dfs(s):
//	  for p in s.extends: dfs(p)          # left-to-right, depth-first
//	  if s not yet in visited: append(visited, s)
//
// C3 (Python-style MRO) is explicitly not used — overkill for YAML config.
func (c *Config) ExpandScene(name string) (*ExpandedScene, error) {
	if _, ok := c.Scenes[name]; !ok {
		return nil, fmt.Errorf("scene %q not found", name)
	}

	var (
		visited []string            // linearization output
		inStack = map[string]bool{} // cycle detection
		stack   []string            // for cycle diagnostic
	)
	seen := map[string]bool{}

	var dfs func(string) error
	dfs = func(s string) error {
		if inStack[s] {
			// Re-slice the stack from the reoccurrence of s to produce a
			// readable cycle path in the error message.
			start := 0
			for i, n := range stack {
				if n == s {
					start = i
					break
				}
			}
			return fmt.Errorf("cycle in extends: %s", strings.Join(append(stack[start:], s), " → "))
		}
		scene, ok := c.Scenes[s]
		if !ok {
			return fmt.Errorf("extends: scene %q not found (referenced in chain %s)",
				s, strings.Join(append(stack, s), " → "))
		}
		inStack[s] = true
		stack = append(stack, s)
		for _, parent := range scene.Extends {
			if err := dfs(parent); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		inStack[s] = false
		if !seen[s] {
			seen[s] = true
			visited = append(visited, s)
		}
		return nil
	}
	if err := dfs(name); err != nil {
		return nil, err
	}

	// Walk linearization and merge.
	out := &ExpandedScene{
		Name:         name,
		Capabilities: map[string]*Capability{},
		Lineage:      append([]string(nil), visited...),
	}

	// Track auto_enable in insertion order but allow "optional → required" upgrades.
	type entry struct {
		name     string
		required bool
	}
	order := []entry{}
	pos := map[string]int{} // name → index in order

	for _, sname := range visited {
		s := c.Scenes[sname]

		// Description: child wins (self is last in linearization).
		if s.Description != "" {
			out.Description = s.Description
		}
		// Tags / Aliases: accumulate unique, preserve first-seen order.
		out.Tags = mergeUniqueStrings(out.Tags, s.Tags)
		out.Aliases = mergeUniqueStrings(out.Aliases, s.Aliases)

		// auto_enable merge with required-wins semantics.
		for _, n := range s.AutoEnable.Required {
			if idx, ok := pos[n]; ok {
				order[idx].required = true
			} else {
				pos[n] = len(order)
				order = append(order, entry{name: n, required: true})
			}
		}
		for _, n := range s.AutoEnable.Optional {
			if _, ok := pos[n]; ok {
				// Already seen — if it was required, stays required; else stays optional.
				continue
			}
			pos[n] = len(order)
			order = append(order, entry{name: n, required: false})
		}

		// Inline capabilities: child replaces parent by name (§A.8 replace).
		for cn, cp := range s.Capabilities {
			out.Capabilities[cn] = cp
		}
	}

	// Split back into required/optional lists preserving declaration order.
	for _, e := range order {
		if e.required {
			out.AutoEnable.Required = append(out.AutoEnable.Required, e.name)
		} else {
			out.AutoEnable.Optional = append(out.AutoEnable.Optional, e.name)
		}
	}

	return out, nil
}

// mergeUniqueStrings appends b's new entries (not already in a) to a,
// preserving a's order then appending b's new entries in order.
func mergeUniqueStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a))
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if seen[s] {
			continue
		}
		seen[s] = true
		a = append(a, s)
	}
	return a
}
