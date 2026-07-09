package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/modelsdev"
)

// refreshTargets are the caches refresh can force, in help-display order, each with
// its one-line description. all refreshes both. This slice is the single source for
// target validation, the unknown-target error, and the Targets help section, so the
// three cannot drift.
var refreshTargets = []struct{ name, desc string }{
	{"catalog", "Re-resolve the agent catalog version"},
	{"models", "Refetch the models.dev catalog"},
	{"all", "Both (default)"},
}

// validRefreshTarget reports whether name is an accepted refresh target.
func validRefreshTarget(name string) bool {
	for _, t := range refreshTargets {
		if t.name == name {
			return true
		}
	}
	return false
}

// refreshTargetList renders the accepted targets as an Oxford-style list ("a, b,
// or c") for the unknown-target error, so the message reads naturally and stays in
// step with the command's Short help while still deriving from refreshTargets.
func refreshTargetList() string {
	names := make([]string, len(refreshTargets))
	for i, t := range refreshTargets {
		names[i] = t.name
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " or " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
	}
}

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
			if !validRefreshTarget(target) {
				return a.usage(cmd, fmt.Errorf("unknown refresh target %q: want %s", target, refreshTargetList()))
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
					fmt.Fprintf(w, "Refreshed %s cache\n", r)
				}
			})
		},
	}
	// Mirror get/models' Fields section: list the accepted [target] values as their
	// own help section, derived from refreshTargets so it cannot drift from what the
	// command accepts.
	width := 0
	for _, t := range refreshTargets {
		if len(t.name) > width {
			width = len(t.name)
		}
	}
	var body strings.Builder
	for _, t := range refreshTargets {
		fmt.Fprintf(&body, "  %-*s  %s\n", width, t.name, t.desc)
	}
	addHelpSection(cmd, "Targets", strings.TrimRight(body.String(), "\n"))
	return cmd
}
