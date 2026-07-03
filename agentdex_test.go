package agentdex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// ptr returns a pointer to v, for building optional KnownAgent fields inline.
func ptr[T any](v T) *T { return &v }

// writeStub writes an executable shell script that prints output, and returns its
// path. Target platforms are Linux, macOS, and WSL, where a shebang script is a
// valid executable for exec and version probing.
func writeStub(t *testing.T, dir, name, output string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\necho " + strconv.Quote(output) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

func TestDetectOmitsNotFoundAndSortsByID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	alphaBin := writeStub(t, dir, "alpha", "alpha 1.0.0")
	gammaBin := writeStub(t, dir, "gamma", "gamma 3.0.0")

	cat := &Catalog{Agents: map[string]KnownAgent{
		"gamma": {Name: "Gamma", Bin: "gamma", Config: PathPair{Global: "~/.gamma"}},
		"alpha": {Name: "Alpha", Bin: "alpha", Config: PathPair{Global: "~/.alpha"}},
		"beta":  {Name: "Beta", Bin: "beta-not-installed", Config: PathPair{Global: "~/.beta"}},
	}}

	agents, err := Detect(context.Background(),
		WithCatalog(cat),
		WithBinPaths(map[string]string{"alpha": alphaBin, "gamma": gammaBin}),
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2 (beta omitted as not found)", len(agents))
	}
	if agents[0].ID != "alpha" || agents[1].ID != "gamma" {
		t.Errorf("agents not sorted by id: %q, %q", agents[0].ID, agents[1].ID)
	}
	if !agents[0].Found || agents[0].BinaryPath != alphaBin {
		t.Errorf("alpha found=%v path=%q, want true %q", agents[0].Found, agents[0].BinaryPath, alphaBin)
	}
}

func TestDetectIncludeMissingKeepsNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	alphaBin := writeStub(t, dir, "alpha", "alpha 1.0.0")

	cat := &Catalog{Agents: map[string]KnownAgent{
		"alpha": {Name: "Alpha", Bin: "alpha", Config: PathPair{Global: "~/.alpha"}},
		"beta": {Name: "Beta", Bin: "beta-not-installed", Config: PathPair{Global: "~/.beta"},
			Version: ptr(VersionProbe{Args: []string{"--version"}, Pattern: `([0-9.]+)`})},
	}}

	agents, err := Detect(context.Background(),
		WithCatalog(cat),
		WithBinPaths(map[string]string{"alpha": alphaBin}),
		IncludeMissing(),
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2 (beta kept as missing)", len(agents))
	}
	beta := agents[1]
	if beta.ID != "beta" || beta.Found {
		t.Fatalf("agents[1] = %q found=%v, want beta found=false", beta.ID, beta.Found)
	}
	if beta.BinaryPath != "" || beta.Version != "" {
		t.Errorf("missing agent bin=%q version=%q, want both empty (no version exec)", beta.BinaryPath, beta.Version)
	}
	if beta.Config.Global == "" {
		t.Errorf("missing agent config global empty, want resolved from catalog")
	}
}

func TestDetectWithDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	alphaBin := writeStub(t, dir, "alpha", "alpha 1.0.0")
	betaBin := writeStub(t, dir, "beta", "beta 2.0.0")

	cat := &Catalog{Agents: map[string]KnownAgent{
		"alpha": {Name: "Alpha", Bin: "alpha", Config: PathPair{Global: "~/.alpha"}},
		"beta":  {Name: "Beta", Bin: "beta", Config: PathPair{Global: "~/.beta"}},
	}}

	agents, err := Detect(context.Background(),
		WithCatalog(cat),
		WithBinPaths(map[string]string{"alpha": alphaBin, "beta": betaBin}),
		WithDisabled("beta"),
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "alpha" {
		t.Fatalf("got %v, want only alpha (beta disabled)", agents)
	}
}

func TestPresenceViaSearchDirAndPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	searchDir := t.TempDir()
	writeStub(t, searchDir, "viasearch", "x 1")

	pathDir := t.TempDir()
	writeStub(t, pathDir, "viapath", "x 1")
	t.Setenv("PATH", pathDir)

	cat := &Catalog{Agents: map[string]KnownAgent{
		"viasearch": {Name: "Via Search", Bin: "viasearch", Config: PathPair{Global: "~/.s"}},
		"viapath":   {Name: "Via Path", Bin: "viapath", Config: PathPair{Global: "~/.p"}},
	}}

	agents, err := Detect(context.Background(), WithCatalog(cat), WithSearchDirs(searchDir))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2 (one via search dir, one via PATH)", len(agents))
	}
}

func TestPresencePathWinsOverSearchDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// The same binary name exists on PATH and in a search dir. PATH is consulted
	// before search dirs, so the PATH copy is the one located.
	pathDir := t.TempDir()
	pathBin := writeStub(t, pathDir, "dup", "path 1.0.0")
	t.Setenv("PATH", pathDir)

	searchDir := t.TempDir()
	writeStub(t, searchDir, "dup", "search 2.0.0")

	cat := &Catalog{Agents: map[string]KnownAgent{
		"dup": {Name: "Dup", Bin: "dup", Config: PathPair{Global: "~/.dup"}},
	}}
	a, found, err := DetectOne(context.Background(), "dup", WithCatalog(cat), WithSearchDirs(searchDir))
	if err != nil {
		t.Fatalf("DetectOne: %v", err)
	}
	if !found || a.BinaryPath != pathBin {
		t.Errorf("BinaryPath = %q, want PATH copy %q (PATH wins over search dir)", a.BinaryPath, pathBin)
	}
}

func TestVersionExecAndSkip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	stdoutBin := writeStub(t, dir, "vstdout", "myagent v1.2.3")
	// A binary that prints its version to stderr, exercising combined-output capture.
	stderrBin := filepath.Join(dir, "vstderr")
	if err := os.WriteFile(stderrBin, []byte("#!/bin/sh\necho 'tool 9.9.9' >&2\n"), 0o755); err != nil {
		t.Fatalf("write stderr stub: %v", err)
	}

	cat := &Catalog{Agents: map[string]KnownAgent{
		"vstdout": {Name: "V Stdout", Bin: "vstdout", Config: PathPair{Global: "~/.v"},
			Version: ptr(VersionProbe{Args: []string{"--version"}, Pattern: `v([0-9.]+)`})},
		"vstderr": {Name: "V Stderr", Bin: "vstderr", Config: PathPair{Global: "~/.v"},
			Version: ptr(VersionProbe{Args: []string{"--version"}, Pattern: `([0-9]+\.[0-9]+\.[0-9]+)`})},
	}}
	binPaths := map[string]string{"vstdout": stdoutBin, "vstderr": stderrBin}

	agents, err := Detect(context.Background(), WithCatalog(cat), WithBinPaths(binPaths))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	got := map[string]string{}
	for _, a := range agents {
		got[a.ID] = a.Version
	}
	if got["vstdout"] != "1.2.3" {
		t.Errorf("vstdout version = %q, want 1.2.3", got["vstdout"])
	}
	if got["vstderr"] != "9.9.9" {
		t.Errorf("vstderr version = %q, want 9.9.9 (stderr captured)", got["vstderr"])
	}

	// WithSkipVersion performs no exec and leaves Version empty.
	skipped, err := Detect(context.Background(), WithCatalog(cat), WithBinPaths(binPaths), WithSkipVersion())
	if err != nil {
		t.Fatalf("Detect skip: %v", err)
	}
	for _, a := range skipped {
		if a.Version != "" {
			t.Errorf("%s version = %q under WithSkipVersion, want empty", a.ID, a.Version)
		}
	}
}

func TestDetectOnePopulatesNotInstalled(t *testing.T) {
	home := t.TempDir()
	wd := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(wd)

	// Global config exists, local config does not; skills global exists, local missing.
	mustMkdir(t, filepath.Join(home, ".myagent"))
	mustMkdir(t, filepath.Join(home, ".myagent", "skills"))

	cat := &Catalog{Agents: map[string]KnownAgent{
		"myagent": {
			Name:   "My Agent",
			Bin:    "myagent-absent",
			Config: PathPair{Global: "~/.myagent", Local: ".myagent"},
			Skills: ptr(PathPair{Global: "~/.myagent/skills", Local: ".myagent/skills"}),
		},
	}}

	a, found, err := DetectOne(context.Background(), "myagent", WithCatalog(cat))
	if err != nil {
		t.Fatalf("DetectOne: %v", err)
	}
	if found || a.Found {
		t.Errorf("Found = %v, want false (binary not installed)", a.Found)
	}
	if a.Config.Global != filepath.Join(home, ".myagent") || !a.Config.GlobalExists {
		t.Errorf("config global = %q exists=%v", a.Config.Global, a.Config.GlobalExists)
	}
	if a.Config.Local != filepath.Join(wd, ".myagent") || a.Config.LocalExists {
		t.Errorf("config local = %q exists=%v, want %q exists=false", a.Config.Local, a.Config.LocalExists, filepath.Join(wd, ".myagent"))
	}
	if a.Skills.Global != filepath.Join(home, ".myagent", "skills") || !a.Skills.GlobalExists {
		t.Errorf("skills global = %q exists=%v", a.Skills.Global, a.Skills.GlobalExists)
	}
	if a.Skills.LocalExists {
		t.Errorf("skills local exists = true, want false")
	}
}

func TestDetectOneNoSkillsAndNoLocalConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cat := &Catalog{Agents: map[string]KnownAgent{
		"bare": {Name: "Bare", Bin: "bare-absent", Config: PathPair{Global: "~/.bare"}},
	}}
	a, _, err := DetectOne(context.Background(), "bare", WithCatalog(cat))
	if err != nil {
		t.Fatalf("DetectOne: %v", err)
	}
	if a.Skills != (ResolvedPaths{}) {
		t.Errorf("skills = %+v, want zero value (no skills concept)", a.Skills)
	}
	if a.Config.Local != "" || a.Config.LocalExists {
		t.Errorf("config local = %q exists=%v, want empty/false", a.Config.Local, a.Config.LocalExists)
	}
}

func TestDetectOneEnvExpansion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mustMkdir(t, filepath.Join(home, ".envagent"))

	cat := &Catalog{Agents: map[string]KnownAgent{
		"envagent": {Name: "Env Agent", Bin: "absent", Config: PathPair{Global: "$HOME/.envagent"}},
	}}
	a, _, err := DetectOne(context.Background(), "envagent", WithCatalog(cat))
	if err != nil {
		t.Fatalf("DetectOne: %v", err)
	}
	if a.Config.Global != filepath.Join(home, ".envagent") || !a.Config.GlobalExists {
		t.Errorf("config global = %q exists=%v, want expanded and existing", a.Config.Global, a.Config.GlobalExists)
	}
}

func TestDetectOneUnknownAgent(t *testing.T) {
	cat := &Catalog{Agents: map[string]KnownAgent{}}
	_, _, err := DetectOne(context.Background(), "nope", WithCatalog(cat))
	if !errors.Is(err, ErrAgentUnknown) {
		t.Errorf("err = %v, want ErrAgentUnknown", err)
	}
}

func TestBinPathOverrideUsedForVersionExec(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// The override path, not PATH, supplies the binary that the version exec runs.
	dir := t.TempDir()
	overrideBin := writeStub(t, dir, "realbin", "override 4.5.6")

	pathDir := t.TempDir()
	writeStub(t, pathDir, "claimed", "path 0.0.1")
	t.Setenv("PATH", pathDir)

	cat := &Catalog{Agents: map[string]KnownAgent{
		"agent": {Name: "Agent", Bin: "claimed", Config: PathPair{Global: "~/.a"},
			Version: ptr(VersionProbe{Args: []string{"--version"}, Pattern: `([0-9.]+)`})},
	}}
	a, _, err := DetectOne(context.Background(), "agent", WithCatalog(cat),
		WithBinPaths(map[string]string{"agent": overrideBin}))
	if err != nil {
		t.Fatalf("DetectOne: %v", err)
	}
	if a.BinaryPath != overrideBin {
		t.Errorf("BinaryPath = %q, want override %q", a.BinaryPath, overrideBin)
	}
	if a.Version != "4.5.6" {
		t.Errorf("version = %q, want 4.5.6 (from override binary)", a.Version)
	}
}

func TestBinPathOverrideMissingReportsNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// An override pointing at a path that does not exist is the sole candidate and
	// is verified like any other: Found is false, not a blind assertion that the
	// binary is present. It does not fall through to a same-named binary on PATH.
	pathDir := t.TempDir()
	writeStub(t, pathDir, "claimed", "path 0.0.1")
	t.Setenv("PATH", pathDir)

	cat := &Catalog{Agents: map[string]KnownAgent{
		"agent": {Name: "Agent", Bin: "claimed", Config: PathPair{Global: "~/.a"}},
	}}
	a, found, err := DetectOne(context.Background(), "agent", WithCatalog(cat),
		WithBinPaths(map[string]string{"agent": filepath.Join(t.TempDir(), "does-not-exist")}))
	if err != nil {
		t.Fatalf("DetectOne: %v", err)
	}
	if found || a.Found {
		t.Errorf("Found = %v, want false for a non-existent override path", a.Found)
	}
	if a.BinaryPath != "" {
		t.Errorf("BinaryPath = %q, want empty for a non-existent override", a.BinaryPath)
	}
}

func TestLoadCatalogPreloadedBypassesRegistry(t *testing.T) {
	// With a preloaded catalog, LoadCatalog returns it verbatim, never stale, and
	// constructs no registry — the offline path callers rely on.
	cat := &Catalog{Agents: map[string]KnownAgent{
		"agent": {Name: "Agent", Bin: "agent", Config: PathPair{Global: "~/.a"}},
	}}
	got, stale, err := LoadCatalog(context.Background(), WithCatalog(cat))
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if got != cat {
		t.Errorf("returned catalog = %p, want preloaded %p", got, cat)
	}
	if stale {
		t.Error("stale = true, want false for a preloaded catalog")
	}
}

func TestDetectHonoursCancelledContextOffline(t *testing.T) {
	// A fully offline run — no models client, version skipped — must still honour a
	// cancelled context rather than return a falsely-complete result. Nothing in
	// presence or path resolution touches the context, so the engine checks it
	// explicitly.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	bin := writeStub(t, dir, "agent", "agent 1.0.0")
	cat := &Catalog{Agents: map[string]KnownAgent{
		"agent": {Name: "Agent", Bin: "agent", Config: PathPair{Global: "~/.a"}},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Detect(ctx, WithCatalog(cat),
		WithBinPaths(map[string]string{"agent": bin}), WithSkipVersion()); !errors.Is(err, context.Canceled) {
		t.Errorf("Detect err = %v, want context.Canceled", err)
	}
	if _, _, err := DetectOne(ctx, "agent", WithCatalog(cat),
		WithBinPaths(map[string]string{"agent": bin}), WithSkipVersion()); !errors.Is(err, context.Canceled) {
		t.Errorf("DetectOne err = %v, want context.Canceled", err)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}
