package config

import "fmt"

// EffectiveCap is one capability resolved for a scene: the concrete config
// the runtime should load, plus the required/optional classification from
// the scene's AutoEnable.
type EffectiveCap struct {
	Name       string
	Capability *Capability
	Required   bool
}

// EffectiveScene resolves a scene name into the ordered list of capabilities
// that should be active when the scene is selected.
//
// Flow:
//  1. ExpandScene walks the extends chain (§A.6 DFS + cycle detect) and
//     merges required/optional lists + inline capabilities per the rules.
//  2. For each name in the expanded AutoEnable, resolve the cap:
//     inline scene.Capabilities wins over global cfg.Capabilities (§A.8).
//
// Errors:
//   - extends cycle or missing parent → error from ExpandScene
//   - required name missing from both inline and global → error
//   - optional name missing → skipped silently
//   - required cap disabled → error; optional disabled → skipped
//   - legacy "all" sentinel (v0.1 single entry "all") expands to every
//     non-disabled cfg.Capabilities as optional
func (c *Config) EffectiveScene(sceneName string) ([]EffectiveCap, error) {
	expanded, err := c.ExpandScene(sceneName)
	if err != nil {
		return nil, err
	}

	// Legacy "all" sentinel.
	all := expanded.AutoEnable.All()
	if len(all) == 1 && all[0] == "all" {
		out := make([]EffectiveCap, 0, len(c.Capabilities))
		for name, cap := range c.Capabilities {
			if cap.Disabled {
				continue
			}
			out = append(out, EffectiveCap{Name: name, Capability: cap, Required: false})
		}
		return out, nil
	}

	total := len(expanded.AutoEnable.Required) + len(expanded.AutoEnable.Optional)
	seen := make(map[string]bool, total)
	out := make([]EffectiveCap, 0, total)

	resolve := func(name string, required bool) error {
		if seen[name] {
			return nil
		}
		seen[name] = true

		var target *Capability
		if inline, ok := expanded.Capabilities[name]; ok {
			target = inline
		}
		if target == nil {
			target = c.Capabilities[name]
		}
		if target == nil {
			if required {
				return fmt.Errorf("scene %q: required capability %q not defined anywhere", sceneName, name)
			}
			return nil
		}
		if target.Disabled {
			if required {
				return fmt.Errorf("scene %q: required capability %q is disabled", sceneName, name)
			}
			return nil
		}
		out = append(out, EffectiveCap{Name: name, Capability: target, Required: required})
		return nil
	}

	for _, name := range expanded.AutoEnable.Required {
		if err := resolve(name, true); err != nil {
			return nil, err
		}
	}
	for _, name := range expanded.AutoEnable.Optional {
		if err := resolve(name, false); err != nil {
			return nil, err
		}
	}
	return out, nil
}
