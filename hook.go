package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// shellEscape wraps a string in single quotes if it contains shell metacharacters.
func shellEscape(s string) string {
	// If the string is safe (only alphanums, hyphens, underscores, dots, slashes, colons, @, =, +, commas),
	// return as-is
	safe := true
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '/' || c == ':' || c == '@' || c == '=' || c == '+' || c == ',') {
			safe = false
			break
		}
	}
	if safe && s != "" {
		return s
	}
	// Wrap in single quotes, escaping any existing single quotes
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// shellSplit splits a command string into arguments, respecting quotes.
func shellSplit(s string) []string {
	var args []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			current.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' && !inSingle {
			escaped = true
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if c == ' ' && !inSingle && !inDouble {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteByte(c)
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

// hookInput is the JSON structure Claude Code sends to PreToolUse hooks.
type hookInput struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

type bashToolInput struct {
	Command string `json:"command"`
}

// hookOutput is the JSON response that modifies the tool invocation.
type hookOutput struct {
	HookSpecificOutput hookSpecific `json:"hookSpecificOutput"`
}

type hookSpecific struct {
	HookEventName    string      `json:"hookEventName"`
	PermissionDecision string    `json:"permissionDecision"`
	UpdatedInput     interface{} `json:"updatedInput,omitempty"`
}

func cmdHook(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "rt: usage: rt hook <handle|install> [options]")
		os.Exit(1)
	}

	switch args[0] {
	case "handle":
		hookHandle()
	case "install":
		global := false
		for _, a := range args[1:] {
			if a == "--global" {
				global = true
			}
		}
		hookInstall(global)
	default:
		fmt.Fprintf(os.Stderr, "rt: unknown hook command: %s\n", args[0])
		os.Exit(1)
	}
}

func hookHandle() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return // silent fail — don't block Claude Code
	}

	var input hookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return
	}

	// Only intercept Bash tool calls
	if input.ToolName != "Bash" {
		return
	}

	var bash bashToolInput
	if err := json.Unmarshal(input.ToolInput, &bash); err != nil {
		return
	}

	cmdStr := strings.TrimSpace(bash.Command)
	if cmdStr == "" {
		return
	}

	// Check if this command matches any filter
	filters, err := loadFiltersWithCache()
	if err != nil {
		return
	}

	f := matchFilter(filters, cmdStr)
	if f == nil {
		return // no filter — let it pass through unchanged
	}

	// Find rt binary path
	rtBin, err := os.Executable()
	if err != nil {
		rtBin = "rt"
	}

	// Rewrite the command to go through rt.
	// Shell-escape each argument to protect metacharacters like (, ), |
	// from being interpreted when Claude Code runs this via sh -c.
	parts := shellSplit(cmdStr)
	var escaped []string
	for _, p := range parts {
		escaped = append(escaped, shellEscape(p))
	}
	newCmd := fmt.Sprintf("%s run %s", rtBin, strings.Join(escaped, " "))

	out := hookOutput{
		HookSpecificOutput: hookSpecific{
			HookEventName:    "PreToolUse",
			PermissionDecision: "allow",
			UpdatedInput: bashToolInput{
				Command: newCmd,
			},
		},
	}

	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(out)
}

func hookInstall(global bool) {
	rtBin, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: cannot determine binary path: %v\n", err)
		os.Exit(1)
	}

	// Create hook shell script
	hookDir := filepath.Join(os.Getenv("HOME"), ".config", "rt", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	hookScript := filepath.Join(hookDir, "pre-tool-use.sh")
	content := fmt.Sprintf("#!/bin/sh\nexec '%s' hook handle\n", rtBin)
	if err := os.WriteFile(hookScript, []byte(content), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	// Update Claude Code settings
	var settingsPath string
	if global {
		settingsPath = filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	} else {
		// Project-local
		cwd, _ := os.Getwd()
		settingsPath = filepath.Join(cwd, ".claude", "settings.json")
	}

	settings := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(data, &settings)
	}

	// Build the hook entry
	hookEntry := map[string]interface{}{
		"matcher": "Bash",
		"hooks": []map[string]interface{}{
			{
				"type":    "command",
				"command": fmt.Sprintf("'%s'", hookScript),
			},
		},
	}

	// Get or create hooks.PreToolUse
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	// Replace any existing Bash matcher or append
	preToolUse, _ := hooks["PreToolUse"].([]interface{})
	found := false
	for i, entry := range preToolUse {
		if m, ok := entry.(map[string]interface{}); ok {
			if m["matcher"] == "Bash" {
				preToolUse[i] = hookEntry
				found = true
				break
			}
		}
	}
	if !found {
		preToolUse = append(preToolUse, hookEntry)
	}

	hooks["PreToolUse"] = preToolUse
	settings["hooks"] = hooks

	// Ensure parent dir exists
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "rt: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("hook script: %s\n", hookScript)
	fmt.Printf("settings updated: %s\n", settingsPath)
	scope := "project-local"
	if global {
		scope = "global"
	}
	fmt.Printf("scope: %s\n", scope)
}
