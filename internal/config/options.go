package config

import (
	"time"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/modelsdev"
)

// Flags carries the global flag values that feed into option mapping.
// SearchDirs is the repeated values from --search-dir; BinPaths the parsed
// id=path map from --bin-path. Both merge with their config.cue counterparts,
// with the flag values taking precedence on a key collision.
type Flags struct {
	SearchDirs []string
	BinPaths   map[string]string
}

// CatalogOptions builds the catalog-source options at the given TTL. Detection
// commands pass the resolved catalog TTL; refresh passes 0 to force
// re-resolution past the cache.
func (c *Config) CatalogOptions(ttl time.Duration) []agentdex.Option {
	return []agentdex.Option{
		agentdex.WithCatalogModule(c.CatalogModule),
		agentdex.WithCatalogTTL(ttl),
	}
}

// LibraryOptions builds the agentdex options for a detection run from config and
// flags. It omits the models option: whether to attach a client, and whether to
// enrich, is a per-command policy the caller composes on top with WithModels.
func (c *Config) LibraryOptions(f Flags) []agentdex.Option {
	opts := c.CatalogOptions(c.CatalogTTL)

	if dirs := mergeSlices(c.SearchDirs, f.SearchDirs); len(dirs) > 0 {
		opts = append(opts, agentdex.WithSearchDirs(dirs...))
	}
	if bin := mergeBinPaths(c.BinPaths, f.BinPaths); len(bin) > 0 {
		opts = append(opts, agentdex.WithBinPaths(bin))
	}
	if len(c.Disabled) > 0 {
		opts = append(opts, agentdex.WithDisabled(c.Disabled...))
	}
	return opts
}

// ModelsClient constructs the modelsdev client from config: the optional URL
// override and the resolved models TTL. The cache directory is left to the
// client's own XDG default so it agrees with the catalog cache location.
func (c *Config) ModelsClient() *modelsdev.Client {
	return c.modelsClient()
}

// ForceRefreshModelsClient is ModelsClient in force-refresh mode: the next fetch
// goes to the network regardless of the cache and reports a failure rather than
// serving stale, so an explicit refresh is honest about whether it fetched.
func (c *Config) ForceRefreshModelsClient() *modelsdev.Client {
	return c.modelsClient(modelsdev.WithForceRefresh())
}

func (c *Config) modelsClient(extra ...modelsdev.ClientOption) *modelsdev.Client {
	opts := append([]modelsdev.ClientOption{modelsdev.WithTTL(c.ModelsTTL)}, extra...)
	if c.ModelsURL != "" {
		opts = append(opts, modelsdev.WithURL(c.ModelsURL))
	}
	return modelsdev.New(opts...)
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
