package catalog

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/module"
	"golang.org/x/mod/semver"
)

// Registry is the loader's boundary to the CUE module registry. It is the test
// seam: production wires the modconfig-backed implementation, tests substitute
// a stub serving a fixture source directory and canned outcomes. Nondeterministic
// network access lives entirely behind this interface.
type Registry interface {
	// ResolveLatestVersion maps a major-version module path (…/catalog@v1) to a
	// canonical version (…/catalog@v1.0.3). Requires the network.
	ResolveLatestVersion(ctx context.Context, modulePath string) (string, error)

	// Fetch returns the on-disk source directory for a canonical module@version.
	// Served from CUE's content cache offline once previously fetched.
	Fetch(ctx context.Context, modulePath string) (sourceDir string, err error)
}

// modconfigRegistry is the production Registry, backed by modconfig. It honours
// CUE_REGISTRY and cue login with no agentdex-specific auth settings.
type modconfigRegistry struct {
	reg modconfig.Registry
}

// NewRegistry constructs the production registry client. modconfig.NewRegistry
// configures itself from the environment (CUE_REGISTRY, cue login, the standard
// CUE cache directory).
func NewRegistry() (Registry, error) {
	reg, err := modconfig.NewRegistry(nil)
	if err != nil {
		return nil, fmt.Errorf("construct registry: %w", err)
	}
	return &modconfigRegistry{reg: reg}, nil
}

func (r *modconfigRegistry) ResolveLatestVersion(ctx context.Context, modulePath string) (string, error) {
	versions, err := r.reg.ModuleVersions(ctx, modulePath)
	if err != nil {
		return "", fmt.Errorf("resolve versions for %s: %w", modulePath, err)
	}
	latest, err := latestVersion(versions)
	if err != nil {
		return "", fmt.Errorf("%s: %w", modulePath, err)
	}
	return latest, nil
}

// latestVersion selects the version to load from those published. It prefers the
// highest stable release, mirroring Go's @latest semantics so a published
// pre-release never silently becomes the resolved version. It falls back to the
// highest pre-release only when no stable release exists, so a registry that has
// published only a release candidate still resolves. Invalid version strings are
// ignored.
func latestVersion(versions []string) (string, error) {
	var stable, latest string
	for _, v := range versions {
		if !semver.IsValid(v) {
			continue
		}
		if latest == "" || semver.Compare(v, latest) > 0 {
			latest = v
		}
		if semver.Prerelease(v) == "" && (stable == "" || semver.Compare(v, stable) > 0) {
			stable = v
		}
	}
	if stable != "" {
		return stable, nil
	}
	if latest != "" {
		return latest, nil
	}
	return "", errors.New("no valid versions published")
}

func (r *modconfigRegistry) Fetch(ctx context.Context, modulePath string) (string, error) {
	v, err := module.ParseVersion(modulePath)
	if err != nil {
		return "", fmt.Errorf("parse canonical module path %q: %w", modulePath, err)
	}
	loc, err := r.reg.Fetch(ctx, v)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", modulePath, err)
	}
	rootFS, ok := loc.FS.(module.OSRootFS)
	if !ok {
		return "", fmt.Errorf("fetch %s: registry source is not backed by the filesystem", modulePath)
	}
	root := rootFS.OSRoot()
	if root == "" {
		return "", fmt.Errorf("fetch %s: registry source has no on-disk path", modulePath)
	}
	return filepath.Join(root, loc.Dir), nil
}

// canonicalModulePath joins a major-version base path (…/catalog@v1) with a
// resolved canonical version (v1.0.3) into the …/catalog@v1.0.3 form Fetch wants.
func canonicalModulePath(basePath, version string) (string, error) {
	v, err := module.NewVersion(basePath, version)
	if err != nil {
		return "", fmt.Errorf("form canonical path for %s@%s: %w", basePath, version, err)
	}
	return v.String(), nil
}
