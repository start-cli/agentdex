// Package config loads and validates the agentdex user configuration
// (config.cue), resolves the XDG paths it lives under, resolves the per-cache
// TTLs, and maps the configuration together with the global flags into the
// agentdex library options and the modelsdev client options. It is the one place
// where config.cue and flags are read; the library and the modelsdev client stay
// configuration-agnostic and option-driven.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "embed"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

// DefaultTTL is the built-in cache TTL applied when neither a section ttl nor
// cache_ttl is configured. It mirrors the library defaults so the resolved value
// is explicit at the call site rather than left implicit in two packages.
const DefaultTTL = 24 * time.Hour

// ErrConfig marks a configuration fault: a malformed config.cue, a value that
// violates the schema, or an unparseable duration. The CLI maps it to exit 78.
var ErrConfig = errors.New("invalid configuration")

//go:embed schema.cue
var schemaSrc string

// Config is the resolved, typed configuration: defaults applied, durations
// parsed into the per-cache TTLs. It is decoupled from config.cue's wire shape so
// callers never re-parse strings.
type Config struct {
	CatalogModule string
	CatalogTTL    time.Duration
	ModelsURL     string
	ModelsTTL     time.Duration
	SearchDirs    []string
	BinPaths      map[string]string
	Disabled      []string
	EnrichModels  bool
	Color         string
}

// raw mirrors config.cue's wire shape for decoding. The json tags carry the
// snake_case field names CUE decodes against.
type raw struct {
	CacheTTL string `json:"cache_ttl"`
	Catalog  struct {
		Module string `json:"module"`
		TTL    string `json:"ttl"`
	} `json:"catalog"`
	Models struct {
		URL string `json:"url"`
		TTL string `json:"ttl"`
	} `json:"models"`
	SearchDirs     []string          `json:"search_dirs"`
	BinPaths       map[string]string `json:"bin_paths"`
	DisabledAgents []string          `json:"disabled_agents"`
	EnrichModels   bool              `json:"enrich_models"`
	Color          string            `json:"color"`
}

// Path resolves the config.cue location from $XDG_CONFIG_HOME with the documented
// home fallback, mirroring how the library resolves XDG paths rather than using a
// platform-specific user-dir helper. It returns "" only when neither XDG_CONFIG_HOME
// nor a home directory can be determined.
func Path() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "agentdex", "config.cue")
	}
	if home := homeDir(); home != "" {
		return filepath.Join(home, ".config", "agentdex", "config.cue")
	}
	return ""
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

// Load reads, validates, and resolves config.cue at path. A missing file is a
// clean, empty configuration: the schema defaults still materialise. A syntax
// error, a schema violation, or an unparseable duration is an ErrConfig.
func Load(path string) (*Config, error) {
	var data []byte
	if path != "" {
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			data = b
		case errors.Is(err, os.ErrNotExist):
			// A missing config is empty; defaults apply.
		default:
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	r, err := decode(data, path)
	if err != nil {
		return nil, err
	}
	return resolve(r)
}

// decode compiles the config bytes against the closed #Config schema, validating
// types and rejecting unknown fields, and decodes the result with defaults
// applied. Empty bytes yield the all-defaults configuration.
func decode(data []byte, filename string) (*raw, error) {
	cuectx := cuecontext.New()
	schema := cuectx.CompileString(schemaSrc, cue.Filename("schema.cue"))
	if err := schema.Err(); err != nil {
		// A broken embedded schema is a programmer error, not a user config fault.
		return nil, fmt.Errorf("compile config schema: %w", err)
	}
	def := schema.LookupPath(cue.ParsePath("#Config"))

	user := cuectx.CompileBytes(data, cue.Filename(filenameOr(filename)))
	if err := user.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}

	unified := def.Unify(user)
	if err := unified.Validate(cue.Concrete(false), cue.All()); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}

	var r raw
	if err := unified.Decode(&r); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	return &r, nil
}

func filenameOr(name string) string {
	if name == "" {
		return "config.cue"
	}
	return name
}

// resolve turns the decoded wire shape into the typed Config: it parses the
// per-cache TTLs under the section-then-global-then-default rule and copies the
// remaining fields through.
func resolve(r *raw) (*Config, error) {
	catalogTTL, err := resolveTTL(r.Catalog.TTL, r.CacheTTL)
	if err != nil {
		return nil, err
	}
	modelsTTL, err := resolveTTL(r.Models.TTL, r.CacheTTL)
	if err != nil {
		return nil, err
	}
	return &Config{
		CatalogModule: r.Catalog.Module,
		CatalogTTL:    catalogTTL,
		ModelsURL:     r.Models.URL,
		ModelsTTL:     modelsTTL,
		SearchDirs:    r.SearchDirs,
		BinPaths:      r.BinPaths,
		Disabled:      r.DisabledAgents,
		EnrichModels:  r.EnrichModels,
		Color:         r.Color,
	}, nil
}

// resolveTTL applies the per-cache rule: the section ttl, then cache_ttl, then the
// built-in default. An unparseable duration at either level is an ErrConfig.
func resolveTTL(section, global string) (time.Duration, error) {
	for _, s := range []string{section, global} {
		if s == "" {
			continue
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("%w: invalid duration %q: %w", ErrConfig, s, err)
		}
		return d, nil
	}
	return DefaultTTL, nil
}
