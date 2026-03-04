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

	// If first arg is "--", treat the rest as a single shell string (passthrough mode).
	// This preserves pipes, redirections, &&, etc.
	shellMode := false
	if args[0] == "--" {
		args = args[1:]
		shellMode = true
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
		// If the command has chain operators, preserve the setup prefix
		// and only replace the last segment with the filter's run command.
		// e.g. "cd /tmp && git status" + run="git status --porcelain -b"
		//    → "cd /tmp && git status --porcelain -b"
		runCmd := f.Run
		if prefix := chainPrefix(cmdStr); prefix != "" {
			runCmd = prefix + runCmd
		}
		result = runCommand(runCmd)
	} else if shellMode || f == nil {
		// Passthrough or explicit shell mode: use sh -c to preserve pipes, redirections, etc.
		result = runCommand(cmdStr)
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
	mode := ""
	for _, a := range args {
		switch a {
		case "--by-filter":
			mode = "by-filter"
		case "--log":
			mode = "log"
		}
	}
	switch mode {
	case "by-filter":
		printGainByFilter()
	case "log":
		printGainLog()
	default:
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

func cmdSuggest() {
	entries, err := querySuggestions(500) // min 500 tokens total
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: error reading stats: %v\n", err)
		os.Exit(1)
	}

	// Filter out commands that now have a filter (historical passthrough data)
	filters, _ := loadFiltersWithCache()
	if len(filters) > 0 {
		var filtered []suggestEntry
		for _, e := range entries {
			if matchFilter(filters, e.BaseCmd) == nil {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	// Filter out ignored patterns
	ignorePatterns, err := loadSuggestIgnore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: warning: could not load suggest-ignore: %v\n", err)
	}
	if len(ignorePatterns) > 0 {
		var filtered []suggestEntry
		for _, e := range entries {
			ignored := false
			for _, p := range ignorePatterns {
				if strings.HasPrefix(e.BaseCmd, p) {
					ignored = true
					break
				}
			}
			if !ignored {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	if len(entries) == 0 {
		fmt.Println("no suggestions — all frequent commands have filters or are below threshold")
		return
	}
	fmt.Println("commands without filters (sorted by total tokens wasted):")
	for _, e := range entries {
		fmt.Printf("  %-35s runs: %4d  avg: %5d tok  total: %d tok\n",
			e.BaseCmd, e.Runs, e.AvgTokens, e.TotalTokens)
	}
}

func cmdSuggestIgnore(args []string) {
	// No args: list current patterns
	if len(args) == 0 {
		patterns, err := loadSuggestIgnore()
		if err != nil {
			fmt.Fprintf(os.Stderr, "rt: error reading suggest-ignore: %v\n", err)
			os.Exit(1)
		}
		if len(patterns) == 0 {
			fmt.Println("no ignored patterns")
			return
		}
		for _, p := range patterns {
			fmt.Println(p)
		}
		return
	}

	pattern := strings.Join(args, " ")

	// Check for duplicates
	patterns, err := loadSuggestIgnore()
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "rt: error reading suggest-ignore: %v\n", err)
		os.Exit(1)
	}
	for _, p := range patterns {
		if p == pattern {
			fmt.Fprintf(os.Stderr, "rt: pattern already ignored: %s\n", pattern)
			return
		}
	}

	// Append to file
	path := suggestIgnorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, pattern); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("added to suggest-ignore: %s\n", pattern)
}

func cmdSkill(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "rt: usage: rt skill install")
		os.Exit(1)
	}

	switch args[0] {
	case "install":
		skillInstall()
	default:
		fmt.Fprintf(os.Stderr, "rt: unknown skill command: %s\n", args[0])
		os.Exit(1)
	}
}

func skillInstall() {
	destDir := filepath.Join(os.Getenv("HOME"), ".claude", "skills", "rt-filter")

	// Remove old installation
	_ = os.RemoveAll(destDir)

	// Copy from embedded skill FS
	if err := copyEmbeddedDir(embeddedSkill, "skill", destDir); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("skill installed: %s\n", destDir)
}

func copyEmbeddedDir(fsys fs.FS, root, dst string) error {
	return fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
