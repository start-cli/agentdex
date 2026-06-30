package cli

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/internal/config"
	"github.com/start-cli/agentdex/modelsdev"
)

// Exit codes are agentdex's taxonomy, shared with the wider start CLI. They are
// the single source of exit-code meaning; commands classify their failures into
// one of these rather than inventing per-command codes.
const (
	codeOK         = 0
	codeFailure    = 1
	codeUsage      = 2
	codeNotFound   = 3
	codePermission = 4
	codeConflict   = 5
	codeTransient  = 75
	codeConfig     = 78
)

// exitError carries the process exit code out of a command. Its message is
// already rendered to the user (as text or in the JSON envelope) by the time it
// is returned, so Execute and main read only the code and print nothing further.
type exitError struct {
	code int
}

func (e *exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// ExitCode reports the code, satisfying the convention other tooling looks for.
func (e *exitError) ExitCode() int { return e.code }

// codeForConfig classifies a config-load failure: a validity fault (malformed
// config.cue, bad value or duration) is config (78), a permission denial is
// permission (4), and any other read failure is a generic failure (1). config.Load
// preserves the underlying OS error, so a permission denial resolves through
// errors.Is without the config package naming the exit taxonomy.
func codeForConfig(err error) int {
	switch {
	case errors.Is(err, config.ErrConfig):
		return codeConfig
	case errors.Is(err, fs.ErrPermission):
		return codePermission
	default:
		return codeFailure
	}
}

// codeFor maps a library error to the exit code that best describes it, for the
// failures a command does not classify by hand. The coverage rollup in get
// chooses its codes directly; this covers catalog-load and enrichment faults.
func codeFor(err error) int {
	switch {
	case errors.Is(err, config.ErrConfig):
		return codeConfig
	case errors.Is(err, modelsdev.ErrModelsSchema):
		// A reachable models.dev serving a malformed model is a data fault, not a
		// transient outage: report it as config, never as transient.
		return codeConfig
	case errors.Is(err, agentdex.ErrCatalogUnavailable):
		return codeTransient
	case errors.Is(err, agentdex.ErrAgentUnknown):
		return codeNotFound
	default:
		return codeFailure
	}
}
