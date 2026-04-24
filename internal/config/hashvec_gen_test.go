package config

// Regenerate testdata/hash_vectors/manifest.json from the YAML vectors.
// Run with:  go test ./internal/config -run TestGenerateGoldenManifest -generate
//
// Default behavior is Skip(): this test is a maintenance utility, not a gate.

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var generateManifest = flag.Bool("generate", false, "regenerate hash_vectors manifest.json")

func TestGenerateGoldenManifest(t *testing.T) {
	if !*generateManifest {
		t.Skip("set -generate to regenerate manifest.json")
	}

	const dir = "testdata/hash_vectors"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	out := make(map[string]Fingerprint)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		key := strings.TrimSuffix(name, ".yaml")
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var cap Capability
		if err := yaml.Unmarshal(data, &cap); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		fp, err := Fingerprints(&cap)
		if err != nil {
			t.Fatalf("hash %s: %v", name, err)
		}
		out[key] = fp
	}

	// Deterministic output: sort keys for stable git diffs.
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ordered := make(map[string]Fingerprint, len(out))
	for _, k := range keys {
		ordered[k] = out[k]
	}

	b, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	t.Logf("wrote %d entries", len(ordered))
}
