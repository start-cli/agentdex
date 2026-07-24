package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// emptyListMessage is the empty-state line for a browse listing. A filter that
// matched nothing names the filter, so the user sees their narrowing was the cause
// rather than an empty catalog; with no filter the genuine-empty fallback is used.
func emptyListMessage(filter, noun, fallback string) string {
	if filter != "" {
		return fmt.Sprintf("No %s match %q.", noun, filter)
	}
	return fallback
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
