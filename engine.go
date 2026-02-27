package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// applyFilter processes raw output through a filter and returns the filtered result.
func applyFilter(f *Filter, raw string, exitCode int) string {
	// Check match_output rules first (short-circuit)
	for _, rule := range f.MatchOutput {
		if rule.Contains != "" && strings.Contains(raw, rule.Contains) {
			return rule.Output
		}
		if rule.Matches != "" {
			if re, err := compileRegex(rule.Matches); err == nil && re.MatchString(raw) {
				return rule.Output
			}
		}
	}

	lines := strings.Split(raw, "\n")

	// Remove trailing empty line from split
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Apply skip rules
	if len(f.Skip) > 0 {
		lines = applySkip(lines, f.Skip)
	}

	// Apply keep rules (allowlist — only retain matching lines)
	if len(f.Keep) > 0 {
		lines = applyKeep(lines, f.Keep)
	}

	// Apply replace rules
	if len(f.Replace) > 0 {
		lines = applyReplace(lines, f.Replace)
	}

	// Apply on_success / on_failure blocks
	result := strings.Join(lines, "\n")
	if exitCode == 0 && f.OnSuccess != nil {
		result = applyOutputBlock(f.OnSuccess, lines, result)
	} else if exitCode != 0 && f.OnFailure != nil {
		result = applyOutputBlock(f.OnFailure, lines, result)
	}

	return result
}

func applySkip(lines []string, patterns []string) []string {
	regexes := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if re, err := compileRegex(p); err == nil {
			regexes = append(regexes, re)
		}
	}

	out := make([]string, 0, len(lines))
	for _, line := range lines {
		skip := false
		for _, re := range regexes {
			if re.MatchString(line) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, line)
		}
	}
	return out
}

func applyKeep(lines []string, patterns []string) []string {
	regexes := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if re, err := compileRegex(p); err == nil {
			regexes = append(regexes, re)
		}
	}

	out := make([]string, 0, len(lines))
	for _, line := range lines {
		for _, re := range regexes {
			if re.MatchString(line) {
				out = append(out, line)
				break
			}
		}
	}
	return out
}

func applyReplace(lines []string, rules []ReplaceRule) []string {
	type compiledRule struct {
		re     *regexp.Regexp
		output string
	}
	compiled := make([]compiledRule, 0, len(rules))
	for _, r := range rules {
		if re, err := compileRegex(r.Pattern); err == nil {
			compiled = append(compiled, compiledRule{re, r.Output})
		}
	}

	out := make([]string, 0, len(lines))
	for _, line := range lines {
		matched := false
		for _, cr := range compiled {
			if m := cr.re.FindStringSubmatch(line); m != nil {
				replaced := cr.output
				for i, group := range m {
					replaced = strings.ReplaceAll(replaced, fmt.Sprintf("{%d}", i), group)
				}
				out = append(out, replaced)
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, line)
		}
	}
	return out
}

func applyOutputBlock(block *OutputBlock, lines []string, full string) string {
	if block.StartAt != "" {
		if re, err := compileRegex(block.StartAt); err == nil {
			for i, line := range lines {
				if re.MatchString(line) {
					lines = lines[i:]
					full = strings.Join(lines, "\n")
					break
				}
			}
		}
	}
	if len(block.Skip) > 0 {
		lines = applySkip(lines, block.Skip)
		full = strings.Join(lines, "\n")
	}
	if len(block.Keep) > 0 {
		lines = applyKeep(lines, block.Keep)
		full = strings.Join(lines, "\n")
	}
	if block.Tail > 0 && len(lines) > block.Tail {
		lines = lines[len(lines)-block.Tail:]
		full = strings.Join(lines, "\n")
	}
	if block.Head > 0 && len(lines) > block.Head {
		lines = lines[:block.Head]
		full = strings.Join(lines, "\n")
	}
	if block.Output != "" {
		return strings.ReplaceAll(block.Output, "{output}", full)
	}
	return full
}

// Regex cache to avoid recompiling the same patterns.
var (
	regexCache   = make(map[string]*regexp.Regexp)
	regexCacheMu sync.RWMutex
)

func compileRegex(pattern string) (*regexp.Regexp, error) {
	regexCacheMu.RLock()
	if re, ok := regexCache[pattern]; ok {
		regexCacheMu.RUnlock()
		return re, nil
	}
	regexCacheMu.RUnlock()

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	regexCacheMu.Lock()
	regexCache[pattern] = re
	regexCacheMu.Unlock()
	return re, nil
}
