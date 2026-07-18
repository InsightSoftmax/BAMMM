// Package rules implements user-defined translation rules that rewrite the SPLAT
// intermediate representation between parse and emit, so users can adjust a
// conversion without patching BAMMM. See docs/translation-rules.md.
package rules

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

// APIVersion is the expected apiVersion of a rules document.
const APIVersion = "bammm.io/rules/v1alpha1"

// Ruleset is a parsed rules document.
type Ruleset struct {
	APIVersion string `json:"apiVersion"`
	Rules      []Rule `json:"rules"`
}

// Rule is a single match → action entry. Actions apply in the order
// default → set → rename → remove when When matches.
type Rule struct {
	When    When              `json:"when"`
	Default map[string]any    `json:"default,omitempty"` // set only if the path is absent
	Set     map[string]any    `json:"set,omitempty"`     // set (overwrite)
	Rename  map[string]string `json:"rename,omitempty"`  // oldPath -> newPath
	Remove  stringList        `json:"remove,omitempty"`  // paths to delete
	Warn    string            `json:"warn,omitempty"`    // printed when the rule fires
}

// When holds the (all-must-hold) match conditions for a rule.
type When struct {
	From   string         `json:"from,omitempty"`   // source format must equal this
	To     string         `json:"to,omitempty"`     // target format must equal this
	Has    stringList     `json:"has,omitempty"`    // these dotted paths must exist
	Equals map[string]any `json:"equals,omitempty"` // path == value
}

// stringList unmarshals from either a single string or a list of strings.
type stringList []string

func (s *stringList) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*s = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

// Load parses and validates a rules document (YAML or JSON).
func Load(data []byte) (*Ruleset, error) {
	var rs Ruleset
	if err := yaml.Unmarshal(data, &rs); err != nil {
		return nil, fmt.Errorf("rules: parse: %w", err)
	}
	if rs.APIVersion != APIVersion {
		return nil, fmt.Errorf("rules: apiVersion %q, want %q", rs.APIVersion, APIVersion)
	}
	return &rs, nil
}

// Apply rewrites the SPLAT job in place per the rules for a from→to conversion,
// returning any warnings emitted by matched rules (in rule order).
func (rs *Ruleset) Apply(job *splat.Job, from, to string) ([]string, error) {
	if rs == nil || len(rs.Rules) == 0 {
		return nil, nil
	}
	// Work on a generic map so dotted paths are easy to read and write.
	var m map[string]any
	data, err := yaml.Marshal(job)
	if err != nil {
		return nil, fmt.Errorf("rules: encode job: %w", err)
	}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("rules: decode job: %w", err)
	}

	var warnings []string
	for i := range rs.Rules {
		r := &rs.Rules[i]
		if !matches(&r.When, m, from, to) {
			continue
		}
		for _, p := range sortedKeys(r.Default) {
			if _, ok := getPath(m, p); !ok {
				setPath(m, p, r.Default[p])
			}
		}
		for _, p := range sortedKeys(r.Set) {
			setPath(m, p, r.Set[p])
		}
		for _, old := range sortedStrKeys(r.Rename) {
			if v, ok := getPath(m, old); ok {
				setPath(m, r.Rename[old], v)
				deletePath(m, old)
			}
		}
		for _, p := range r.Remove {
			deletePath(m, p)
		}
		if r.Warn != "" {
			warnings = append(warnings, r.Warn)
		}
	}

	// Materialize back into a fresh job so removed paths actually disappear.
	out, err := yaml.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("rules: re-encode: %w", err)
	}
	var rebuilt splat.Job
	if err := yaml.Unmarshal(out, &rebuilt); err != nil {
		return nil, fmt.Errorf("rules: rebuild job: %w", err)
	}
	*job = rebuilt
	return warnings, nil
}

func matches(w *When, m map[string]any, from, to string) bool {
	if w.From != "" && w.From != from {
		return false
	}
	if w.To != "" && w.To != to {
		return false
	}
	for _, p := range w.Has {
		if v, ok := getPath(m, p); !ok || v == nil {
			return false
		}
	}
	for _, p := range sortedKeys(w.Equals) {
		v, ok := getPath(m, p)
		if !ok || !scalarEqual(v, w.Equals[p]) {
			return false
		}
	}
	return true
}

// ── dotted-path helpers over map[string]any ──────────────────────────────────

func getPath(m map[string]any, path string) (any, bool) {
	segs := strings.Split(path, ".")
	var cur any = m
	for _, s := range segs {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = mm[s]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func setPath(m map[string]any, path string, val any) {
	segs := strings.Split(path, ".")
	cur := m
	for _, s := range segs[:len(segs)-1] {
		next, ok := cur[s].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[s] = next
		}
		cur = next
	}
	cur[segs[len(segs)-1]] = val
}

func deletePath(m map[string]any, path string) {
	segs := strings.Split(path, ".")
	cur := m
	for _, s := range segs[:len(segs)-1] {
		next, ok := cur[s].(map[string]any)
		if !ok {
			return
		}
		cur = next
	}
	delete(cur, segs[len(segs)-1])
}

// scalarEqual compares two scalar values tolerantly (YAML/JSON may decode
// numbers as float64), by their string forms.
func scalarEqual(a, b any) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStrKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
