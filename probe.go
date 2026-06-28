package agentdex

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/start-cli/agentdex/modelsdev"
)

// locateBinary resolves an agent's binary. An explicit per-agent override wins
// outright — it is the sole candidate, not a hint that falls through to PATH —
// but it is still verified to exist and be executable, so Found reflects reality
// the same way the PATH and search-dir strategies do. Otherwise PATH is searched
// (exec.LookPath), then any extra search dirs. The result is made absolute so it
// satisfies the BinaryPath contract and so the version exec runs it directly
// rather than re-resolving through PATH.
func locateBinary(id string, ka KnownAgent, cfg *config) (string, bool) {
	if override, ok := cfg.binPaths[id]; ok && override != "" {
		if isExecutable(override) {
			return absPath(override), true
		}
		return "", false
	}
	if p, err := exec.LookPath(ka.Bin); err == nil {
		return absPath(p), true
	}
	for _, dir := range cfg.searchDirs {
		candidate := filepath.Join(dir, ka.Bin)
		if isExecutable(candidate) {
			return absPath(candidate), true
		}
	}
	return "", false
}

// resolvePaths expands a catalog PathPair into resolved global/local paths with
// per-scope existence. Local is resolved relative to the working directory when
// it is not already absolute. An empty local scope stays empty and not-existing.
func resolvePaths(pp PathPair, env pathEnv) ResolvedPaths {
	rp := ResolvedPaths{
		Global: expandPath(pp.Global, env),
	}
	rp.GlobalExists = pathExists(rp.Global)
	if pp.Local != "" {
		local := expandPath(pp.Local, env)
		if !filepath.IsAbs(local) {
			local = filepath.Join(env.wd, local)
		}
		rp.Local = local
		rp.LocalExists = pathExists(rp.Local)
	}
	return rp
}

// expandPath applies environment-variable expansion and then leading-tilde
// expansion to a catalog path. Order is deliberate: env expansion first so a
// value like "$XDG_CONFIG_HOME/agent" resolves, tilde second for the "~/..." form.
//
// Expansion uses the environment snapshot captured at the run boundary (env.env),
// not the live process environment, so path resolution stays a pure function of
// its inputs. There is no XDG home fallback: an unset variable becomes empty, so
// "$XDG_CONFIG_HOME/agent" with XDG_CONFIG_HOME unset yields "/agent", not
// "~/.config/agent". The engine is a plain tilde+env expander by design;
// XDG-with-fallback resolution is the loader's job for its own cache path. Catalog
// entries should therefore write home-rooted paths ("~/.config/agent") rather than
// rely on bare XDG variables.
func expandPath(raw string, env pathEnv) string {
	if raw == "" {
		return ""
	}
	expanded := os.Expand(raw, func(key string) string { return env.env[key] })
	switch {
	case expanded == "~":
		return env.home
	case strings.HasPrefix(expanded, "~/"):
		return filepath.Join(env.home, expanded[len("~/"):])
	}
	return expanded
}

// environSnapshot captures the process environment once at the run boundary so
// expandPath resolves $VAR against a fixed map rather than the live environment.
// An entry without a '=' (not produced on the target platforms) is skipped.
func environSnapshot() map[string]string {
	pairs := os.Environ()
	m := make(map[string]string, len(pairs))
	for _, kv := range pairs {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func pathExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func isExecutable(p string) bool {
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// enrich fills ProviderEnv (and, when EnrichModels was requested, Models) from
// models.dev for the agent's providers. With no client it is a no-op. A transient
// gap — models.dev unreachable with no cache — degrades to nil for both rather
// than failing; a modelsdev.ErrModelsSchema from a requested provider propagates
// so schema drift stays loud. Provider-env is read through the validating
// accessor so a malformed model in a requested provider raises ErrModelsSchema
// even when per-model enrichment is off; the two are independent.
func enrich(ctx context.Context, a *Agent, cfg *config) error {
	if cfg.models == nil {
		return nil
	}
	mc := cfg.models.client

	providerEnv := make(map[string]bool)
	var found []string
	for _, pid := range a.Providers {
		p, ok, err := mc.Provider(ctx, pid)
		if err != nil {
			if errors.Is(err, modelsdev.ErrModelsSchema) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err() // caller cancelled or deadline expired, not a transient models.dev gap
			}
			return nil // transient: degrade, leaving ProviderEnv and Models nil
		}
		if !ok {
			continue // provider absent from models.dev is a per-provider fact, not an error
		}
		found = append(found, pid)
		for _, env := range p.Env {
			_, present := os.LookupEnv(env)
			providerEnv[env] = present
		}
	}
	// providerEnv stays nil unless models.dev was actually consulted: with at
	// least one provider, mc.Provider ran (even if the provider was absent),
	// which is the non-nil-empty "consulted, nothing present" case. No providers
	// means no consultation, so ProviderEnv stays nil per its contract.
	if len(a.Providers) > 0 {
		a.ProviderEnv = providerEnv
	}

	if cfg.models.enrich && len(found) > 0 {
		// found holds only providers mc.Provider already validated, and the client
		// memoises its catalog, so this cannot fail transiently; any error is a
		// genuine fault and propagates rather than degrading.
		models, err := mc.Models(ctx, found...)
		if err != nil {
			return err
		}
		a.Models = models
	}
	return nil
}
