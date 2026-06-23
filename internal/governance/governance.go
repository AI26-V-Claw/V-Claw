// Package governance provides helpers to compute the governance metadata that
// V-Claw attaches to every run, tool call, approval, risk decision, and audit
// entry. The fields are described in docs/03-contracts.md (GovernanceMetadata)
// and consumed by N4 monitoring/trace UIs.
//
// All helpers are pure functions: same input → same output. They never read the
// clock, the filesystem, or any I/O — callers pass any time/value in.
package governance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// versionHashLen is the prefix length kept from a sha256 hex digest.
// Eight hex characters give 32 bits of entropy — enough to spot drift in audit
// while staying readable in logs/dashboards.
const versionHashLen = 8

// Source attribution constants. Tool layers stamp ToolResult.Source with one of
// these prefixes so audit/N4 can group records by origin without parsing names.
const (
	// SourceToolPrefix is used by agent-callable tools that wrap a connector
	// or a local action (e.g. "tool:gmail", "tool:sandbox.python").
	SourceToolPrefix = "tool:"
	// SourceConnectorPrefix is used by raw external clients invoked outside the
	// tool layer (e.g. "connector:tavily").
	SourceConnectorPrefix = "connector:"
	// SourceUserChannel marks records that originate from a user-facing channel
	// (telegram/cli) rather than a tool execution.
	SourceUserChannel = "channel"
)

// PromptVersion returns a stable short fingerprint of the system-prompt
// material. The caller passes every piece of text that contributes to the
// effective system prompt — typically runtimeSystemPrompt() output and any
// loaded SOUL.md content. The prompt content is the source of truth: when it
// changes, the version changes automatically without anyone having to bump a
// constant.
//
// Empty parts are skipped so callers can pass optional fragments unchecked.
// Whitespace inside a part is preserved to keep the digest sensitive to layout
// changes that could affect model behaviour.
func PromptVersion(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		if p == "" {
			continue
		}
		// Length-prefix each part so concatenation can't accidentally collide.
		fmt.Fprintf(h, "%d\x00", len(p))
		h.Write([]byte(p))
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)[:versionHashLen]
}

// ToolSchemaVersion returns a stable short fingerprint of a tool's exposed
// parameters schema. We canonicalise the JSON (sorted keys) so cosmetic
// reordering or re-serialisation doesn't shift the version when the tool's
// surface is unchanged.
//
// The input is typically tools.ToolDefinition.Parameters. We accept any value
// to keep this package independent of internal/tools.
func ToolSchemaVersion(parameters any) string {
	if parameters == nil {
		return ""
	}
	canonical, err := canonicalJSON(parameters)
	if err != nil || len(canonical) == 0 {
		return ""
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])[:versionHashLen]
}

// PolicyRef returns the composite reference string V-Claw uses to point from
// any record (tool call, action, audit entry) back to the risk decision that
// authorised it. The format is intentionally readable so it can be grep'd in
// JSONL audit files without joining tables:
//
//	policy:<runID>:<toolCallID>:<unixSeconds>
//
// Empty inputs are tolerated — the caller will get an empty string back so the
// audit row stores nothing rather than a malformed reference.
func PolicyRef(runID, toolCallID string, checkedAt time.Time) string {
	runID = strings.TrimSpace(runID)
	toolCallID = strings.TrimSpace(toolCallID)
	if runID == "" || toolCallID == "" {
		return ""
	}
	ts := checkedAt
	if ts.IsZero() {
		return ""
	}
	return fmt.Sprintf("policy:%s:%s:%d", runID, toolCallID, ts.UTC().Unix())
}

// canonicalJSON marshals value with deterministic key ordering. encoding/json
// already sorts map keys alphabetically when marshalling map[string]any, but
// it does NOT sort keys produced by struct serialisation in nested objects.
// We round-trip the value through map[string]any to force consistent ordering
// regardless of how the caller assembled the input.
func canonicalJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	return marshalSorted(generic)
}

// marshalSorted writes a JSON value with sorted keys at every level.
// We can't rely on encoding/json alone for nested ordering after Unmarshal —
// it preserves order for arrays but key order in maps is decided at marshal
// time, which is alphabetic for map[string]any. So a single Marshal(generic)
// is in fact deterministic; we still walk the tree to keep the contract
// explicit and survive future stdlib changes.
func marshalSorted(value any) ([]byte, error) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			keyJSON, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			b.Write(keyJSON)
			b.WriteByte(':')
			child, err := marshalSorted(v[k])
			if err != nil {
				return nil, err
			}
			b.Write(child)
		}
		b.WriteByte('}')
		return []byte(b.String()), nil
	case []any:
		var b strings.Builder
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			child, err := marshalSorted(item)
			if err != nil {
				return nil, err
			}
			b.Write(child)
		}
		b.WriteByte(']')
		return []byte(b.String()), nil
	default:
		return json.Marshal(v)
	}
}
