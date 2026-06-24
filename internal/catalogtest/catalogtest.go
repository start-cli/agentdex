// Package catalogtest provides a stub registry and fixture helpers shared by
// the catalog loader tests and the root-package mapping tests. It lets the
// loader run its real load, validate, and cache logic against an on-disk
// fixture module with no registry, and supports injecting resolve/fetch
// failures.
package catalogtest

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// StubRegistry implements catalog.Registry. OnResolve and OnFetch supply the
// canned outcomes; call counts are recorded per module path so tests can assert
// that, e.g., a within-TTL load did not re-resolve, or that resolving one module
// path never touches another's entry.
type StubRegistry struct {
	OnResolve func(ctx context.Context, modulePath string) (string, error)
	OnFetch   func(ctx context.Context, modulePath string) (string, error)

	mu           sync.Mutex
	resolveCalls map[string]int
	fetchCalls   map[string]int
}

// Serve returns a stub that resolves the given base module path to version and
// fetches its canonical form from sourceDir. Other module paths are served from
// the same version/sourceDir too, which keeps single-module tests trivial; tests
// that care about per-path behaviour set OnResolve/OnFetch directly.
func Serve(version, sourceDir string) *StubRegistry {
	return &StubRegistry{
		OnResolve: func(context.Context, string) (string, error) { return version, nil },
		OnFetch:   func(context.Context, string) (string, error) { return sourceDir, nil },
	}
}

func (s *StubRegistry) ResolveLatestVersion(ctx context.Context, modulePath string) (string, error) {
	s.record(&s.resolveCalls, modulePath)
	return s.OnResolve(ctx, modulePath)
}

func (s *StubRegistry) Fetch(ctx context.Context, modulePath string) (string, error) {
	s.record(&s.fetchCalls, modulePath)
	return s.OnFetch(ctx, modulePath)
}

// ResolveCalls returns how many times the given base module path was resolved.
func (s *StubRegistry) ResolveCalls(modulePath string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resolveCalls[modulePath]
}

// FetchCalls returns how many times a canonical form of the given path was
// fetched. Fetches are recorded under the canonical module@version, so a base
// path (…/catalog@v1) is matched against its canonical fetches (…/catalog@v1.0.0)
// by treating the base as a "base." prefix; an exact key also counts.
func (s *StubRegistry) FetchCalls(modulePath string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for key, n := range s.fetchCalls {
		if key == modulePath || strings.HasPrefix(key, modulePath+".") {
			total += n
		}
	}
	return total
}

// TotalResolveCalls returns the total number of resolve calls across all paths.
func (s *StubRegistry) TotalResolveCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, n := range s.resolveCalls {
		total += n
	}
	return total
}

func (s *StubRegistry) record(m *map[string]int, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if *m == nil {
		*m = make(map[string]int)
	}
	(*m)[key]++
}

// FixtureDir returns the absolute path to a fixture module under testdata,
// resolved relative to this source file so it works from any test package.
func FixtureDir(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("catalogtest: cannot determine source location")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(repoRoot, "testdata", name)
}
