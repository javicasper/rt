package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

// runResult holds the output and exit code of a command execution.
type runResult struct {
	Output   string
	ExitCode int
}

// runCommand executes a command string via sh -c to support quoting and special characters.
func runCommand(cmdStr string) runResult {
	if cmdStr == "" {
		return runResult{Output: "", ExitCode: 1}
	}

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	return runResult{
		Output:   buf.String(),
		ExitCode: exitCode,
	}
}

// runCommandFromArgs executes the original command args (not the filter's run override).
func runCommandFromArgs(args []string) runResult {
	if len(args) == 0 {
		return runResult{Output: "", ExitCode: 1}
	}

	// Join args and run via shell to handle quoting consistently
	return runCommand(strings.Join(args, " "))
}
