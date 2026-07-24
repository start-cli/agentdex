package agentdex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/start-cli/agentdex/internal/catalogtest"
	"github.com/start-cli/agentdex/internal/modelsdevtest"
	"github.com/start-cli/agentdex/modelsdev"
)

// testCatalog is the standard fixture body: a home-provider agent (alpha-cli,
// anthropic), a multi-provider home agent (gamma-agent, google+openai), and a
// provider-agnostic agent (delta-agent).
const testCatalog = `
agents: "alpha-cli": {
	name: "Alpha CLI"
	bin:  "alpha"
	config: {global: "~/.alpha", local: ".alpha"}
	skills: {global: "~/.alpha/skills", local: ".alpha/skills"}
	version: {args: ["--version"], pattern: "v([0-9.]+)"}
	provider: ["anthropic"]
	homepage: "https://example.com/alpha"
}
agents: "gamma-agent": {
	name: "Gamma Agent"
	bin:  "gamma"
	config: {global: "~/.gamma"}
	provider: ["google", "openai"]
}
agents: "delta-agent": {
	name: "Delta Agent"
	bin:  "delta"
	config: {global: "~/.delta"}
	agnostic: true
}
`

func openAgents(t *testing.T, body string, opts ...Option) *Index {
	t.Helper()
	dir := catalogtest.WriteModule(t, body)
	base := []Option{WithCatalogDir(dir), WithCacheDir(t.TempDir())}
	idx, err := Open(context.Background(), append(base, opts...)...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return idx
}

// binDir writes executable fake binaries that print a version banner, so
// detection finds them through WithSearchDirs and the version probe extracts a
// value.
func binDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("#!/bin/sh\necho v1.0.0\n"), 0o755); err != nil {
			t.Fatalf("write fake bin: %v", err)
		}
		if err := os.Chmod(p, 0o755); err != nil {
			t.Fatalf("chmod fake bin: %v", err)
		}
	}
	return dir
}

// envFn returns an environment lookup with a fixed HOME and the named variables
// marked present.
func envFn(home string, present ...string) func(string) (string, bool) {
	set := map[string]struct{}{}
	for _, k := range present {
		set[k] = struct{}{}
	}
	return func(k string) (string, bool) {
		if k == "HOME" {
			return home, true
		}
		_, ok := set[k]
		return "", ok
	}
}

func hasWarning(ws []Warning, kind WarningKind) bool {
	for _, w := range ws {
		if w.Kind == kind {
			return true
		}
	}
	return false
}

func warningMsg(ws []Warning, kind WarningKind) (string, bool) {
	for _, w := range ws {
		if w.Kind == kind {
			return w.Msg, true
		}
	}
	return "", false
}

func TestGetDetectionFactsOfflineAtEnrichNone(t *testing.T) {
	home := t.TempDir()
	wd := t.TempDir()
	idx := openAgents(t, testCatalog,
		WithSearchDirs(binDir(t, "alpha")),
		WithEnvLookup(envFn(home)),
		WithWorkingDir(wd),
		WithModelsURL(modelsdevtest.MustNotFetch(t)),
	)

	d, err := idx.Agents.Get(context.Background(), "alpha-cli", AgentGetQuery{Enrich: EnrichNone})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !d.Detection.Found {
		t.Fatal("alpha-cli should be found")
	}
	if d.Detection.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", d.Detection.Version)
	}
	if d.Detection.Config.Global != filepath.Join(home, ".alpha") {
		t.Errorf("Config.Global = %q, want %s/.alpha", d.Detection.Config.Global, home)
	}
	if d.Detection.Config.Local != filepath.Join(wd, ".alpha") {
		t.Errorf("Config.Local = %q, want %s/.alpha", d.Detection.Config.Local, wd)
	}
	if d.Enrichment != EnrichNotRequested {
		t.Errorf("Enrichment = %v, want EnrichNotRequested", d.Enrichment)
	}
	if len(d.Providers) != 0 {
		t.Errorf("Providers = %v, want none at EnrichNone", d.Providers)
	}
	if d.Coverage.Status != CoverageNotProbed {
		t.Errorf("Coverage.Status = %v, want CoverageNotProbed", d.Coverage.Status)
	}
}

func TestGetHomeProviderEnrichProvidersOffline(t *testing.T) {
	idx := openAgents(t, testCatalog,
		WithSearchDirs(binDir(t, "alpha")),
		WithEnvLookup(envFn(t.TempDir())),
		WithModelsURL(modelsdevtest.MustNotFetch(t)),
	)
	d, err := idx.Agents.Get(context.Background(), "alpha-cli", AgentGetQuery{Enrich: EnrichProviders})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(d.Providers) != 1 || d.Providers[0] != "anthropic" {
		t.Errorf("Providers = %v, want [anthropic]", d.Providers)
	}
	if d.Enrichment != EnrichApplied {
		t.Errorf("Enrichment = %v, want EnrichApplied", d.Enrichment)
	}
	if d.Coverage.Status != CoverageNotProbed {
		t.Errorf("Coverage.Status = %v, want CoverageNotProbed", d.Coverage.Status)
	}
}

func TestGetHomeProviderRejectsExplicitProvidersEveryLevel(t *testing.T) {
	idx := openAgents(t, testCatalog,
		WithSearchDirs(binDir(t, "alpha")),
		WithEnvLookup(envFn(t.TempDir())),
		WithModelsURL(modelsdevtest.MustNotFetch(t)),
	)
	for _, lvl := range []Enrich{EnrichNone, EnrichProviders, EnrichCount, EnrichFull} {
		d, err := idx.Agents.Get(context.Background(), "alpha-cli", AgentGetQuery{Enrich: lvl, Providers: []string{"anthropic"}})
		if !errors.Is(err, ErrProvidersNotAllowed) {
			t.Errorf("level %v: err = %v, want ErrProvidersNotAllowed", lvl, err)
		}
		if err != nil && err.Error() != `agent "alpha-cli" has catalog providers` {
			t.Errorf("level %v: message = %q", lvl, err.Error())
		}
		_ = d
	}
}

func TestGetAgnosticNoProvidersNotApplicable(t *testing.T) {
	idx := openAgents(t, testCatalog,
		WithSearchDirs(binDir(t, "delta")),
		WithEnvLookup(envFn(t.TempDir())),
		WithModelsURL(modelsdevtest.MustNotFetch(t)), // no round-trip at any level
	)
	for _, lvl := range []Enrich{EnrichCount, EnrichFull} {
		d, err := idx.Agents.Get(context.Background(), "delta-agent", AgentGetQuery{Enrich: lvl})
		if err != nil {
			t.Fatalf("level %v: Get: %v", lvl, err)
		}
		if d.Enrichment != EnrichNotApplicable {
			t.Errorf("level %v: Enrichment = %v, want EnrichNotApplicable", lvl, d.Enrichment)
		}
		if msg, ok := warningMsg(d.Warnings, WarnProvidersRequired); !ok || msg != `"delta-agent" is provider-agnostic` {
			t.Errorf("level %v: providers-required warning = %q (present=%v)", lvl, msg, ok)
		}
		if d.Coverage.Status != CoverageNotProbed {
			t.Errorf("level %v: Coverage.Status = %v, want CoverageNotProbed", lvl, d.Coverage.Status)
		}
	}
}

func TestGetAgnosticValidatesCallerProviders(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"google"})
	idx := openAgents(t, testCatalog,
		WithSearchDirs(binDir(t, "delta")),
		WithEnvLookup(envFn(t.TempDir(), "GOOGLE_API_KEY")),
		WithModelsURL(srv.URL),
	)
	// Known provider resolves.
	d, err := idx.Agents.Get(context.Background(), "delta-agent", AgentGetQuery{Enrich: EnrichFull, Providers: []string{"google"}})
	if err != nil {
		t.Fatalf("Get google: %v", err)
	}
	if len(d.Providers) != 1 || d.Providers[0] != "google" {
		t.Errorf("Providers = %v, want [google]", d.Providers)
	}
	if d.Enrichment != EnrichApplied || d.ModelCount != 1 {
		t.Errorf("Enrichment=%v ModelCount=%d, want EnrichApplied 1", d.Enrichment, d.ModelCount)
	}
	// Unknown provider is rejected.
	_, err = idx.Agents.Get(context.Background(), "delta-agent", AgentGetQuery{Enrich: EnrichProviders, Providers: []string{"bogus"}})
	if !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("unknown provider err = %v, want ErrUnknownProvider", err)
	}
}

func TestGetCoverageVerdicts(t *testing.T) {
	t.Run("all present", func(t *testing.T) {
		srv := modelsdevtest.Server(t, []string{"google", "openai"})
		idx := openAgents(t, testCatalog,
			WithSearchDirs(binDir(t, "gamma")),
			WithEnvLookup(envFn(t.TempDir(), "GOOGLE_API_KEY")),
			WithModelsURL(srv.URL),
		)
		d, err := idx.Agents.Get(context.Background(), "gamma-agent", AgentGetQuery{Enrich: EnrichFull})
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if d.Coverage.Status != CoverageAllPresent {
			t.Errorf("Status = %v, want CoverageAllPresent", d.Coverage.Status)
		}
		if d.Enrichment != EnrichApplied || d.ModelCount != 2 {
			t.Errorf("Enrichment=%v ModelCount=%d, want EnrichApplied 2", d.Enrichment, d.ModelCount)
		}
		if d.ProviderEnv["GOOGLE_API_KEY"] != true || d.ProviderEnv["OPENAI_API_KEY"] != false {
			t.Errorf("ProviderEnv = %v, want GOOGLE present, OPENAI absent", d.ProviderEnv)
		}
		// Newest release first: openai (2025-01-01) before google (2024-01-01).
		if len(d.Models) != 2 || d.Models[0].ID != "openai-model" {
			t.Errorf("Models order = %v, want openai-model first", modelIDs(d.Models))
		}
	})

	t.Run("some present", func(t *testing.T) {
		srv := modelsdevtest.Server(t, []string{"google"}) // openai absent
		idx := openAgents(t, testCatalog,
			WithSearchDirs(binDir(t, "gamma")),
			WithEnvLookup(envFn(t.TempDir(), "GOOGLE_API_KEY")),
			WithModelsURL(srv.URL),
		)
		d, err := idx.Agents.Get(context.Background(), "gamma-agent", AgentGetQuery{Enrich: EnrichFull})
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if d.Coverage.Status != CoverageSomePresent {
			t.Errorf("Status = %v, want CoverageSomePresent", d.Coverage.Status)
		}
		if len(d.Coverage.Absent) != 1 || d.Coverage.Absent[0] != "openai" {
			t.Errorf("Absent = %v, want [openai]", d.Coverage.Absent)
		}
		if msg, ok := warningMsg(d.Warnings, WarnSomeProvidersAbsent); !ok || msg != "some providers are absent from models.dev: openai" {
			t.Errorf("some-absent warning = %q (present=%v)", msg, ok)
		}
		if len(d.Models) != 1 || d.Models[0].ID != "google-model" {
			t.Errorf("Models = %v, want the present provider's model", modelIDs(d.Models))
		}
	})

	t.Run("none present", func(t *testing.T) {
		srv := modelsdevtest.Server(t, []string{"google"}) // alpha uses anthropic, absent
		idx := openAgents(t, testCatalog,
			WithSearchDirs(binDir(t, "alpha")),
			WithEnvLookup(envFn(t.TempDir())),
			WithModelsURL(srv.URL),
		)
		d, err := idx.Agents.Get(context.Background(), "alpha-cli", AgentGetQuery{Enrich: EnrichFull})
		if err != nil {
			t.Fatalf("Get should not fail on a coverage verdict: %v", err)
		}
		if d.Coverage.Status != CoverageNonePresent {
			t.Errorf("Status = %v, want CoverageNonePresent", d.Coverage.Status)
		}
		if len(d.Coverage.Absent) != 1 || d.Coverage.Absent[0] != "anthropic" {
			t.Errorf("Absent = %v, want [anthropic]", d.Coverage.Absent)
		}
		if d.Enrichment != EnrichApplied {
			t.Errorf("Enrichment = %v, want EnrichApplied (a true zero)", d.Enrichment)
		}
	})

	t.Run("schema drift", func(t *testing.T) {
		srv := modelsdevtest.Server(t, []string{"google"}, "anthropic") // anthropic malformed
		idx := openAgents(t, testCatalog,
			WithSearchDirs(binDir(t, "alpha")),
			WithEnvLookup(envFn(t.TempDir())),
			WithModelsURL(srv.URL),
		)
		d, err := idx.Agents.Get(context.Background(), "alpha-cli", AgentGetQuery{Enrich: EnrichFull})
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if d.Coverage.Status != CoverageSchemaDrift {
			t.Errorf("Status = %v, want CoverageSchemaDrift", d.Coverage.Status)
		}
		if !errors.Is(d.Coverage.Err, modelsdev.ErrModelsSchema) {
			t.Errorf("Coverage.Err = %v, want to wrap ErrModelsSchema", d.Coverage.Err)
		}
		if d.Enrichment != EnrichDegraded {
			t.Errorf("Enrichment = %v, want EnrichDegraded", d.Enrichment)
		}
		if hasWarning(d.Warnings, WarnModelsUnreachable) {
			t.Error("schema drift on Get must not raise the unreachable warning")
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		idx := openAgents(t, testCatalog,
			WithSearchDirs(binDir(t, "alpha")),
			WithEnvLookup(envFn(t.TempDir())),
			WithModelsURL(modelsdevtest.Closed(t)),
		)
		d, err := idx.Agents.Get(context.Background(), "alpha-cli", AgentGetQuery{Enrich: EnrichFull})
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if d.Coverage.Status != CoverageUnreachable {
			t.Errorf("Status = %v, want CoverageUnreachable", d.Coverage.Status)
		}
		if d.Enrichment != EnrichDegraded {
			t.Errorf("Enrichment = %v, want EnrichDegraded", d.Enrichment)
		}
		if msg, ok := warningMsg(d.Warnings, WarnModelsUnreachable); !ok || msg != "models.dev is unreachable and not cached: model enrichment and provider-env omitted" {
			t.Errorf("unreachable warning = %q (present=%v)", msg, ok)
		}
	})
}

func TestGetNotInstalledEnrichesLikeInstalled(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic"})
	idx := openAgents(t, testCatalog,
		// alpha binary not installed.
		WithSearchDirs(binDir(t)),
		WithEnvLookup(envFn(t.TempDir(), "ANTHROPIC_API_KEY")),
		WithModelsURL(srv.URL),
	)
	d, err := idx.Agents.Get(context.Background(), "alpha-cli", AgentGetQuery{Enrich: EnrichFull})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if d.Detection.Found {
		t.Fatal("alpha-cli should not be installed")
	}
	if !hasWarning(d.Warnings, WarnNotInstalled) {
		t.Errorf("expected the not-installed warning: %v", d.Warnings)
	}
	if msg, _ := warningMsg(d.Warnings, WarnNotInstalled); msg != `agent "alpha-cli" is catalogued but not installed` {
		t.Errorf("not-installed message = %q", msg)
	}
	// Enrichment does not depend on installation (R4): coverage, provider-env, and
	// models are all filled for the absent binary.
	if d.Coverage.Status != CoverageAllPresent || d.Enrichment != EnrichApplied {
		t.Errorf("Status=%v Enrichment=%v, want CoverageAllPresent EnrichApplied", d.Coverage.Status, d.Enrichment)
	}
	if d.ModelCount != 1 || len(d.Models) != 1 {
		t.Errorf("ModelCount=%d Models=%v, want 1 model filled", d.ModelCount, modelIDs(d.Models))
	}
	if _, ok := d.ProviderEnv["ANTHROPIC_API_KEY"]; !ok {
		t.Errorf("ProviderEnv = %v, want ANTHROPIC_API_KEY reported", d.ProviderEnv)
	}
}

func TestGetEnrichCountOmitsModelsList(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic"})
	idx := openAgents(t, testCatalog,
		WithSearchDirs(binDir(t, "alpha")),
		WithEnvLookup(envFn(t.TempDir(), "ANTHROPIC_API_KEY")),
		WithModelsURL(srv.URL),
	)
	d, err := idx.Agents.Get(context.Background(), "alpha-cli", AgentGetQuery{Enrich: EnrichCount})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if d.ModelCount != 1 {
		t.Errorf("ModelCount = %d, want 1", d.ModelCount)
	}
	if d.Models != nil {
		t.Errorf("Models = %v, want nil at EnrichCount", modelIDs(d.Models))
	}
	if d.ProviderEnv == nil {
		t.Error("ProviderEnv should be filled at EnrichCount")
	}
}

func TestGetUnknownAgentCarriesMessage(t *testing.T) {
	idx := openAgents(t, testCatalog, WithModelsURL(modelsdevtest.MustNotFetch(t)))
	_, err := idx.Agents.Get(context.Background(), "no-such", AgentGetQuery{Enrich: EnrichNone})
	if !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("err = %v, want ErrAgentUnknown", err)
	}
	if err.Error() != `no agent "no-such"` {
		t.Errorf("message = %q, want library text", err.Error())
	}
}

func TestListOrdersByIDAndNarrowsByInstalled(t *testing.T) {
	idx := openAgents(t, testCatalog,
		WithSearchDirs(binDir(t, "alpha", "gamma")), // delta not installed
		WithEnvLookup(envFn(t.TempDir())),
		WithModelsURL(modelsdevtest.MustNotFetch(t)),
	)
	res, err := idx.Agents.List(context.Background(), AgentQuery{Enrich: EnrichNone})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := agentIDs(res.Items); !equal(got, []string{"alpha-cli", "delta-agent", "gamma-agent"}) {
		t.Errorf("order = %v, want by id", got)
	}

	res, err = idx.Agents.List(context.Background(), AgentQuery{Enrich: EnrichNone, Installed: true})
	if err != nil {
		t.Fatalf("List installed: %v", err)
	}
	if got := agentIDs(res.Items); !equal(got, []string{"alpha-cli", "gamma-agent"}) {
		t.Errorf("installed = %v, want the detected agents", got)
	}
}

func TestListFilterNarrowsByIDAndName(t *testing.T) {
	idx := openAgents(t, testCatalog,
		WithSearchDirs(binDir(t, "alpha", "gamma", "delta")),
		WithEnvLookup(envFn(t.TempDir())),
		WithModelsURL(modelsdevtest.MustNotFetch(t)),
	)
	res, err := idx.Agents.List(context.Background(), AgentQuery{Enrich: EnrichNone, Filter: "alpha"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := agentIDs(res.Items); !equal(got, []string{"alpha-cli"}) {
		t.Errorf("filtered = %v, want [alpha-cli]", got)
	}
}

func TestListEnrichFullPerAgent(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic", "google", "openai"})
	idx := openAgents(t, testCatalog,
		WithSearchDirs(binDir(t, "alpha", "gamma", "delta")),
		WithEnvLookup(envFn(t.TempDir())),
		WithModelsURL(srv.URL),
	)
	res, err := idx.Agents.List(context.Background(), AgentQuery{Enrich: EnrichFull})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	by := byID(res.Items)
	if a := by["alpha-cli"]; a.Enrichment != EnrichApplied || a.ModelCount != 1 {
		t.Errorf("alpha: Enrichment=%v ModelCount=%d", a.Enrichment, a.ModelCount)
	}
	if g := by["gamma-agent"]; g.Enrichment != EnrichApplied || g.ModelCount != 2 {
		t.Errorf("gamma: Enrichment=%v ModelCount=%d", g.Enrichment, g.ModelCount)
	}
	// Agnostic row without a provider set is not-applicable and silent (R8).
	if d := by["delta-agent"]; d.Enrichment != EnrichNotApplicable || len(d.Models) != 0 {
		t.Errorf("delta: Enrichment=%v Models=%v, want not-applicable and empty", d.Enrichment, modelIDs(d.Models))
	}
	if hasWarning(res.Warnings, WarnProvidersRequired) {
		t.Error("a listing must not raise the agnostic guidance warning")
	}
	// The agnostic row carries the newest-first model order for gamma.
	if g := by["gamma-agent"]; g.Models[0].ID != "openai-model" {
		t.Errorf("gamma Models order = %v, want openai-model first", modelIDs(g.Models))
	}
}

func TestListDegradeWarnings(t *testing.T) {
	t.Run("unreachable", func(t *testing.T) {
		idx := openAgents(t, testCatalog,
			WithSearchDirs(binDir(t, "alpha")),
			WithEnvLookup(envFn(t.TempDir())),
			WithModelsURL(modelsdevtest.Closed(t)),
		)
		res, err := idx.Agents.List(context.Background(), AgentQuery{Enrich: EnrichFull, Installed: true})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if msg, ok := warningMsg(res.Warnings, WarnModelsUnreachable); !ok || msg != "model counts unavailable: models.dev is unreachable and not cached" {
			t.Errorf("list unreachable warning = %q (present=%v)", msg, ok)
		}
		if a := byID(res.Items)["alpha-cli"]; a.Enrichment != EnrichDegraded {
			t.Errorf("alpha Enrichment = %v, want EnrichDegraded", a.Enrichment)
		}
	})

	t.Run("schema drift", func(t *testing.T) {
		srv := modelsdevtest.Server(t, nil, "anthropic")
		idx := openAgents(t, testCatalog,
			WithSearchDirs(binDir(t, "alpha")),
			WithEnvLookup(envFn(t.TempDir())),
			WithModelsURL(srv.URL),
		)
		res, err := idx.Agents.List(context.Background(), AgentQuery{Enrich: EnrichFull, Installed: true})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		msg, ok := warningMsg(res.Warnings, WarnModelsSchemaDrift)
		if !ok || msg != `model counts omitted: provider "anthropic" model "claude-sonnet" malformed: models.dev schema unrecognised` {
			t.Errorf("list schema-drift warning = %q (present=%v)", msg, ok)
		}
	})
}

func TestListProviderValidationAtBoundary(t *testing.T) {
	t.Run("unknown provider fails even when result is empty", func(t *testing.T) {
		srv := modelsdevtest.Server(t, []string{"anthropic"})
		idx := openAgents(t, testCatalog,
			WithSearchDirs(binDir(t)), // nothing installed
			WithEnvLookup(envFn(t.TempDir())),
			WithModelsURL(srv.URL),
		)
		_, err := idx.Agents.List(context.Background(), AgentQuery{Enrich: EnrichFull, Installed: true, Providers: []string{"bogus"}})
		if !errors.Is(err, ErrUnknownProvider) {
			t.Errorf("err = %v, want ErrUnknownProvider regardless of which binaries are present", err)
		}
	})

	t.Run("unreachable degrades not rejects", func(t *testing.T) {
		idx := openAgents(t, testCatalog,
			WithSearchDirs(binDir(t, "alpha")),
			WithEnvLookup(envFn(t.TempDir())),
			WithModelsURL(modelsdevtest.Closed(t)),
		)
		res, err := idx.Agents.List(context.Background(), AgentQuery{Enrich: EnrichFull, Providers: []string{"anthropic"}})
		if err != nil {
			t.Fatalf("List: %v, want no rejection on an outage", err)
		}
		if !hasWarning(res.Warnings, WarnModelsUnreachable) {
			t.Errorf("expected a degrade warning, got %v", res.Warnings)
		}
	})
}

func TestListFetchesModelsDevOnce(t *testing.T) {
	srv, count := modelsdevtest.CountingServer(t, []string{"anthropic", "google", "openai"})
	idx := openAgents(t, testCatalog,
		WithSearchDirs(binDir(t, "alpha", "gamma")),
		WithEnvLookup(envFn(t.TempDir())),
		WithModelsURL(srv.URL),
	)
	if _, err := idx.Agents.List(context.Background(), AgentQuery{Enrich: EnrichFull}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if n := count.Load(); n != 1 {
		t.Errorf("models.dev fetched %d times, want once", n)
	}
}

func TestGetNoLocalConfigOrSkills(t *testing.T) {
	body := `
agents: "beta-tool": {
	name: "Beta Tool"
	bin:  "beta"
	config: {global: "~/.config/beta"}
	provider: ["openai"]
}
`
	home := t.TempDir()
	idx := openAgents(t, body,
		WithSearchDirs(binDir(t, "beta")),
		WithEnvLookup(envFn(home)),
		WithModelsURL(modelsdevtest.MustNotFetch(t)),
	)
	d, err := idx.Agents.Get(context.Background(), "beta-tool", AgentGetQuery{Enrich: EnrichNone})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if d.Detection.Config.Local != "" {
		t.Errorf("Config.Local = %q, want empty", d.Detection.Config.Local)
	}
	if (d.Detection.Skills != ResolvedPaths{}) {
		t.Errorf("Skills = %+v, want zero value for an agent with no skills", d.Detection.Skills)
	}
}

func TestGetHonoursCancelledContext(t *testing.T) {
	idx := openAgents(t, testCatalog,
		WithSearchDirs(binDir(t, "alpha")),
		WithEnvLookup(envFn(t.TempDir())),
		WithModelsURL(modelsdevtest.MustNotFetch(t)),
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := idx.Agents.List(ctx, AgentQuery{Enrich: EnrichNone})
	if err == nil {
		t.Error("cancelled context should surface an error")
	}
}

// helpers for asserting id/model ordering.

func agentIDs(agents []Agent) []string {
	out := make([]string, len(agents))
	for i, a := range agents {
		out[i] = a.ID
	}
	return out
}

func byID(agents []Agent) map[string]Agent {
	m := make(map[string]Agent, len(agents))
	for _, a := range agents {
		m[a.ID] = a
	}
	return m
}

func modelIDs(models []modelsdev.Model) []string {
	out := make([]string, len(models))
	for i, m := range models {
		out[i] = m.ID
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
