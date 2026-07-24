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

// codeFor maps a library error to the exit code that describes it (R15). The
// library never chooses an exit code; it returns typed sentinels, and this is the
// single place that classifies them. A reachable models.dev serving malformed data
// is a data fault (config), never a transient outage; a catalog or models.dev
// outage is transient; an exact-get miss is not-found; a caller/usage fault is
// usage.
func codeFor(err error) int {
	switch {
	case errors.Is(err, config.ErrConfig),
		errors.Is(err, agentdex.ErrCatalogInvalid),
		errors.Is(err, modelsdev.ErrModelsSchema):
		return codeConfig
	case errors.Is(err, agentdex.ErrCatalogUnavailable),
		errors.Is(err, agentdex.ErrModelsUnavailable):
		return codeTransient
	case errors.Is(err, agentdex.ErrAgentUnknown),
		errors.Is(err, agentdex.ErrNotFound):
		return codeNotFound
	case errors.Is(err, agentdex.ErrUnknownProvider),
		errors.Is(err, agentdex.ErrProvidersRequired),
		errors.Is(err, agentdex.ErrProvidersNotAllowed),
		errors.Is(err, agentdex.ErrMalformedModelID):
		return codeUsage
	default:
		return codeFailure
	}
}
