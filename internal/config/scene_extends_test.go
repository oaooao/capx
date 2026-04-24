package config

import (
	"strings"
	"testing"
)

func TestExpandScene_NoExtends(t *testing.T) {
	cfg := &Config{Scenes: map[string]*Scene{
		"s": {AutoEnable: AutoEnable{Required: []string{"a"}, Optional: []string{"b"}}},
	}}
	exp, err := cfg.ExpandScene("s")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if len(exp.AutoEnable.Required) != 1 || exp.AutoEnable.Required[0] != "a" {
		t.Errorf("required: %+v", exp.AutoEnable.Required)
	}
	if len(exp.AutoEnable.Optional) != 1 || exp.AutoEnable.Optional[0] != "b" {
		t.Errorf("optional: %+v", exp.AutoEnable.Optional)
	}
	if len(exp.Lineage) != 1 || exp.Lineage[0] != "s" {
		t.Errorf("lineage: %+v", exp.Lineage)
	}
}

func TestExpandScene_SimpleParent(t *testing.T) {
	cfg := &Config{Scenes: map[string]*Scene{
		"base":  {AutoEnable: AutoEnable{Optional: []string{"a"}}},
		"child": {Extends: []string{"base"}, AutoEnable: AutoEnable{Optional: []string{"b"}}},
	}}
	exp, err := cfg.ExpandScene("child")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	opts := exp.AutoEnable.Optional
	if len(opts) != 2 || opts[0] != "a" || opts[1] != "b" {
		t.Errorf("optional order: %+v (want [a b] — parent before child)", opts)
	}
	if exp.Lineage[0] != "base" || exp.Lineage[1] != "child" {
		t.Errorf("lineage order: %+v", exp.Lineage)
	}
}

func TestExpandScene_ChildRequiredPromotesParentOptional(t *testing.T) {
	cfg := &Config{Scenes: map[string]*Scene{
		"base":  {AutoEnable: AutoEnable{Optional: []string{"x"}}},
		"child": {Extends: []string{"base"}, AutoEnable: AutoEnable{Required: []string{"x"}}},
	}}
	exp, err := cfg.ExpandScene("child")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if len(exp.AutoEnable.Required) != 1 || exp.AutoEnable.Required[0] != "x" {
		t.Errorf("required should include x (child upgraded): %+v", exp.AutoEnable.Required)
	}
	if len(exp.AutoEnable.Optional) != 0 {
		t.Errorf("optional should be empty after upgrade: %+v", exp.AutoEnable.Optional)
	}
}

func TestExpandScene_ChildCannotDemoteParentRequired(t *testing.T) {
	// Per §A.6: optional parent → required child → required.
	// required parent + optional child → still required (child cannot demote).
	cfg := &Config{Scenes: map[string]*Scene{
		"base":  {AutoEnable: AutoEnable{Required: []string{"x"}}},
		"child": {Extends: []string{"base"}, AutoEnable: AutoEnable{Optional: []string{"x"}}},
	}}
	exp, err := cfg.ExpandScene("child")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if len(exp.AutoEnable.Required) != 1 || exp.AutoEnable.Required[0] != "x" {
		t.Errorf("required parent must not be demoted: %+v", exp.AutoEnable.Required)
	}
}

func TestExpandScene_InlineCapabilityOverride(t *testing.T) {
	parentCap := &Capability{Type: "mcp", URL: "http://parent"}
	childCap := &Capability{Type: "mcp", URL: "http://child"}
	cfg := &Config{Scenes: map[string]*Scene{
		"base":  {Capabilities: map[string]*Capability{"p": parentCap}},
		"child": {Extends: []string{"base"}, Capabilities: map[string]*Capability{"p": childCap}},
	}}
	exp, err := cfg.ExpandScene("child")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if exp.Capabilities["p"] != childCap {
		t.Errorf("child inline cap must replace parent's same-name inline")
	}
}

func TestExpandScene_CycleDetection(t *testing.T) {
	cfg := &Config{Scenes: map[string]*Scene{
		"a": {Extends: []string{"b"}},
		"b": {Extends: []string{"a"}},
	}}
	_, err := cfg.ExpandScene("a")
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected cycle error, got %v", err)
	}
}

func TestExpandScene_MissingParent(t *testing.T) {
	cfg := &Config{Scenes: map[string]*Scene{
		"a": {Extends: []string{"ghost"}},
	}}
	_, err := cfg.ExpandScene("a")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected missing-parent error, got %v", err)
	}
}

func TestExpandScene_DiamondKeepsFirstOccurrence(t *testing.T) {
	// Diamond: D extends [B, C]; B extends A; C extends A.
	// Linearization (DFS + first-seen): A, B, A (skip), C, D → [A, B, C, D].
	cfg := &Config{Scenes: map[string]*Scene{
		"A": {AutoEnable: AutoEnable{Optional: []string{"a"}}},
		"B": {Extends: []string{"A"}, AutoEnable: AutoEnable{Optional: []string{"b"}}},
		"C": {Extends: []string{"A"}, AutoEnable: AutoEnable{Optional: []string{"c"}}},
		"D": {Extends: []string{"B", "C"}, AutoEnable: AutoEnable{Optional: []string{"d"}}},
	}}
	exp, err := cfg.ExpandScene("D")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	wantLineage := []string{"A", "B", "C", "D"}
	if len(exp.Lineage) != len(wantLineage) {
		t.Fatalf("lineage len: want %d got %d (%v)", len(wantLineage), len(exp.Lineage), exp.Lineage)
	}
	for i, n := range wantLineage {
		if exp.Lineage[i] != n {
			t.Errorf("lineage[%d]: want %q got %q", i, n, exp.Lineage[i])
		}
	}
}

func TestExpandScene_DescriptionFromDeepestChild(t *testing.T) {
	cfg := &Config{Scenes: map[string]*Scene{
		"parent": {Description: "parent desc", AutoEnable: AutoEnable{Optional: []string{"a"}}},
		"child":  {Extends: []string{"parent"}, Description: "child desc"},
	}}
	exp, err := cfg.ExpandScene("child")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if exp.Description != "child desc" {
		t.Errorf("description: want child-desc, got %q", exp.Description)
	}
}

func TestExpandScene_EmptyChildDescriptionInheritsParent(t *testing.T) {
	cfg := &Config{Scenes: map[string]*Scene{
		"parent": {Description: "parent desc", AutoEnable: AutoEnable{Optional: []string{"a"}}},
		"child":  {Extends: []string{"parent"}},
	}}
	exp, err := cfg.ExpandScene("child")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if exp.Description != "parent desc" {
		t.Errorf("empty child description should inherit parent, got %q", exp.Description)
	}
}
