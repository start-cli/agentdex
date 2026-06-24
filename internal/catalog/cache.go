package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// resolutionCache persists the resolved version of a catalog module under the
// cache directory, one file per module path so resolutions for different module
// paths are fully independent — there is no shared slot to collide in, and the
// resolution cached for one module is never served for another. This is
// version-resolution caching layered over CUE's own module content cache, not a
// snapshot of the catalog data.
type resolutionCache struct {
	dir string
}

// resolution records the version resolved for a module path and when it was
// resolved, so the loader can apply the TTL and keep-last-resolved behaviour.
type resolution struct {
	ModulePath string    `json:"module_path"`
	Version    string    `json:"version"`
	ResolvedAt time.Time `json:"resolved_at"`
}

func newResolutionCache(dir string) *resolutionCache {
	return &resolutionCache{dir: dir}
}

// fresh reports whether the resolution is still within the TTL relative to now.
func (r resolution) fresh(now time.Time, ttl time.Duration) bool {
	return now.Sub(r.ResolvedAt) < ttl
}

// path returns the cache file for a module path. The name is a hash of the
// module path so registry coordinates (slashes, @) are filesystem-safe and each
// module path maps to its own distinct file.
func (c *resolutionCache) path(modulePath string) string {
	sum := sha256.Sum256([]byte(modulePath))
	return filepath.Join(c.dir, "catalog-resolution-"+hex.EncodeToString(sum[:8])+".json")
}

// read returns the cached resolution for modulePath. The bool is false when no
// resolution is cached. A missing file is not an error.
func (c *resolutionCache) read(modulePath string) (resolution, bool, error) {
	data, err := os.ReadFile(c.path(modulePath))
	if errors.Is(err, fs.ErrNotExist) {
		return resolution{}, false, nil
	}
	if err != nil {
		return resolution{}, false, fmt.Errorf("read resolution cache: %w", err)
	}
	var res resolution
	if err := json.Unmarshal(data, &res); err != nil {
		// A corrupt entry is treated as absent rather than fatal: re-resolution
		// will overwrite it.
		return resolution{}, false, nil
	}
	// Guard against a hash collision serving another module's resolution.
	if res.ModulePath != modulePath {
		return resolution{}, false, nil
	}
	return res, true, nil
}

// write persists a resolution, creating the cache directory if needed. It writes
// to a uniquely-named temp file in the same directory and renames it over the
// target, so a concurrent reader sees either the old or the new file, never a
// torn one. fsync is intentionally skipped: the resolution cache is a
// regenerable optimization, so a write lost to a crash costs one re-resolution,
// not data, and is not worth the durability cost.
func (c *resolutionCache) write(res resolution) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	data, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("marshal resolution: %w", err)
	}

	tmp, err := os.CreateTemp(c.dir, "catalog-resolution-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp resolution file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // a no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp resolution file: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set temp resolution permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp resolution file: %w", err)
	}
	if err := os.Rename(tmpName, c.path(res.ModulePath)); err != nil {
		return fmt.Errorf("rename resolution cache into place: %w", err)
	}
	return nil
}
