package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/start-cli/agentdex/internal/tui"
)

// envelope is the agentdex JSON output contract: a status, the command's data,
// an error message, and any warnings. It is settled once here and shared by every
// command so the JSON shape never diverges per command.
type envelope struct {
	Status   string   `json:"status"`             // "ok" or "error"
	Data     any      `json:"data,omitempty"`     // command payload on success
	Error    string   `json:"error,omitempty"`    // message on failure
	Warnings []string `json:"warnings,omitempty"` // non-fatal notes
}

// field is one selectable output field: a JSON value and its text rendering. The
// two are kept together so --fields selects the same field for both surfaces.
type field struct {
	key  string
	val  any
	text string
}

// fieldSet is the declared field authority for a record type. all is the full,
// canonically ordered set of valid --fields keys; defaults is the subset shown as
// table columns without an explicit selection. It is the single source of truth
// for validation (independent of how many records exist) and for field ordering,
// so a record never derives validity from the fields it happens to carry.
type fieldSet struct {
	all      []string
	defaults []string
	index    map[string]bool
}

// newFieldSet builds a fieldSet, precomputing the membership index used for
// validation. defaults must be a subset of all.
func newFieldSet(all, defaults []string) fieldSet {
	index := make(map[string]bool, len(all))
	for _, k := range all {
		index[k] = true
	}
	return fieldSet{all: all, defaults: defaults, index: index}
}

// validate reports the first selected field not in the declared set as a usage
// error. An empty selection is always valid; defaults are valid by construction.
func (fs fieldSet) validate(fields []string) error {
	for _, k := range fields {
		if !fs.index[k] {
			return fmt.Errorf("unknown field %q (valid: %s)", k, strings.Join(fs.all, ", "))
		}
	}
	return nil
}

// record is one output row (an agent, a model) as an ordered, selectable field
// set drawn from a declared fieldSet. present holds the fields this instance
// actually carries; a selected key that is valid but absent (an empty canonical
// id, say) resolves to a blank rather than an unknown-field error.
type record struct {
	set     fieldSet
	order   []string
	present map[string]field
}

func newRecord(set fieldSet) *record {
	return &record{set: set, present: map[string]field{}}
}

// add records a present field. The key's validity comes from the declared
// fieldSet, not from being added here.
func (r *record) add(key string, val any, text string) {
	if _, dup := r.present[key]; !dup {
		r.order = append(r.order, key)
	}
	r.present[key] = field{key: key, val: val, text: text}
}

// resolve returns the fields to emit for the requested selection. An empty
// selection means every field this record carries, in declared order — the full
// JSON record and detail view; a caller wanting the narrower table defaults passes
// them explicitly (see tabulate). A non-empty selection is validated against the
// declared set, and an unknown field is a usage error. A selected-but-absent field
// resolves to a blank.
func (r *record) resolve(fields []string) ([]field, error) {
	if err := r.set.validate(fields); err != nil {
		return nil, err
	}
	keys := fields
	if len(keys) == 0 {
		keys = r.order
	}
	out := make([]field, 0, len(keys))
	for _, k := range keys {
		if f, ok := r.present[k]; ok {
			out = append(out, f)
			continue
		}
		out = append(out, field{key: k, val: "", text: ""})
	}
	return out, nil
}

// jsonObject builds the map a record contributes to the JSON envelope's data.
func jsonObject(fields []field) map[string]any {
	m := make(map[string]any, len(fields))
	for _, f := range fields {
		m[f.key] = f.val
	}
	return m
}

// writeJSON renders an envelope as indented JSON with a trailing newline.
func writeJSON(w io.Writer, env envelope) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(env)
}

// emitWarnings writes warnings to stderr for text output, one per line.
func emitWarnings(w io.Writer, warnings []string) {
	for _, msg := range warnings {
		fmt.Fprintln(w, tui.Warn.Sprint("warning:")+" "+msg)
	}
}
