package config

import (
	"github.com/start-cli/agentdex"
)

// Flags carries the global flag values that feed into option mapping.
// SearchDirs is the repeated values from --search-dir; BinPaths the parsed
// id=path map from --bin-path. Both merge with their config.cue counterparts,
// with the flag values taking precedence on a key collision.
type Flags struct {
	SearchDirs []string
	BinPaths   map[string]string
}

// Options builds the agentdex.Open options from the resolved configuration and
// the global flags. The models.dev client is constructed inside the library, so
// this maps the catalog source, cache, and models.dev settings into Open options
// rather than building a client; force-refresh is owned by Index.Refresh. When
// catalog.dir is set it is passed alongside catalog.module and wins in the
// library, so a working-tree catalog is loaded without a registry (R11).
func (c *Config) Options(f Flags) []agentdex.Option {
	opts := []agentdex.Option{
		agentdex.WithCatalogModule(c.CatalogModule),
		agentdex.WithCatalogTTL(c.CatalogTTL),
		agentdex.WithModelsTTL(c.ModelsTTL),
	}
	if c.CatalogDir != "" {
		opts = append(opts, agentdex.WithCatalogDir(c.CatalogDir))
	}
	if c.ModelsURL != "" {
		opts = append(opts, agentdex.WithModelsURL(c.ModelsURL))
	}
	if dirs := mergeSlices(c.SearchDirs, f.SearchDirs); len(dirs) > 0 {
		opts = append(opts, agentdex.WithSearchDirs(dirs...))
	}
	if bin := mergeBinPaths(c.BinPaths, f.BinPaths); len(bin) > 0 {
		opts = append(opts, agentdex.WithBinPaths(bin))
	}
	return opts
}

// mergeSlices concatenates config values then flag values, preserving order and
// dropping exact duplicates so a value given in both places is not searched twice.
func mergeSlices(cfg, flags []string) []string {
	out := make([]string, 0, len(cfg)+len(flags))
	seen := make(map[string]struct{}, len(cfg)+len(flags))
	for _, group := range [][]string{cfg, flags} {
		for _, v := range group {
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

// mergeBinPaths overlays flag overrides onto the config map, so an id given on
// both the command line and in config.cue takes the command-line path.
func mergeBinPaths(cfg, flags map[string]string) map[string]string {
	if len(cfg) == 0 && len(flags) == 0 {
		return nil
	}
	out := make(map[string]string, len(cfg)+len(flags))
	for id, p := range cfg {
		out[id] = p
	}
	for id, p := range flags {
		out[id] = p
	}
	return out
}
