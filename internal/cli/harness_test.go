package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"cuelang.org/go/mod/modcache"
	"cuelang.org/go/mod/modregistrytest"

	"github.com/start-cli/agentdex/internal/catalogtest"
	"github.com/start-cli/agentdex/modelsdev"
)

// result is the captured outcome of one CLI invocation.
type result struct {
	stdout string
	stderr string
	code   int
}

// env decode helper for asserting envelope JSON.
func (r result) envelope(t *testing.T) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal([]byte(r.stdout), &env); err != nil {
		t.Fatalf("decode envelope from %q: %v", r.stdout, err)
	}
	return env
}

// runCLI builds a fresh command tree, runs it with args against captured buffers,
// and maps the resulting error to an exit code exactly as Execute does.
func runCLI(args ...string) result {
	root := NewRootCommand()
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)

	code := codeOK
	if err := root.Execute(); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			code = ee.code
		} else {
			code = codeUsage
			fmt.Fprintln(&errb, "error: "+err.Error())
		}
	}
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

// scenario captures the per-test world: temp XDG dirs, a fake-binary directory, a
// local catalog registry, and an optional models.dev server, wired through
// config.cue.
type scenario struct {
	home          string
	binDir        string
	configDir     string
	closeRegistry func() // shuts down the in-process catalog registry mid-test
}

// newScenario stands up an isolated agentdex world: temp HOME and XDG dirs, the
// fixture catalog published to an in-process OCI registry, fake agent binaries on
// a search dir, and a config.cue pointing model enrichment at modelsURL (empty to
// omit it). bins lists which fixture agent binaries to install, so a test can make
// an agent "not installed" by leaving its binary out.
func newScenario(t *testing.T, modelsURL string, bins ...string) *scenario {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	closeRegistry := startCatalogRegistry(t)

	binDir := filepath.Join(home, "bin")
	mustMkdir(t, binDir)
	for _, name := range bins {
		installFakeBin(t, binDir, name)
	}

	configDir := filepath.Join(home, ".config", "agentdex")
	mustMkdir(t, configDir)
	var b strings.Builder
	b.WriteString("color: \"never\"\n")
	fmt.Fprintf(&b, "search_dirs: [%q]\n", binDir)
	if modelsURL != "" {
		fmt.Fprintf(&b, "models: url: %q\n", modelsURL)
	}
	writeFile(t, filepath.Join(configDir, "config.cue"), b.String())

	return &scenario{home: home, binDir: binDir, configDir: configDir, closeRegistry: closeRegistry}
}

// writeConfig overwrites the scenario's config.cue, for tests that need a
// malformed or bespoke configuration.
func (s *scenario) writeConfig(t *testing.T, body string) {
	t.Helper()
	writeFile(t, filepath.Join(s.configDir, "config.cue"), body)
}

// fixtureBins are the catalog-valid fixture's binary names by agent id.
var fixtureBins = map[string]string{
	"alpha-cli":   "alpha",
	"beta-tool":   "beta",
	"gamma-agent": "gamma",
}

func installFakeBin(t *testing.T, dir, agentID string) {
	t.Helper()
	path := filepath.Join(dir, fixtureBinName(t, agentID))
	writeFile(t, path, "#!/bin/sh\necho \"v1.0.0\"\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod fake bin: %v", err)
	}
}

// installCountingBin installs a fake binary that appends a line to counterPath on
// each invocation, so a test can assert how many times the version probe exec'd
// the binary.
func installCountingBin(t *testing.T, dir, agentID, counterPath string) {
	t.Helper()
	path := filepath.Join(dir, fixtureBinName(t, agentID))
	writeFile(t, path, fmt.Sprintf("#!/bin/sh\necho run >> %q\necho \"v1.0.0\"\n", counterPath))
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod counting bin: %v", err)
	}
}

// probeCount returns how many times the counting binary was invoked, or 0 if it
// was never run.
func probeCount(t *testing.T, counterPath string) int {
	t.Helper()
	data, err := os.ReadFile(counterPath)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("read probe counter: %v", err)
	}
	return strings.Count(string(data), "\n")
}

func fixtureBinName(t *testing.T, agentID string) string {
	t.Helper()
	name, ok := fixtureBins[agentID]
	if !ok {
		t.Fatalf("unknown fixture agent %q", agentID)
	}
	return name
}

// modelsServer serves a tailored models.dev catalog.json. present lists the
// providers to expose (each with one valid model); malformed lists providers to
// expose with a model that has a zero limit, to trigger ErrModelsSchema.
func modelsServer(t *testing.T, present []string, malformed ...string) *httptest.Server {
	t.Helper()
	data := modelsCatalog(present, malformed)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// closedModelsServer returns the URL of a server that is already shut down, so a
// fetch against it fails: the no-network, no-cache condition.
func closedModelsServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	return url
}

// modelsCatalog marshals a models.dev catalog.json with the requested providers.
func modelsCatalog(present, malformed []string) []byte {
	cat := modelsdev.Catalog{
		// A non-empty agnostic map keeps validateTopLevel happy and carries the
		// canonical id used by the canonical-id tests.
		Models: map[string]modelsdev.Model{
			"anthropic/claude-sonnet": {ID: "anthropic/claude-sonnet", Name: "Claude Sonnet", Limit: modelsdev.Limit{Context: 200000}},
		},
		Providers: map[string]modelsdev.Provider{},
	}
	for _, pid := range present {
		cat.Providers[pid] = provider(pid, false)
	}
	for _, pid := range malformed {
		cat.Providers[pid] = provider(pid, true)
	}
	data, err := json.Marshal(cat)
	if err != nil {
		panic(err)
	}
	return data
}

// providerReleaseDates gives each fixture provider's model a distinct release
// date, so newest-first ordering is observable across providers: openai's model
// is newer than google's even though google sorts first by id.
var providerReleaseDates = map[string]string{
	"anthropic": "2025-06-01",
	"google":    "2024-01-01",
	"openai":    "2025-01-01",
}

// provider builds one provider with a single model. anthropic carries the
// "claude-sonnet" model so the canonical-id path resolves; other providers carry a
// generic model. A malformed provider's model has a zero limit.
func provider(pid string, malformed bool) modelsdev.Provider {
	limit := modelsdev.Limit{Context: 200000, Output: 8192}
	if malformed {
		limit = modelsdev.Limit{}
	}
	key, name := pid+"-model", pid+" model"
	if pid == "anthropic" {
		key, name = "claude-sonnet", "Claude Sonnet"
	}
	return modelsdev.Provider{
		ID:   pid,
		Name: strings.ToUpper(pid[:1]) + pid[1:],
		Env:  []string{strings.ToUpper(pid) + "_API_KEY"},
		Models: map[string]modelsdev.Model{
			key: {
				ID:          key,
				Name:        name,
				ReleaseDate: providerReleaseDates[pid],
				Limit:       limit,
				Cost:        &modelsdev.Cost{Input: 3, Output: 15},
			},
		},
	}
}

// startCatalogRegistry publishes the catalog-valid fixture to an in-process OCI
// registry and points CUE_REGISTRY and CUE_CACHE_DIR at it, so the production
// loader resolves the default catalog module fully offline. It returns a closer,
// also registered for cleanup, so a test can take the registry offline mid-run.
func startCatalogRegistry(t *testing.T) func() {
	t.Helper()
	dir := catalogtest.FixtureDir(t, "catalog-valid")
	const moduleDir = "github.com_start-cli_agentdex_catalog_v1.0.0"

	fsys := fstest.MapFS{}
	for _, rel := range []string{"cue.mod/module.cue", "schema.cue", "agents.cue"} {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("read fixture %s: %v", rel, err)
		}
		fsys[path.Join(moduleDir, rel)] = &fstest.MapFile{Data: data}
	}
	reg, err := modregistrytest.New(fsys, "")
	if err != nil {
		t.Fatalf("start local registry: %v", err)
	}
	var once sync.Once
	closeReg := func() { once.Do(reg.Close) }
	t.Cleanup(closeReg)

	t.Setenv("CUE_REGISTRY", reg.Host()+"+insecure")
	t.Setenv("CUE_CACHE_DIR", cueCacheDir(t))
	return closeReg
}

func cueCacheDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "agentdex-cli-cue-cache")
	if err != nil {
		t.Fatalf("create cue cache dir: %v", err)
	}
	t.Cleanup(func() { _ = modcache.RemoveAll(dir) })
	return dir
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
