package modelsdev

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// cacheFileName is the plain-JSON stale cache of the fetched catalog.json. It is
// distinct from the CUE version-resolution cache the agent catalog uses.
const cacheFileName = "catalog-modelsdev.json"

// cache is the on-disk stale cache for the fetched catalog.json bytes. It stores
// the upstream JSON verbatim and relies on the file's mtime for TTL, keeping the
// cache a plain copy of catalog.json rather than a wrapped snapshot.
type cache struct {
	dir string
}

// read returns the cached bytes and their modification time. ok is false when no
// cache file exists or it cannot be read; the cache is a best-effort optimization,
// so an unreadable entry is treated as absent rather than fatal.
func (c cache) read() (data []byte, modTime time.Time, ok bool) {
	path := filepath.Join(c.dir, cacheFileName)
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, false
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, false
	}
	return data, info.ModTime(), true
}

// write persists the catalog bytes, creating the cache directory if needed. It
// writes to a uniquely-named temp file in the same directory and renames it over
// the target, so a concurrent reader sees either the old or the new file, never a
// torn one. fsync is skipped: the cache is regenerable, so a write lost to a
// crash costs one re-fetch, not data.
func (c cache) write(data []byte) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	tmp, err := os.CreateTemp(c.dir, cacheFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp cache file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // a no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp cache file: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set temp cache permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp cache file: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(c.dir, cacheFileName)); err != nil {
		return fmt.Errorf("rename cache into place: %w", err)
	}
	return nil
}

// defaultCacheDir resolves $XDG_CACHE_HOME/agentdex, falling back to
// ~/.cache/agentdex, then to a relative path if neither is available. It mirrors
// the agent catalog loader's resolution; this leaf package cannot import it.
func defaultCacheDir() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "agentdex")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "agentdex")
	}
	return filepath.Join(".cache", "agentdex")
}
