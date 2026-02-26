package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		cmdRun(os.Args[2:])
	case "ls":
		cmdLs()
	case "show":
		cmdShow(os.Args[2:])
	case "check":
		cmdCheck(os.Args[2:])
	case "gain":
		cmdGain(os.Args[2:])
	case "add":
		cmdAdd(os.Args[2:])
	case "eject":
		cmdEject(os.Args[2:])
	case "cache":
		cmdCache(os.Args[2:])
	case "skill":
		cmdSkill()
	case "--version", "-V":
		fmt.Println("rt", version)
	case "--help", "-h", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "rt: unknown command %q\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `rt %s — reduce tokens in command output

Usage: rt <command> [args...]

Commands:
  run <cmd...>       Run a command and filter its output
  ls                 List available filters
  show <filter>      Show filter TOML source
  check <file>       Validate a filter TOML file
  gain [--by-filter] Show token savings statistics
  add <file|url>     Install a filter
  eject <filter>     Copy built-in filter to user dir for customization
  cache clear|info   Manage filter cache
  skill              Install the Claude Code skill for filter authoring

Options:
  -V, --version      Print version
  -h, --help         Print help
`, version)
}
