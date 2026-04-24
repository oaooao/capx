package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// sha256NullHex is the fixed tools_hash for capabilities with no declared tools
// (all MCP capabilities, and CLI capabilities that omit the tools field).
// Computed as sha256("null") where "null" is the JSON canonicalization of a
// nil tools map per §A.12.
var sha256NullHex = func() string {
	sum := sha256.Sum256([]byte("null"))
	return hex.EncodeToString(sum[:])
}()

// -----------------------------------------------------------------------------
// Equivalence — runtime-equivalent configs must produce identical hashes.
// -----------------------------------------------------------------------------

func TestProcessHash_EquivalentEmptyCollections(t *testing.T) {
	// §A.12: "等价配置必须产生同一 process_hash". The minimal config and the
	// "explicit empty" config should hash identically.
	a := &Capability{
		Type: "mcp",
		URL:  "https://mcp.context7.com/mcp",
	}
	b := &Capability{
		Type:        "mcp",
		URL:         "https://mcp.context7.com/mcp",
		Args:        []string{},
		Env:         map[string]string{},
		RequiredEnv: []string{},
	}
	ha, err := ProcessHash(a)
	if err != nil {
		t.Fatalf("ProcessHash(a): %v", err)
	}
	hb, err := ProcessHash(b)
	if err != nil {
		t.Fatalf("ProcessHash(b): %v", err)
	}
	if ha != hb {
		t.Errorf("equivalent configs produced different process_hash:\n  a=%s\n  b=%s", ha, hb)
	}
}

func TestProcessHash_EnvKeyOrderIrrelevant(t *testing.T) {
	// Same env map, different insertion order — hash must match.
	a := &Capability{
		Type:    "cli",
		Command: "echo",
		Env:     map[string]string{"A": "1", "B": "2", "C": "3"},
	}
	b := &Capability{
		Type:    "cli",
		Command: "echo",
		Env:     map[string]string{"C": "3", "A": "1", "B": "2"},
	}
	ha, _ := ProcessHash(a)
	hb, _ := ProcessHash(b)
	if ha != hb {
		t.Errorf("env key order must not affect hash:\n  a=%s\n  b=%s", ha, hb)
	}
}

func TestProcessHash_RequiredEnvDedupAndSort(t *testing.T) {
	a := &Capability{
		Type:        "mcp",
		URL:         "https://example.com",
		RequiredEnv: []string{"FOO", "BAR", "FOO", "BAZ"},
	}
	b := &Capability{
		Type:        "mcp",
		URL:         "https://example.com",
		RequiredEnv: []string{"BAZ", "FOO", "BAR"},
	}
	ha, _ := ProcessHash(a)
	hb, _ := ProcessHash(b)
	if ha != hb {
		t.Errorf("required_env dedup+sort must canonicalize duplicates/reordering:\n  a=%s\n  b=%s", ha, hb)
	}
}

func TestProcessHash_ArgsOrderSensitive(t *testing.T) {
	// args carry semantic order (they're argv[1:]) — reordering is a real change.
	a := &Capability{Type: "cli", Command: "echo", Args: []string{"hello", "world"}}
	b := &Capability{Type: "cli", Command: "echo", Args: []string{"world", "hello"}}
	ha, _ := ProcessHash(a)
	hb, _ := ProcessHash(b)
	if ha == hb {
		t.Errorf("args order is semantic — reordering must change process_hash (got %s for both)", ha)
	}
}

func TestToolsHash_EquivalentEmptyDescription(t *testing.T) {
	// description: "" ≡ missing per §A.12
	a := &Capability{
		Type: "cli", Command: "echo",
		Tools: map[string]*CLITool{
			"run": {Description: "", Args: []string{"run"}},
		},
	}
	b := &Capability{
		Type: "cli", Command: "echo",
		Tools: map[string]*CLITool{
			"run": {Args: []string{"run"}},
		},
	}
	ha, _ := ToolsHash(a)
	hb, _ := ToolsHash(b)
	if ha != hb {
		t.Errorf("empty description ≡ missing, but hashes differ:\n  a=%s\n  b=%s", ha, hb)
	}
}

func TestToolsHash_EmptyParamsEqualsNil(t *testing.T) {
	a := &Capability{
		Type: "cli", Command: "x",
		Tools: map[string]*CLITool{
			"t": {Description: "d", Args: []string{"a"}, Params: map[string]*CLIParam{}},
		},
	}
	b := &Capability{
		Type: "cli", Command: "x",
		Tools: map[string]*CLITool{
			"t": {Description: "d", Args: []string{"a"}},
		},
	}
	ha, _ := ToolsHash(a)
	hb, _ := ToolsHash(b)
	if ha != hb {
		t.Errorf("empty params ≡ missing, but hashes differ:\n  a=%s\n  b=%s", ha, hb)
	}
}

// -----------------------------------------------------------------------------
// Independence — the two hashes must track independent dimensions.
// -----------------------------------------------------------------------------

func TestHashes_ToolOnlyChangeAffectsOnlyToolsHash(t *testing.T) {
	base := &Capability{
		Type: "cli", Command: "echo",
		Tools: map[string]*CLITool{
			"run": {Description: "old"},
		},
	}
	changed := &Capability{
		Type: "cli", Command: "echo",
		Tools: map[string]*CLITool{
			"run": {Description: "new"},
		},
	}
	fb, _ := Fingerprints(base)
	fc, _ := Fingerprints(changed)

	if fb.ProcessHash != fc.ProcessHash {
		t.Errorf("tool-only change must NOT affect process_hash:\n  before=%s\n  after=%s",
			fb.ProcessHash, fc.ProcessHash)
	}
	if fb.ToolsHash == fc.ToolsHash {
		t.Errorf("tool-only change must affect tools_hash (both=%s)", fb.ToolsHash)
	}
}

func TestHashes_ProcessOnlyChangeAffectsOnlyProcessHash(t *testing.T) {
	base := &Capability{
		Type: "cli", Command: "echo", Args: []string{"a"},
		Tools: map[string]*CLITool{"run": {Description: "d"}},
	}
	changed := &Capability{
		Type: "cli", Command: "echo", Args: []string{"b"},
		Tools: map[string]*CLITool{"run": {Description: "d"}},
	}
	fb, _ := Fingerprints(base)
	fc, _ := Fingerprints(changed)

	if fb.ToolsHash != fc.ToolsHash {
		t.Errorf("process-only change must NOT affect tools_hash:\n  before=%s\n  after=%s",
			fb.ToolsHash, fc.ToolsHash)
	}
	if fb.ProcessHash == fc.ProcessHash {
		t.Errorf("process-only change must affect process_hash (both=%s)", fb.ProcessHash)
	}
}

// -----------------------------------------------------------------------------
// Fixed sentinel — MCP or CLI without declared tools hashes to sha256("null").
// -----------------------------------------------------------------------------

func TestToolsHash_NilToolsFixedSentinel(t *testing.T) {
	cap := &Capability{Type: "mcp", URL: "https://x.example"}
	h, err := ToolsHash(cap)
	if err != nil {
		t.Fatalf("ToolsHash: %v", err)
	}
	if h != sha256NullHex {
		t.Errorf("MCP without tools must hash to sha256(\"null\"):\n  got=%s\n  want=%s", h, sha256NullHex)
	}

	cliNoTools := &Capability{Type: "cli", Command: "echo"}
	h2, _ := ToolsHash(cliNoTools)
	if h2 != sha256NullHex {
		t.Errorf("CLI without tools must also hash to sha256(\"null\"), got %s", h2)
	}
}

// -----------------------------------------------------------------------------
// Metadata exclusion — pure metadata never affects either hash.
// -----------------------------------------------------------------------------

func TestHashes_MetadataDoesNotAffectHashes(t *testing.T) {
	a := &Capability{
		Type: "mcp", URL: "https://x",
	}
	b := &Capability{
		Type:        "mcp",
		URL:         "https://x",
		Description: "some description",
		Tags:        []string{"t1", "t2"},
		Aliases:     []string{"a1"},
		Keywords:    []string{"k1", "k2"},
	}
	fa, _ := Fingerprints(a)
	fb, _ := Fingerprints(b)
	if fa != fb {
		t.Errorf("metadata must not affect hashes:\n  a=%+v\n  b=%+v", fa, fb)
	}
}

// -----------------------------------------------------------------------------
// Golden vectors — cross-implementation determinism.
//
// testdata/hash_vectors/ contains one <name>.yaml (a Capability) per vector
// and a manifest.json mapping vector-name → {process_hash, tools_hash}. Any
// independent implementation (TypeScript, Python, …) should be able to load
// the YAML under our schema, apply the spec in §A.12, and reproduce the same
// hashes byte-for-byte.
// -----------------------------------------------------------------------------

func TestGoldenVectors(t *testing.T) {
	const dir = "testdata/hash_vectors"

	manifestBytes, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]Fingerprint
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	if len(manifest) == 0 {
		t.Fatal("manifest is empty — at minimum the spec requires vectors for "+
			"equivalent-empty, tool-only change, process-only change, and fixed sentinel",
		)
	}

	for name, want := range manifest {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".yaml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read vector %s: %v", name, err)
			}
			var cap Capability
			if err := yaml.Unmarshal(data, &cap); err != nil {
				t.Fatalf("parse vector %s: %v", name, err)
			}
			got, err := Fingerprints(&cap)
			if err != nil {
				t.Fatalf("Fingerprints(%s): %v", name, err)
			}
			if got != want {
				t.Errorf("vector %s fingerprint mismatch:\n  want=%+v\n  got =%+v", name, want, got)
			}
		})
	}
}
