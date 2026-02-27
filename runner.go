package main

import (
	"bytes"
	"os"
	"os/exec"
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

// runCommandFromArgs executes command args directly without shell interpolation.
// This preserves arguments that contain shell metacharacters like (, ), |, etc.
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
