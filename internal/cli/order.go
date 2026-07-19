package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex/modelsdev"
)

// ordered returns a copy of the field set with its default sort key and the set of
// naturally descending keys declared. It is the single place a command states how
// its rows are ordered when --order-by is absent.
func (fs fieldSet) ordered(defaultKey string, descend ...string) fieldSet {
	fs.defaultKey = defaultKey
	fs.descend = make(map[string]bool, len(descend))
	for _, k := range descend {
		fs.descend[k] = true
	}
	return fs
}

// value returns the typed JSON value the record carries for key, or nil when the
// field is absent. It is the ordering surface: sorting compares typed values, not
// the formatted text, so numbers and dates order correctly.
func (r *record) value(key string) any {
	if f, ok := r.present[key]; ok {
		return f.val
	}
	return nil
}

// registerOrderFlags adds the shared --order-by and --reverse flags. Valid keys are
// the command's field set (the same set --fields validates against); an unknown key
// is a usage error surfaced at apply time, so the help never drifts from what is
// accepted.
func registerOrderFlags(cmd *cobra.Command, orderBy *string, reverse *bool) {
	cmd.Flags().StringVar(orderBy, "order-by", "", "Sort rows by this field")
	cmd.Flags().BoolVar(reverse, "reverse", false, "Reverse the sort direction")
}

// applyOrder stable-sorts recs by the effective sort key and reports that key so the
// caller can pull its column leftmost. The key is --order-by when given, else the
// field set's default key; --reverse flips whichever direction is in effect (a key's
// natural direction is ascending unless the field set marks it descending). An
// unknown --order-by key is a usage error.
func applyOrder(recs []*record, set fieldSet, orderBy string, reverse bool) (string, error) {
	key := orderBy
	if key == "" {
		key = set.defaultKey
	}
	if !set.index[key] {
		return "", fmt.Errorf("unknown field %q (valid: %s)", key, strings.Join(set.all, ", "))
	}
	// XOR: --reverse flips the field's natural direction.
	descending := set.descend[key] != reverse
	orderRecords(recs, key, descending)
	return key, nil
}

// orderRecords stable-sorts recs by key in the given direction, breaking ties by id
// so the order is deterministic. Records whose value for key is missing sink to the
// end regardless of direction, matching how undated models and unknown prices trail.
func orderRecords(recs []*record, key string, descending bool) {
	sort.SliceStable(recs, func(i, j int) bool {
		if less, tie := lessByKey(recs[i], recs[j], key, descending); !tie {
			return less
		}
		return recordLess(recs[i], recs[j])
	})
}

const (
	orderMissing = iota // absent or empty value: always sorts last
	orderNum
	orderStr
)

// lessByKey reports whether record a orders before b on key, and whether the two are
// tied on that key (so the caller can break the tie deterministically). Missing
// values always order last regardless of direction; present values compare by their
// typed value, inverted when descending.
func lessByKey(a, b *record, key string, descending bool) (less, tie bool) {
	ak, an, as := orderKey(a.value(key))
	bk, bn, bs := orderKey(b.value(key))
	if ak == orderMissing || bk == orderMissing {
		if ak == bk {
			return false, true
		}
		return bk == orderMissing, false // the non-missing record sorts first
	}
	switch {
	case ak != bk:
		less = ak == orderNum // numbers before strings; mixed kinds per column should not occur
	case ak == orderNum:
		if an == bn {
			return false, true
		}
		less = an < bn
	default:
		if as == bs {
			return false, true
		}
		less = as < bs
	}
	if descending {
		less = !less
	}
	return less, false
}

// orderKey reduces a field value to a comparable key: a numeric kind, a string kind,
// or missing. A nil value or an empty string is missing and sorts last, mirroring the
// undated-model and unknown-price rules. Slice and map values order by length, so
// --order-by models ranks agents by model count.
func orderKey(v any) (kind int, num float64, str string) {
	switch t := v.(type) {
	case nil:
		return orderMissing, 0, ""
	case string:
		if t == "" {
			return orderMissing, 0, ""
		}
		return orderStr, 0, t
	case bool:
		if t {
			return orderNum, 1, ""
		}
		return orderNum, 0, ""
	case int:
		return orderNum, float64(t), ""
	case int64:
		return orderNum, float64(t), ""
	case float64:
		return orderNum, t, ""
	case []string:
		return orderNum, float64(len(t)), ""
	case []modelsdev.Model:
		return orderNum, float64(len(t)), ""
	case map[string]bool:
		return orderNum, float64(len(t)), ""
	default:
		return orderMissing, 0, ""
	}
}

// recordLess orders two records by their id for a stable tiebreak. Every list record
// carries an id (the agent, model, or provider id), so this is always defined.
func recordLess(a, b *record) bool {
	return recordID(a) < recordID(b)
}

func recordID(r *record) string {
	if id, ok := r.value("id").(string); ok {
		return id
	}
	return ""
}

// orderColumns pulls the sort column to the leftmost position of the table columns so
// the ordering is legible: it is prepended when absent and moved to the front when
// already present. It applies only to the default and verbose column sets; an
// explicit --fields selection is authoritative and never passed here.
func orderColumns(cols []string, sortKey string) []string {
	out := make([]string, 0, len(cols)+1)
	out = append(out, sortKey)
	for _, c := range cols {
		if c != sortKey {
			out = append(out, c)
		}
	}
	return out
}
