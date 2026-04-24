package config

import (
	"fmt"
	"strings"
)

// ValidateAndInferTransport applies the §A.5 deterministic rules:
//
//   - transport may be omitted for type: mcp; inferred from command vs url.
//   - command and url are mutually exclusive for mcp; exactly one must be present.
//   - explicit transport must match the fields actually set.
//   - cli capability: command is required; transport/url are rejected.
//
// On success, cap.Transport is populated (even if it was omitted in YAML) so
// downstream consumers never have to re-run inference.
func ValidateAndInferTransport(name string, c *Capability) error {
	if strings.Contains(name, "__") {
		return fmt.Errorf(
			"capability %q: name must not contain `__` (reserved as namespace separator between capability and tool in exposed MCP tool names)",
			name,
		)
	}
	switch c.Type {
	case "":
		return fmt.Errorf("capability %q: missing required field `type`", name)
	case "mcp":
		return validateMCP(name, c)
	case "cli":
		return validateCLI(name, c)
	default:
		return fmt.Errorf("capability %q: unknown type %q (expected mcp | cli)", name, c.Type)
	}
}

func validateMCP(name string, c *Capability) error {
	hasCommand := c.Command != ""
	hasURL := c.URL != ""

	if hasCommand && hasURL {
		return fmt.Errorf("capability %q: command and url are mutually exclusive for type: mcp", name)
	}
	if !hasCommand && !hasURL {
		return fmt.Errorf("capability %q: type: mcp requires either command (stdio) or url (http)", name)
	}

	// Inference.
	inferred := "stdio"
	if hasURL {
		inferred = "http"
	}

	if c.Transport == "" {
		c.Transport = inferred
		return nil
	}
	// Explicit transport: must match the fields actually present.
	switch c.Transport {
	case "stdio":
		if !hasCommand {
			return fmt.Errorf(
				"capability %q: transport: stdio requires command (got url instead)", name)
		}
	case "http":
		if !hasURL {
			return fmt.Errorf(
				"capability %q: transport: http requires url (got command instead)", name)
		}
	default:
		return fmt.Errorf("capability %q: unknown transport %q (expected stdio | http)", name, c.Transport)
	}
	return nil
}

func validateCLI(name string, c *Capability) error {
	if c.Command == "" {
		return fmt.Errorf("capability %q: type: cli requires command", name)
	}
	if c.URL != "" {
		return fmt.Errorf("capability %q: type: cli rejects url field", name)
	}
	if c.Transport != "" {
		return fmt.Errorf("capability %q: type: cli rejects transport field (reserved for type: mcp)", name)
	}
	return nil
}

// ValidateAllCapabilities runs ValidateAndInferTransport against every
// capability in the config and also enforces the §A.5 cross-capability
// aliases uniqueness rule: a single alias may appear on at most one visible
// capability. Disabled capabilities are excluded from the alias conflict
// check (they can't be selected anyway).
//
// Returns the first error encountered; stops early on hard schema violations.
// Alias conflicts accumulate into a single multi-line error so the user can
// see every colliding pair in one shot.
func ValidateAllCapabilities(c *Config) error {
	// 1. Per-capability schema validation (hard failures).
	for name, cap := range c.Capabilities {
		if err := ValidateAndInferTransport(name, cap); err != nil {
			return err
		}
	}
	// 2. Alias conflict scan across visible (non-disabled) capabilities.
	// alias → []name. Any bucket with >1 entry is a conflict.
	aliasOwners := make(map[string][]string)
	for name, cap := range c.Capabilities {
		if cap.Disabled {
			continue
		}
		for _, alias := range cap.Aliases {
			aliasOwners[alias] = append(aliasOwners[alias], name)
		}
		// An alias that collides with a capability NAME is equally ambiguous
		// (`enable <name_or_alias>` has to disambiguate). Treat name as an
		// implicit self-alias by inserting it now.
	}
	// Names themselves occupy the alias space — an alias must not collide
	// with any capability name other than its owner.
	for _, alias := range collectKeys(aliasOwners) {
		owners := aliasOwners[alias]
		if nameCap, nameExists := c.Capabilities[alias]; nameExists && !nameCap.Disabled {
			// alias "X" matches another cap whose name is "X"; owner list must
			// only be that cap itself (self-alias is harmless but also useless).
			for _, owner := range owners {
				if owner != alias {
					return fmt.Errorf(
						"alias conflict: %q is declared as alias of %q but is also the name of a different capability",
						alias, owner,
					)
				}
			}
		}
	}
	var conflictMsg string
	for alias, owners := range aliasOwners {
		if len(owners) > 1 {
			conflictMsg += fmt.Sprintf(
				"\n  alias %q is declared by multiple capabilities: %v", alias, owners)
		}
	}
	if conflictMsg != "" {
		return fmt.Errorf("alias conflicts:%s", conflictMsg)
	}
	return nil
}

func collectKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
