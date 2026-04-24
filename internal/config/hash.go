package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// ProcessHash computes the canonical fingerprint of a capability's process-level
// configuration — the dimension that answers "do we need to restart the process?".
//
// The exact schema, normalization, and serialization rules are specified in the
// v0.2 design doc §A.12 ("process_hash 规范化"). In short:
//
//   - All of {type, transport, command, args, url, env, required_env} are kept
//     as top-level keys; "meaningless empty" values ([], {}, missing) collapse
//     to JSON null so that aesthetically-different but runtime-equivalent
//     configurations produce the same hash.
//   - env keys are sorted lexicographically (UTF-8 code points); required_env
//     entries are sorted and de-duplicated; args preserve declared order.
//   - Canonical JSON uses Go's encoding/json which sorts map keys by Unicode
//     code point, matches RFC 8785 for the ASCII-only key space we use, and
//     emits no superfluous whitespace.
//   - Output is sha256(canonical_json_bytes) as lowercase hex.
//
// Pure metadata (description, aliases, keywords, tags, disabled) and the tools
// map are intentionally excluded — the latter belongs to ToolsHash.
func ProcessHash(cap *Capability) (string, error) {
	obj := map[string]any{
		"type":         nullIfEmpty(cap.Type),
		"transport":    nullIfEmpty(cap.Transport),
		"command":      nullIfEmpty(cap.Command),
		"args":         canonicalArgs(cap.Args),
		"url":          nullIfEmpty(cap.URL),
		"env":          canonicalEnv(cap.Env),
		"required_env": canonicalRequiredEnv(cap.RequiredEnv),
	}
	return canonicalHash(obj)
}

// ToolsHash computes the canonical fingerprint of a capability's tool surface
// — the dimension that answers "must we re-register MCP tool schemas / invalidate
// downstream tool caches?". Specified in design §A.12 ("tools_hash 规范化").
//
// For type == "mcp" (or any capability with no declared tools), the tool surface
// is not described in capx YAML and is fingerprinted as the fixed value
// sha256("null") — this keeps the field present and comparable without having
// to fetch the upstream MCP server's tools/list at hash time.
func ToolsHash(cap *Capability) (string, error) {
	if len(cap.Tools) == 0 {
		return canonicalHash(nil)
	}
	obj := make(map[string]any, len(cap.Tools))
	for name, tool := range cap.Tools {
		obj[name] = canonicalTool(tool)
	}
	return canonicalHash(obj)
}

// Fingerprint bundles both hashes for a capability. It's the unit that
// set_scene diff consumes (§A.12 "Diff 算法").
type Fingerprint struct {
	ProcessHash string `json:"process_hash"`
	ToolsHash   string `json:"tools_hash"`
}

// Fingerprints returns both hashes for a capability.
func Fingerprints(cap *Capability) (Fingerprint, error) {
	ph, err := ProcessHash(cap)
	if err != nil {
		return Fingerprint{}, err
	}
	th, err := ToolsHash(cap)
	if err != nil {
		return Fingerprint{}, err
	}
	return Fingerprint{ProcessHash: ph, ToolsHash: th}, nil
}

// ---- canonicalization helpers ----

// nullIfEmpty returns nil (→ JSON null) for "" and the original string otherwise.
// type / transport / command / url all collapse "" to null per the spec.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// canonicalArgs preserves declared order; empty or nil → JSON null.
func canonicalArgs(args []string) any {
	if len(args) == 0 {
		return nil
	}
	// Copy to insulate callers from canonicalization side-effects.
	out := make([]string, len(args))
	copy(out, args)
	return out
}

// canonicalEnv sorts keys lexicographically (Go's json.Marshal already sorts
// map[string]* keys, but we build an explicit ordered map for clarity and to
// guarantee nil on empty input). Empty env → JSON null.
func canonicalEnv(env map[string]string) any {
	if len(env) == 0 {
		return nil
	}
	// json.Marshal on map[string]string sorts keys by Unicode code point,
	// which matches our "UTF-8 字典序" requirement for ASCII keys.
	return env
}

// canonicalRequiredEnv sorts and de-duplicates; empty → JSON null.
func canonicalRequiredEnv(rs []string) any {
	if len(rs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(rs))
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// canonicalTool builds the per-tool canonical object for tools_hash.
func canonicalTool(tool *CLITool) map[string]any {
	desc := any(tool.Description)
	if tool.Description == "" {
		desc = nil
	}
	obj := map[string]any{
		"description": desc,
		"args":        canonicalArgs(tool.Args),
		"params":      canonicalParams(tool.Params),
	}
	return obj
}

// canonicalParams builds the per-param canonical object; sorted by param name.
func canonicalParams(params map[string]*CLIParam) any {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]any, len(params))
	for name, p := range params {
		desc := any(p.Description)
		if p.Description == "" {
			desc = nil
		}
		var enumField any
		if len(p.Enum) > 0 {
			enumCopy := make([]string, len(p.Enum))
			copy(enumCopy, p.Enum)
			enumField = enumCopy
		}
		out[name] = map[string]any{
			"type":        p.Type,
			"required":    p.Required,
			"description": desc,
			"enum":        enumField,
		}
	}
	return out
}

// canonicalHash serializes obj to canonical JSON and returns sha256 hex.
//
// Go's encoding/json already produces a form close enough to RFC 8785 for our
// schema — string-keyed maps are serialized in Unicode code-point order, output
// is UTF-8, and there is no non-significant whitespace. We never encode
// floating-point numbers in capability configs, so the number-formatting
// divergence between encoding/json and JCS is irrelevant here. If in the future
// we add numeric fields we must revisit this.
func canonicalHash(obj any) (string, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("canonicalize: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
