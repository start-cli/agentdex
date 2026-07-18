// Package cli is the agentdex command-line interface: a thin wrapper over the
// agentdex library and the modelsdev client. It owns the cobra command tree, the
// JSON envelope, the exit-code taxonomy, --fields selection, and the
// catalog/models.dev coverage rollup that drives get reporting. It reimplements no
// library behaviour; detection, resolution, the merge, and caching all live in the
// library, and the one piece of CLI-only policy — the coverage rollup — composes
// public library facts rather than reaching past the public API.
package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex/internal/config"
	"github.com/start-cli/agentdex/internal/tui"
)

// groupCore is the help group carrying every real command, so only cobra's
// built-in help and completion fall under Additional Commands.
const groupCore = "core"

// app holds the resolved global flag values and the loaded configuration for one
// invocation. Subcommands close over it for the shared output, config, and logger.
type app struct {
	jsonOut    bool
	verbose    bool
	quiet      bool
	color      string
	debug      bool
	searchDirs []string
	binPaths   []string

	cfg    *config.Config
	cfgErr error
	log    *slog.Logger
}

// NewRootCommand builds the agentdex command tree with global flags bound. It is
// the single construction point so tests can drive the CLI with a fresh tree and
// captured output.
func NewRootCommand() *cobra.Command {
	a := &app{}
	root := &cobra.Command{
		Use:   "agentdex",
		Short: "Detect AI coding agents installed on this machine",
		Long: "agentdex detects AI coding agents installed on the local machine and " +
			"reports their binary, version, config and skills directories, providers, " +
			"and models available from models.dev.",
		SilenceUsage:      true,
		SilenceErrors:     true,
		PersistentPreRunE: a.preRun,
	}

	f := root.PersistentFlags()
	f.BoolVar(&a.jsonOut, "json", false, "Emit a JSON envelope on stdout")
	f.BoolVar(&a.verbose, "verbose", false, "Add detail to output")
	f.BoolVar(&a.quiet, "quiet", false, "Suppress non-essential output")
	f.StringVar(&a.color, "color", "auto", "Colour output: auto, always, never")
	f.BoolVar(&a.debug, "debug", false, "Diagnostic logging to stderr")
	// StringArray, not StringSlice: a directory path can legally contain a comma,
	// so values are taken literally rather than csv-split, matching --bin-path.
	f.StringArrayVar(&a.searchDirs, "search-dir", nil, "Extra binary search locations (repeatable)")
	f.StringArrayVar(&a.binPaths, "bin-path", nil, "Override an agent's binary path as id=path (repeatable)")

	// A single named group keeps the real commands together in help, leaving
	// cobra's help and completion under Additional Commands.
	root.AddGroup(&cobra.Group{ID: groupCore, Title: "Core Commands:"})
	root.AddCommand(
		a.newAgentsCmd(),
		a.newProvidersCmd(),
		a.newModelsCmd(),
		a.newRefreshCmd(),
		a.newVersionCmd(),
	)
	return root
}

// newNounCmd builds a data-entity noun group carrying the two shared verbs, list
// and get. The group itself is not a runnable operation: a bare invocation, or an
// unrecognised verb, routes to the shared usage fault via nounUsage. The singular
// alias is a pure synonym for the plural and selects the same group.
func (a *app) newNounCmd(use, alias, short string, subs ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:     use,
		Aliases: []string{alias},
		GroupID: groupCore,
		Short:   short,
		// ArbitraryArgs routes a bare invocation or an unknown verb to RunE (the
		// shared usage fault) rather than cobra's terse error, so the JSON envelope
		// and exit code stay under our control.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.nounUsage(cmd, args)
		},
	}
	cmd.AddCommand(subs...)
	return cmd
}

// nounUsage is the shared usage fault for a noun group reached without a valid
// verb. In text mode it prints the group help to stdout so the verbs are
// discoverable; under --json it prints no help, so the error envelope sits alone on
// stdout and stays parseable. Either way it exits 2 through the usage path.
func (a *app) nounUsage(cmd *cobra.Command, args []string) error {
	if !a.jsonOut {
		_ = cmd.Help()
	}
	if len(args) > 0 {
		return a.usage(cmd, fmt.Errorf("unknown %s subcommand %q: use list or get", cmd.Name(), args[0]))
	}
	return a.usage(cmd, fmt.Errorf("%s requires a subcommand: list or get", cmd.Name()))
}

// Execute runs the command tree and returns the process exit code. A command's
// own failures arrive as *exitError carrying their classified code; a
// cobra-originated error (an unknown command, a bad flag, a wrong argument count)
// is a usage fault, printed here and reported as exit 2.
func Execute() int {
	root := NewRootCommand()
	err := root.Execute()
	if err == nil {
		return codeOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	fmt.Fprintln(os.Stderr, "error: "+err.Error())
	return codeUsage
}

// preRun loads configuration and sets up colour and logging before any command
// runs. A malformed config is not fatal here — version and completion must still
// work — so it is stashed and surfaced only by commands that need config. Colour
// and an invalid --color value are settled here because they apply to every
// command, including those that ignore config.
func (a *app) preRun(cmd *cobra.Command, _ []string) error {
	switch a.color {
	case "auto", "always", "never":
	default:
		// Route through the shared usage path so an invalid --color carries the JSON
		// envelope under --json like every other usage fault, rather than falling back
		// to plain stderr.
		return a.usage(cmd, fmt.Errorf("invalid --color %q: want auto, always, or never", a.color))
	}

	a.cfg, a.cfgErr = config.Load(config.Path())

	mode := "auto"
	if a.cfg != nil {
		mode = a.cfg.Color
	}
	if cmd.Flags().Changed("color") {
		mode = a.color
	}
	tui.Configure(mode, os.Stdout)

	level := slog.LevelWarn
	if a.debug {
		level = slog.LevelDebug
	}
	a.log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	return nil
}

// requireConfig returns the loaded config or the stashed load error, so a command
// that needs config surfaces a config-load failure as its own classified failure.
func (a *app) requireConfig() (*config.Config, error) {
	if a.cfgErr != nil {
		return nil, a.cfgErr
	}
	return a.cfg, nil
}

// failConfig reports a config-load failure with the exit code its cause warrants:
// a validity fault is config (78), a permission error is permission (4), any other
// read failure is a generic failure (1).
func (a *app) failConfig(cmd *cobra.Command, err error) error {
	return a.fail(cmd, codeForConfig(err), err)
}

// mapFlags parses the global flag values that feed option mapping. A malformed
// --bin-path entry is a usage fault.
func (a *app) mapFlags() (config.Flags, error) {
	bin := make(map[string]string, len(a.binPaths))
	for _, entry := range a.binPaths {
		id, path, ok := strings.Cut(entry, "=")
		if !ok || id == "" || path == "" {
			return config.Flags{}, fmt.Errorf("invalid --bin-path %q: want id=path", entry)
		}
		bin[id] = path
	}
	return config.Flags{SearchDirs: a.searchDirs, BinPaths: bin}, nil
}

// ok renders a successful result: the JSON envelope under --json, otherwise any
// warnings to stderr followed by the command's text. Warnings are suppressed by
// --quiet in text mode.
func (a *app) ok(cmd *cobra.Command, data any, warnings []string, text func(io.Writer)) error {
	if a.jsonOut {
		writeJSON(cmd.OutOrStdout(), envelope{Status: "ok", Data: data, Warnings: warnings})
		return nil
	}
	if !a.quiet {
		emitWarnings(cmd.ErrOrStderr(), warnings)
	}
	if text != nil {
		text(cmd.OutOrStdout())
	}
	return nil
}

// fail renders a failure and returns the exit code. Under --json the error and any
// warnings go into the envelope on stdout; otherwise warnings then the error go to
// stderr. The returned *exitError carries only the code, since the message is
// already rendered.
func (a *app) fail(cmd *cobra.Command, code int, err error, warnings ...string) error {
	if a.jsonOut {
		writeJSON(cmd.OutOrStdout(), envelope{Status: "error", Error: err.Error(), Warnings: warnings})
	} else {
		if !a.quiet {
			emitWarnings(cmd.ErrOrStderr(), warnings)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "error: "+err.Error())
	}
	return &exitError{code: code}
}

// failData is fail with a payload: a failure that still carries data, for the
// rows that report an agent (or a provider) and then exit non-zero. Under --json
// both the data and the error sit in the envelope; in text mode the optional text
// renders to stdout before the error goes to stderr.
func (a *app) failData(cmd *cobra.Command, code int, err error, data any, text func(io.Writer), warnings []string) error {
	if a.jsonOut {
		writeJSON(cmd.OutOrStdout(), envelope{Status: "error", Data: data, Error: err.Error(), Warnings: warnings})
		return &exitError{code: code}
	}
	if !a.quiet {
		emitWarnings(cmd.ErrOrStderr(), warnings)
	}
	if text != nil {
		text(cmd.OutOrStdout())
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "error: "+err.Error())
	return &exitError{code: code}
}

// usage reports an argument or flag fault as exit 2 through the shared failure
// path, so usage errors carry the envelope shape under --json like any other.
func (a *app) usage(cmd *cobra.Command, err error) error {
	return a.fail(cmd, codeUsage, err)
}
