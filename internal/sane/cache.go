package sane

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// DefaultCacheTTL is how long cached scanner devices and capabilities are
// considered fresh before a startup background re-probe is triggered.
const DefaultCacheTTL = 24 * time.Hour

// ScannerCache stores discovered SANE devices and their per-device capabilities.
type ScannerCache struct {
	UpdatedAt    time.Time               `json:"updated_at"`
	Devices      []Device                `json:"devices"`
	Capabilities map[string]Capabilities `json:"capabilities"`
}

// IsFresh reports whether the cache has at least one device and was updated within ttl of now.
func (c ScannerCache) IsFresh(ttl time.Duration, now time.Time) bool {
	if len(c.Devices) == 0 || c.UpdatedAt.IsZero() {
		return false
	}
	if ttl <= 0 {
		return false
	}
	return now.Sub(c.UpdatedAt) < ttl
}

// DefaultCachePath returns the default file path for caching scanner discovery
// (typically ~/.cache/lazyprint/scanners.json).
func DefaultCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil || home == "" {
			return ""
		}
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "lazyprint", "scanners.json")
}

// LoadCache reads and decodes the scanner cache from the given file path.
func LoadCache(path string) (ScannerCache, error) {
	if path == "" {
		return ScannerCache{}, os.ErrNotExist
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ScannerCache{}, err
	}
	var cache ScannerCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return ScannerCache{}, err
	}
	if cache.Capabilities == nil {
		cache.Capabilities = make(map[string]Capabilities)
	}
	return cache, nil
}

// SaveCache atomically writes the scanner cache to the given file path.
func SaveCache(path string, cache ScannerCache) error {
	if path == "" {
		return nil
	}
	if cache.Capabilities == nil {
		cache.Capabilities = make(map[string]Capabilities)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
