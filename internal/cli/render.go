package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/start-cli/agentdex/internal/tui"
)

// tabulate projects records onto two independent surfaces: the JSON data slice and
// a parallel header/rows pair for the text table. jsonFields is the user's --fields
// selection: empty means each record's full field listing, because JSON is a data
// format and is never silently truncated to the table's columns; a non-empty
// selection narrows the JSON to exactly those keys. tableCols is the column set for
// the text table, always supplied explicitly by the caller (the declared defaults,
// the verbose set, or a widened set). When the user gives --fields the caller passes
// the same selection as tableCols so both surfaces agree. jsonFields is validated up
// front so an unknown --fields key is a usage error whether the result set is empty
// or not.
func tabulate(recs []*record, jsonFields, tableCols []string, set fieldSet) (data []map[string]any, headers []string, rows [][]string, err error) {
	if err := set.validate(jsonFields); err != nil {
		return nil, nil, nil, err
	}
	if len(recs) == 0 {
		return []map[string]any{}, tableCols, nil, nil
	}

	first, err := recs[0].resolve(tableCols)
	if err != nil {
		return nil, nil, nil, err
	}
	headers = make([]string, len(first))
	for i, f := range first {
		headers[i] = f.key
	}

	data = make([]map[string]any, 0, len(recs))
	rows = make([][]string, 0, len(recs))
	for _, r := range recs {
		jf, err := r.resolve(jsonFields)
		if err != nil {
			return nil, nil, nil, err
		}
		data = append(data, jsonObject(jf))

		cells, err := r.resolve(tableCols)
		if err != nil {
			return nil, nil, nil, err
		}
		row := make([]string, len(cells))
		for i, f := range cells {
			row[i] = f.text
		}
		rows = append(rows, row)
	}
	return data, headers, rows, nil
}

// renderTable writes records as an aligned table with uppercased column headers,
// or an empty-state line when there are no rows.
func renderTable(w io.Writer, headers []string, rows [][]string, empty string) {
	if len(rows) == 0 {
		fmt.Fprintln(w, empty)
		return
	}
	t := tui.NewTable(upper(headers)...)
	for _, row := range rows {
		t.Append(row...)
	}
	t.Render(w)
}

// priceUnitNote is the muted footer printed under any surface that showed
// per-model pricing, so the bare "$3" cells are never ambiguous about unit or
// currency.
const priceUnitNote = "Prices in USD per 1M tokens (models.dev)"

// renderPriceFooter writes the pricing-unit footer when the rendered columns
// include a price. It is a text-surface affordance only; JSON carries raw
// numbers under documented keys.
func renderPriceFooter(w io.Writer, cols []string) {
	for _, c := range cols {
		if c == "input" || c == "output" || c == "total" {
			fmt.Fprintln(w, tui.Muted.Sprint(priceUnitNote))
			return
		}
	}
}

// renderFields writes a single record's selected fields for scripting: a lone
// field prints its bare value (so --fields canonical_id is pipe-friendly), several
// print "key: value" lines.
func renderFields(w io.Writer, fs []field) {
	if len(fs) == 1 {
		fmt.Fprintln(w, fs[0].text)
		return
	}
	for _, f := range fs {
		fmt.Fprintf(w, "%s: %s\n", f.key, f.text)
	}
}

// renderDetail writes a single record as aligned "Label: value" lines, the
// default text view for a single model.
func renderDetail(w io.Writer, fs []field) {
	width := 0
	for _, f := range fs {
		if n := len(f.key); n > width {
			width = n
		}
	}
	for _, f := range fs {
		label := tui.Label.Sprint(padRight(f.key, width))
		fmt.Fprintf(w, "%s  %s\n", label, f.text)
	}
}

func upper(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = strings.ToUpper(k)
	}
	return out
}

func padRight(s string, width int) string {
	if pad := width - len(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
