package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed filters
var embeddedFilters embed.FS

// Filter represents a parsed TOML filter definition.
type Filter struct {
	Command      StringOrSlice     `toml:"command"`
	Run          string            `toml:"run"`
	StripAnsi    bool              `toml:"strip_ansi"`
	Skip         []string          `toml:"skip"`
	Keep         []string          `toml:"keep"`
	Replace      []ReplaceRule     `toml:"replace"`
	MatchOutput  []MatchOutputRule `toml:"match_output"`
	OnSuccess    *OutputBlock      `toml:"on_success"`
	OnFailure    *OutputBlock      `toml:"on_failure"`
	Variants     []Variant         `toml:"variant"`

	// Metadata (not from TOML)
	Name   string `toml:"-"`
	Source string `toml:"-"` // "built-in" or "user"
	Path   string `toml:"-"`
}

type ReplaceRule struct {
	Pattern string `toml:"pattern"`
	Output  string `toml:"output"`
}

type MatchOutputRule struct {
	Contains string `toml:"contains"`
	Matches  string `toml:"matches"`
	Output   string `toml:"output"`
}

type OutputBlock struct {
	Output string   `toml:"output"`
	Head   int      `toml:"head"`
	Tail   int      `toml:"tail"`
	Skip   []string `toml:"skip"`
}

type Variant struct {
	Name   string        `toml:"name"`
	Detect VariantDetect `toml:"detect"`
	Filter string        `toml:"filter"`
}

type VariantDetect struct {
	Files []string `toml:"files"`
}

// StringOrSlice handles TOML fields that can be a string or []string.
type StringOrSlice []string

func (s *StringOrSlice) UnmarshalTOML(data interface{}) error {
	switch v := data.(type) {
	case string:
		*s = []string{v}
	case []interface{}:
		for _, item := range v {
			str, ok := item.(string)
			if !ok {
				return fmt.Errorf("expected string in array, got %T", item)
			}
			*s = append(*s, str)
		}
	default:
		return fmt.Errorf("expected string or array, got %T", data)
	}
	return nil
}

func userFilterDir() string {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".config", "rt", "filters")
	}
	return filepath.Join(cfg, "rt", "filters")
}

// loadAllFilters loads filters from user dir and built-in, user takes precedence.
func loadAllFilters() ([]Filter, error) {
	byName := make(map[string]Filter)

	// Load built-in filters first
	if err := loadFiltersFromFS(embeddedFilters, "filters", "built-in", byName); err != nil {
		return nil, fmt.Errorf("loading built-in filters: %w", err)
	}

	// Load user filters (overrides built-in)
	userDir := userFilterDir()
	if info, err := os.Stat(userDir); err == nil && info.IsDir() {
		if err := loadFiltersFromDisk(userDir, "user", byName); err != nil {
			return nil, fmt.Errorf("loading user filters: %w", err)
		}
	}

	filters := make([]Filter, 0, len(byName))
	for _, f := range byName {
		filters = append(filters, f)
	}
	sort.Slice(filters, func(i, j int) bool {
		return filters[i].Name < filters[j].Name
	})
	return filters, nil
}

func loadFiltersFromFS(fsys fs.FS, root, source string, out map[string]Filter) error {
	return fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".toml") {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		name := toFilterName(strings.TrimPrefix(path, root+"/"))
		f, err := parseFilter(data, name, source, path)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		out[name] = f
		return nil
	})
}

func loadFiltersFromDisk(dir, source string, out map[string]Filter) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".toml") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		name := toFilterName(rel)
		f, err := parseFilter(data, name, source, path)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		out[name] = f
		return nil
	})
}

func parseFilter(data []byte, name, source, path string) (Filter, error) {
	var f Filter
	if err := toml.Unmarshal(data, &f); err != nil {
		return f, err
	}
	f.Name = name
	f.Source = source
	f.Path = path
	return f, nil
}

// toFilterName converts "git/status.toml" to "git/status"
func toFilterName(path string) string {
	path = strings.TrimSuffix(path, ".toml")
	return filepath.ToSlash(path)
}

// matchFilter finds the best filter for a command string.
func matchFilter(filters []Filter, cmdStr string) *Filter {
	var best *Filter
	bestScore := -1

	for i := range filters {
		for _, pattern := range filters[i].Command {
			score := matchScore(pattern, cmdStr)
			if score > bestScore {
				bestScore = score
				best = &filters[i]
			}
		}
	}
	return best
}

// matchScore returns how well a pattern matches a command.
// -1 means no match. Higher is more specific.
func matchScore(pattern, cmd string) int {
	patParts := strings.Fields(pattern)
	cmdParts := strings.Fields(cmd)

	if len(patParts) == 0 || len(cmdParts) < len(patParts) {
		return -1
	}

	score := 0
	for i, pp := range patParts {
		if pp == "*" {
			// wildcard matches any remaining args
			score += 1
		} else if pp == cmdParts[i] {
			score += 10
		} else {
			return -1
		}
	}
	return score
}
