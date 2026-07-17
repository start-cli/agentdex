package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex/modelsdev"
)

func (a *app) newProvidersCmd() *cobra.Command {
	var fields []string
	cmd := &cobra.Command{
		Use:     "providers [filter]",
		GroupID: groupCore,
		Short:   "List the models.dev providers agentdex can enrich against",
		Long: "List the models.dev providers usable with --provider on list, get, and models, " +
			"with each provider's id, display name, API-key environment variables and whether they " +
			"are set, and its model count. The optional filter narrows the list to providers whose id " +
			"or name contains it (case-insensitive); it is a browse narrowing, not a selector, so a " +
			"filter matching nothing prints an empty listing and exits 0.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.requireConfig()
			if err != nil {
				return a.failConfig(cmd, err)
			}
			filter := ""
			if len(args) == 1 {
				filter = args[0]
			}
			return a.providersList(cmd, cfg.ModelsClient(), filter, fields)
		},
	}
	registerFieldsFlag(cmd, &fields)
	addFieldsHelpSection(cmd, providerFieldSet)
	return cmd
}

// providersList fetches the merged models.dev catalog, narrows it by the filter,
// and reports the providers sorted by id. It reads env-var presence at the boundary
// through os.LookupEnv and loads no agent catalog: a provider listing is a pure
// models.dev surface.
func (a *app) providersList(cmd *cobra.Command, client *modelsdev.Client, filter string, fields []string) error {
	cat, err := client.Catalog(cmd.Context())
	if err != nil {
		return a.fail(cmd, providersCode(err), err)
	}

	needle := strings.ToLower(filter)
	var recs []*record
	for _, id := range sortedKeys(cat.Providers) {
		p := cat.Providers[id]
		if needle != "" && !providerMatches(p, needle) {
			continue
		}
		recs = append(recs, providerRecord(p, envPresence(p.Env, os.LookupEnv)))
	}

	tableCols := fields
	if len(tableCols) == 0 {
		tableCols = providerFieldSet.defaults
	}
	data, headers, rows, err := tabulate(recs, fields, tableCols, providerFieldSet)
	if err != nil {
		return a.usage(cmd, err)
	}
	return a.ok(cmd, data, nil, func(w io.Writer) {
		fmt.Fprintln(w)
		renderTable(w, headers, rows, "No providers.")
	})
}

// providerMatches reports whether the provider's id or display name contains needle,
// which the caller has already lower-cased.
func providerMatches(p modelsdev.Provider, needle string) bool {
	return strings.Contains(strings.ToLower(p.ID), needle) ||
		strings.Contains(strings.ToLower(p.Name), needle)
}

// envPresence reads whether each of a provider's API-key variables is set, through
// an injected lookup (os.LookupEnv in production) so record building is testable
// from inputs without t.Setenv. Only presence is read, never the value.
func envPresence(env []string, lookup func(string) (string, bool)) map[string]bool {
	present := make(map[string]bool, len(env))
	for _, name := range env {
		_, ok := lookup(name)
		present[name] = ok
	}
	return present
}

// providersCode classifies a providers-command failure. A no-result models.dev
// outage is transient; recognisable gross schema drift is a data fault, config,
// never transient. This mirrors the tail of modelsCode without the selector-match
// codes, which this browse surface cannot raise.
func providersCode(err error) int {
	if errors.Is(err, modelsdev.ErrModelsSchema) {
		return codeConfig
	}
	return codeTransient
}
