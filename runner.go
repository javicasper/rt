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

// runCommand executes a command string and captures combined stdout+stderr.
func runCommand(cmdStr string) runResult {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return runResult{Output: "", ExitCode: 1}
	}

	cmd := exec.Command(parts[0], parts[1:]...)
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

// runCommandRaw executes the original command args (not the filter's run override).
func runCommandFromArgs(args []string) runResult {
	if len(args) == 0 {
		return runResult{Output: "", ExitCode: 1}
	}

	cmd := exec.Command(args[0], args[1:]...)
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
