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
// B2 scope: we read the scene's own AutoEnable (required + optional) and
// resolve each name against inline scene.Capabilities (preferred) then the
// global cfg.Capabilities. We do NOT yet walk extends chains — that lands
// in B3 (§A.6 extends: DFS linearization + cycle detection).
//
// Rules:
//   - inline scene.Capabilities[name] wins over cfg.Capabilities[name]
//     (§A.8: scene-inline is highest precedence for same name)
//   - required names missing from both sources → error (scene unusable)
//   - optional names missing → skipped silently (Agent workbench tolerates
//     holes in optional cap)
//   - duplicate names across required and optional → required wins
//   - "all" legacy sentinel (v0.1): single required entry "all" expands to
//     every non-disabled cfg.Capabilities as optional
func (c *Config) EffectiveScene(sceneName string) ([]EffectiveCap, error) {
	scene, ok := c.Scenes[sceneName]
	if !ok {
		return nil, fmt.Errorf("scene %q not found", sceneName)
	}

	// Legacy "all" sentinel — preserve v0.1 semantics.
	allNames := scene.AutoEnable.All()
	if len(allNames) == 1 && allNames[0] == "all" {
		out := make([]EffectiveCap, 0, len(c.Capabilities))
		for name, cap := range c.Capabilities {
			if cap.Disabled {
				continue
			}
			out = append(out, EffectiveCap{Name: name, Capability: cap, Required: false})
		}
		return out, nil
	}

	total := len(scene.AutoEnable.Required) + len(scene.AutoEnable.Optional)
	seen := make(map[string]bool, total)
	out := make([]EffectiveCap, 0, total)

	resolve := func(name string, required bool) error {
		if seen[name] {
			return nil
		}
		seen[name] = true

		var target *Capability
		if scene.Capabilities != nil {
			if inline, ok := scene.Capabilities[name]; ok {
				target = inline
			}
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

	for _, name := range scene.AutoEnable.Required {
		if err := resolve(name, true); err != nil {
			return nil, err
		}
	}
	for _, name := range scene.AutoEnable.Optional {
		if err := resolve(name, false); err != nil {
			return nil, err
		}
	}
	return out, nil
}
