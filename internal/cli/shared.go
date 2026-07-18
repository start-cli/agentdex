package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// staleCatalogWarning is the single warning every command emits when the catalog
// load returns stale, so the wording never drifts between the noun surfaces.
const staleCatalogWarning = "agent catalog is stale: re-resolution failed, using the last resolved version"

// matchesFilter is the shared browse narrowing for every list verb: it reports
// whether id or name contains needle, which the caller has already lower-cased.
// It is deliberately not the none/one/many selector — a list filter is tolerant of
// zero and many matches, so it never resolves an identity.
func matchesFilter(id, name, needle string) bool {
	return strings.Contains(strings.ToLower(id), needle) ||
		strings.Contains(strings.ToLower(name), needle)
}

// flattenProviders normalises --provider values: StringSlice already csv-splits,
// but empty entries from accidental commas are dropped, as are duplicate ids
// (a repeated id would double-list models and break unique query resolution).
func flattenProviders(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// addHelpSection injects a titled, pre-formatted section into the command's help,
// rendered between Flags and Global Flags so it mirrors cobra's own section layout
// rather than being buried in the description. body is emitted verbatim, so the
// caller owns its indentation and line breaks.
func addHelpSection(cmd *cobra.Command, title, body string) {
	section := "\n\n" + title + ":\n" + body
	tmpl := strings.Replace(cmd.UsageTemplate(),
		"{{if .HasAvailableInheritedFlags}}",
		section+"{{if .HasAvailableInheritedFlags}}", 1)
	cmd.SetUsageTemplate(tmpl)
}

// addFieldsHelpSection injects a "Fields" section listing the valid --fields keys.
// The list is drawn from the field set --fields validates against, so the help can
// never drift from what is accepted.
func addFieldsHelpSection(cmd *cobra.Command, set fieldSet) {
	// Split the keys across two indented rows so the section stays compact rather
	// than running to one wide line.
	half := (len(set.all) + 1) / 2
	rows := "  " + strings.Join(set.all[:half], ", ") + "\n  " + strings.Join(set.all[half:], ", ")
	addHelpSection(cmd, "Fields", rows)
}

// registerFieldsFlag adds the shared --fields flag and accepts the singular
// --field as an alias, so a common slip resolves to the same flag instead of
// failing with an unknown-flag usage error. The alias is invisible in help;
// --fields stays the one documented name.
func registerFieldsFlag(cmd *cobra.Command, fields *[]string) {
	cmd.Flags().StringSliceVar(fields, "fields", nil, "Select output fields (csv)")
	cmd.Flags().SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "field" {
			name = "fields"
		}
		return pflag.NormalizedName(name)
	})
}
