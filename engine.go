package agentdex

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
)

// pathEnv is the boundary-captured environment a detection run resolves catalog
// paths against: the home directory for tilde expansion, the working directory
// for local-scope paths, and a snapshot of the process environment for $VAR
// expansion. Capturing all three once per run keeps the per-agent path
// resolution a pure function of its inputs.
type pathEnv struct {
	home string
	wd   string
	env  map[string]string
}

func newEnv() pathEnv {
	return pathEnv{home: homeDir(), wd: workingDir(), env: environSnapshot()}
}

// homeDir resolves the home directory from the published HOME variable, falling
// back to os.UserHomeDir. Platforms are Linux, macOS, and WSL (Linux-native), so
// HOME is authoritative; no platform-specific user-dir helper is used.
func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

func workingDir() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// maxConcurrentDetections bounds how many catalog entries are processed at once.
// Each detection may exec a binary for its version probe, so without a cap a
// large catalog would fan out to one child process per entry. The work is
// I/O-bound (subprocess and network waits), so a fixed cap above GOMAXPROCS keeps
// healthy parallelism while keeping process count bounded as the catalog grows.
const maxConcurrentDetections = 16

// detectAll runs every non-disabled catalog entry through the engine
// concurrently, omitting agents whose binary was not found (unless the config
// includes missing agents), and returns the rest sorted by id. The first
// non-degradable error (a models.dev schema fault) cancels the remaining work
// and is returned. Concurrency is bounded by maxConcurrentDetections.
func detectAll(ctx context.Context, cat *Catalog, cfg *config) ([]Agent, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	env := newEnv()
	sem := make(chan struct{}, maxConcurrentDetections)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		agents   []Agent
		firstErr error
	)

	for id, ka := range cat.Agents {
		if _, off := cfg.disabled[id]; off {
			continue
		}
		wg.Add(1)
		go func(id string, ka KnownAgent) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			a, err := detectAgent(ctx, id, ka, cfg, env, detectMode{omitIfMissing: !cfg.includeMissing})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				if firstErr == nil {
					firstErr = err
					cancel()
				}
			case a != nil:
				agents = append(agents, *a)
			}
		}(id, ka)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	// A cancellation that arrives after every agent has passed its own start
	// check leaves firstErr nil; surface it here so a cancelled or expired
	// context is honoured even on a fully offline run that no per-agent step
	// would otherwise report against.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].ID < agents[j].ID })
	return agents, nil
}

// detectMode carries the two orthogonal switches that shape one detection. Its
// zero value is the multi-agent Detect default: populate every found agent, no
// single-target semantics.
type detectMode struct {
	// omitIfMissing drops a not-found binary as (nil, nil): the multi-agent Detect
	// path without IncludeMissing. Cleared, the agent is populated either way.
	omitIfMissing bool
	// single marks the DetectOne path. Only single distinguishes the
	// provider-agnostic hard-fail from the multi-agent soft-skip, since
	// IncludeMissing also clears omitIfMissing.
	single bool
}

// detectAgent applies the data-driven engine steps to one catalog entry. The
// mode selects drop-if-missing and single-target behaviour (see detectMode). An
// error is returned only when enrichment hits non-degradable schema drift or a
// single-target query lacks required providers.
func detectAgent(ctx context.Context, id string, ka KnownAgent, cfg *config, env pathEnv, mode detectMode) (*Agent, error) {
	// Honour cancellation before any work: presence and path resolution take no
	// context, and the version probe treats cancellation as a non-fatal empty
	// result, so without this check an offline or skip-version run would ignore
	// a cancelled or expired context entirely.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a := &Agent{
		ID:       id,
		Name:     ka.Name,
		Bin:      ka.Bin,
		Homepage: ka.Homepage,
	}

	a.BinaryPath, a.Found = locateBinary(id, ka, cfg)
	if mode.omitIfMissing && !a.Found {
		return nil, nil
	}

	a.Config = resolvePaths(ka.Config, env)
	if ka.Skills != nil {
		a.Skills = resolvePaths(*ka.Skills, env)
	}
	// Agnostic agents take the caller-supplied set; home-provider agents always
	// use the catalog list and ignore WithProviders.
	if ka.Agnostic {
		if len(cfg.callerProviders) > 0 {
			a.Providers = append([]string(nil), cfg.callerProviders...)
		}
	} else if len(ka.Provider) > 0 {
		a.Providers = append([]string(nil), ka.Provider...)
	}
	if a.Found && ka.Version != nil && !cfg.skipVersion {
		a.Version = probeVersion(ctx, a.BinaryPath, *ka.Version)
	}
	if cfg.models != nil && ka.Agnostic && len(cfg.callerProviders) == 0 {
		// A targeted DetectOne fails so the missing providers surface; multi-agent
		// Detect soft-skips enrichment so a mixed catalog still lists.
		if mode.single {
			return nil, fmt.Errorf("%w: %q is provider-agnostic; supply providers (e.g. --provider)", ErrProvidersRequired, id)
		}
		return a, nil
	}
	if err := enrich(ctx, a, cfg, ka.Agnostic); err != nil {
		return nil, err
	}
	return a, nil
}
