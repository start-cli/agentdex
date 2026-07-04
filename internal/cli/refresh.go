package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/modelsdev"
)

// refreshTargets are the caches refresh can force. all refreshes both.
var refreshTargets = map[string]bool{"catalog": true, "models": true, "all": true}

func (a *app) newRefreshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "refresh [target]",
		GroupID: groupCore,
		Short:   "Force a cache refresh: catalog, models, or all",
		Long: "Force a refresh of the cached catalog version and/or the models.dev " +
			"catalog. The target defaults to all.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.requireConfig()
			if err != nil {
				return a.failConfig(cmd, err)
			}
			target := "all"
			if len(args) == 1 {
				target = args[0]
			}
			if !refreshTargets[target] {
				return a.usage(cmd, fmt.Errorf("unknown refresh target %q: want catalog, models, or all", target))
			}

			var refreshed []string
			if target == "catalog" || target == "all" {
				// A zero TTL forces re-resolution past the cached version. A stale
				// result means re-resolution failed and the last resolved version was
				// reused: that is not a successful refresh, so report it transient
				// rather than claiming fresh data was resolved.
				_, stale, err := agentdex.LoadCatalog(cmd.Context(), cfg.CatalogOptions(0)...)
				if err != nil {
					return a.fail(cmd, codeFor(err), err)
				}
				if stale {
					return a.fail(cmd, codeTransient, errors.New("catalog refresh failed: could not re-resolve the latest version, the cached version is unchanged"))
				}
				refreshed = append(refreshed, "catalog")
				a.log.Debug("refreshed catalog cache")
			}
			if target == "models" || target == "all" {
				// A zero TTL treats any cached catalog.json as stale, forcing a fetch.
				// A failed fetch is transient (refresh is inherently a network action),
				// unless models.dev served recognisably malformed data, which is a data
				// fault.
				if _, err := cfg.ForceRefreshModelsClient().Catalog(cmd.Context()); err != nil {
					code := codeTransient
					if errors.Is(err, modelsdev.ErrModelsSchema) {
						code = codeConfig
					}
					return a.fail(cmd, code, err)
				}
				refreshed = append(refreshed, "models")
				a.log.Debug("refreshed models.dev cache")
			}

			data := map[string]any{"refreshed": refreshed}
			return a.ok(cmd, data, nil, func(w io.Writer) {
				for _, r := range refreshed {
					fmt.Fprintf(w, "refreshed %s cache\n", r)
				}
			})
		},
	}
	return cmd
}
