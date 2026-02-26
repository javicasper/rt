package main

import (
	"encoding/gob"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func cacheFilePath() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return filepath.Join(cache, "rt", "filters.gob")
}

type filterCache struct {
	Filters []Filter
	BuiltAt time.Time
}

// Register Filter types for gob encoding.
func init() {
	gob.Register(filterCache{})
}

// loadFiltersWithCache tries the gob cache first, falls back to parsing TOMLs.
func loadFiltersWithCache() ([]Filter, error) {
	cachePath := cacheFilePath()

	if filters, err := readCache(cachePath); err == nil {
		return filters, nil
	}

	// Cache miss or stale — load from source
	filters, err := loadAllFilters()
	if err != nil {
		return nil, err
	}

	// Write cache (best-effort)
	_ = writeCache(cachePath, filters)
	return filters, nil
}

func readCache(path string) ([]Filter, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	// Check if any user filter is newer than cache
	if isUserDirNewer(info.ModTime()) {
		return nil, fmt.Errorf("cache stale")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var c filterCache
	if err := gob.NewDecoder(f).Decode(&c); err != nil {
		return nil, err
	}
	return c.Filters, nil
}

func writeCache(path string, filters []Filter) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return gob.NewEncoder(f).Encode(filterCache{
		Filters: filters,
		BuiltAt: time.Now(),
	})
}

func clearCache() error {
	return os.Remove(cacheFilePath())
}

func isUserDirNewer(cacheTime time.Time) bool {
	dir := userFilterDir()
	newer := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".toml") {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(cacheTime) {
			newer = true
			return fs.SkipAll
		}
		return nil
	})
	return newer
}
