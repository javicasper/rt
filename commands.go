package main

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func cmdRun(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "rt: usage: rt run <command...>")
		os.Exit(1)
	}

	cmdStr := strings.Join(args, " ")

	filters, err := loadFiltersWithCache()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: error loading filters: %v\n", err)
		os.Exit(1)
	}

	f := matchFilter(filters, cmdStr)

	// Determine what command to actually execute
	var result runResult
	if f != nil && f.Run != "" {
		result = runCommand(f.Run)
	} else {
		result = runCommandFromArgs(args)
	}

	// No filter matched — passthrough
	if f == nil {
		if result.ExitCode != 0 {
			fmt.Fprintf(os.Stdout, "Error: Exit code %d\n", result.ExitCode)
		}
		fmt.Print(result.Output)
		recordRun("passthrough", cmdStr, result.Output, result.Output)
		return
	}

	filtered := applyFilter(f, result.Output, result.ExitCode)

	if result.ExitCode != 0 {
		fmt.Fprintf(os.Stdout, "Error: Exit code %d\n", result.ExitCode)
	}
	fmt.Print(filtered)
	if filtered != "" && !strings.HasSuffix(filtered, "\n") {
		fmt.Println()
	}

	recordRun(f.Name, cmdStr, result.Output, filtered)
}

func cmdLs() {
	filters, err := loadFiltersWithCache()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: error loading filters: %v\n", err)
		os.Exit(1)
	}

	for _, f := range filters {
		cmds := strings.Join(f.Command, ", ")
		src := f.Source
		fmt.Printf("  %-25s [%s]  →  %s\n", f.Name, src, cmds)
	}
}

func cmdShow(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "rt: usage: rt show <filter-name>")
		os.Exit(1)
	}
	name := args[0]

	// Try user dir first
	userPath := filepath.Join(userFilterDir(), name+".toml")
	if data, err := os.ReadFile(userPath); err == nil {
		fmt.Print(string(data))
		return
	}

	// Try built-in
	embeddedPath := "filters/" + name + ".toml"
	if data, err := fs.ReadFile(embeddedFilters, embeddedPath); err == nil {
		fmt.Print(string(data))
		return
	}

	fmt.Fprintf(os.Stderr, "rt: filter not found: %s\n", name)
	os.Exit(1)
}

func cmdCheck(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "rt: usage: rt check <file.toml>")
		os.Exit(1)
	}

	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	_, err = parseFilter(data, "check", "file", args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: invalid filter: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("ok")
}

func cmdGain(args []string) {
	byFilter := false
	for _, a := range args {
		if a == "--by-filter" {
			byFilter = true
		}
	}
	if byFilter {
		printGainByFilter()
	} else {
		printGainSummary()
	}
}

func cmdAdd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "rt: usage: rt add <file.toml | url>")
		os.Exit(1)
	}
	src := args[0]

	var data []byte
	var err error
	var baseName string

	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		resp, err := http.Get(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rt: download failed: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rt: read failed: %v\n", err)
			os.Exit(1)
		}
		// Extract filename from URL
		parts := strings.Split(src, "/")
		baseName = parts[len(parts)-1]
	} else {
		data, err = os.ReadFile(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rt: %v\n", err)
			os.Exit(1)
		}
		baseName = filepath.Base(src)
	}

	// Validate
	if _, err := parseFilter(data, "check", "file", src); err != nil {
		fmt.Fprintf(os.Stderr, "rt: invalid filter: %v\n", err)
		os.Exit(1)
	}

	dest := filepath.Join(userFilterDir(), baseName)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(dest, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	_ = clearCache()
	fmt.Printf("installed: %s\n", dest)
}

func cmdEject(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "rt: usage: rt eject <filter-name>")
		os.Exit(1)
	}
	name := args[0]

	embeddedPath := "filters/" + name + ".toml"
	data, err := fs.ReadFile(embeddedFilters, embeddedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: built-in filter not found: %s\n", name)
		os.Exit(1)
	}

	dest := filepath.Join(userFilterDir(), name+".toml")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(dest, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	_ = clearCache()
	fmt.Printf("ejected to: %s\n", dest)
}

func cmdCache(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "rt: usage: rt cache <clear|info>")
		os.Exit(1)
	}
	switch args[0] {
	case "clear":
		if err := clearCache(); err != nil {
			if os.IsNotExist(err) {
				fmt.Println("no cache file")
			} else {
				fmt.Fprintf(os.Stderr, "rt: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Println("cache cleared")
		}
	case "info":
		path := cacheFilePath()
		info, err := os.Stat(path)
		if err != nil {
			fmt.Printf("cache path: %s\nstatus: not found\n", path)
			return
		}
		fmt.Printf("cache path: %s\nsize: %d bytes\nbuilt: %s\n", path, info.Size(), info.ModTime().Format("2006-01-02 15:04:05"))
	default:
		fmt.Fprintf(os.Stderr, "rt: unknown cache command: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdSkill() {
	skillDir := filepath.Join(os.Getenv("HOME"), ".claude", "skills")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	// Read skill from embedded or from alongside binary
	srcPath := filepath.Join(projectDir(), "SKILL.md")
	data, err := os.ReadFile(srcPath)
	if err != nil {
		// Fallback: use the embedded skill content
		data = []byte(embeddedSkillContent)
	}

	dest := filepath.Join(skillDir, "rt-filter.md")
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("skill installed: %s\n", dest)
}

func projectDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

const embeddedSkillContent = `# Skill: Creating rt filters

## What is rt?

rt (reduce tokens) filters command output to minimize token usage in LLM conversations.
Filters are TOML files stored in ~/.config/rt/filters/.

## Filter TOML format

` + "```" + `toml
# Required: command pattern(s) to match. Use * for wildcard args.
command = "git status"
# or multiple: command = ["npm test", "pnpm test"]

# Optional: override the actual command executed
run = "git status --porcelain -b"

# Optional: skip lines matching these regex patterns
skip = [
  "^\\s*Compiling ",
  "^\\s*Downloading ",
]

# Optional: replace lines matching regex. Use {1}, {2} for capture groups.
[[replace]]
pattern = '^## (\\S+?)(?:\\.\\.\\.\\S+)?$'
output = "{1}"

# Optional: short-circuit if output contains/matches a pattern
[[match_output]]
contains = "not a git repository"
output = "Not a git repository"

# Optional: post-processing on success (exit code 0)
[on_success]
output = "{output}"  # {output} = the filtered text

# Optional: post-processing on failure (exit code != 0)
[on_failure]
tail = 20  # only keep last 20 lines on error
` + "```" + `

## File naming convention

Filters go in ~/.config/rt/filters/ with path matching the command:
- git status → git/status.toml
- cargo install → cargo/install.toml
- npm test → npm/test.toml

## Validation

Run ` + "`rt check path/to/filter.toml`" + ` to validate a filter before installing.

## Testing

After creating a filter, test it with:
` + "```" + `bash
rt run <command>       # see filtered output
` + "```" + `
`
