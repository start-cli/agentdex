package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
)

// refreshTargets are the caches refresh can force, in help-display order, each with
// its one-line description and the note printed when it is refreshed. all refreshes
// both. This slice is the single source for target validation, the unknown-target
// error, the Targets help section, and the success note, so they cannot drift.
var refreshTargets = []struct{ name, desc, note string }{
	{"catalog", "Re-resolve the agentdex catalog version", "Refreshed agentdex catalog (agent data)"},
	{"models.dev", "Refetch the models.dev catalog", "Refreshed models.dev catalog (provider and model data)"},
	{"all", "Both (default)", ""},
}

// refreshTargetFor maps an accepted target name to the library Target. The name is
// validated by validRefreshTarget before this is called, so the default is "all".
func refreshTargetFor(name string) agentdex.Target {
	switch name {
	case "catalog":
		return agentdex.TargetCatalog
	case "models.dev":
		return agentdex.TargetModels
	default:
		return agentdex.TargetAll
	}
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

// refreshNote returns the success note for a refreshed target, drawn from
// refreshTargets so the output cannot drift from the accepted targets.
func refreshNote(name string) string {
	for _, t := range refreshTargets {
		if t.name == name {
			return t.note
		}
	}
	return ""
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
		Short:   "Force a refresh: " + refreshTargetList(),
		Long: "Force a refresh of the agentdex catalog (agent data) and/or the " +
			"models.dev catalog (provider and model data). The target defaults to all.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			idx, err := a.index(cmd)
			if err != nil {
				return err
			}
			target := "all"
			if len(args) == 1 {
				target = args[0]
			}
			if !validRefreshTarget(target) {
				return a.usage(cmd, fmt.Errorf("unknown refresh target %q: want %s", target, refreshTargetList()))
			}

			// The library owns the sequencing (catalog then models.dev under all, stop
			// at the first failure) and reports which targets actually refreshed; the
			// CLI maps the target name to the library Target and renders the outcome.
			// A stale catalog fallback and a models.dev outage arrive as typed errors
			// the exit-code table classifies (R13, R15).
			done, err := idx.Refresh(cmd.Context(), refreshTargetFor(target))
			if err != nil {
				return a.fail(cmd, codeFor(err), err)
			}
			var refreshed []string
			if done.Catalog {
				refreshed = append(refreshed, "catalog")
			}
			if done.Models {
				refreshed = append(refreshed, "models.dev")
			}

			data := map[string]any{"refreshed": refreshed}
			return a.ok(cmd, data, nil, func(w io.Writer) {
				for _, r := range refreshed {
					fmt.Fprintln(w, refreshNote(r))
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
