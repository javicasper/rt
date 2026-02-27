package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func statsDBPath() string {
	data, err := os.UserCacheDir()
	if err != nil {
		data = filepath.Join(os.Getenv("HOME"), ".local", "share")
	} else {
		// Use data dir, not cache dir
		data = strings.Replace(data, ".cache", ".local/share", 1)
	}
	return filepath.Join(data, "rt", "tracking.db")
}

func openStatsDB() (*sql.DB, error) {
	path := statsDBPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		filter_name TEXT NOT NULL,
		command TEXT NOT NULL,
		input_tokens INTEGER NOT NULL,
		output_tokens INTEGER NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// estimateTokens gives a rough token count (chars / 4 is a reasonable approximation).
func estimateTokens(s string) int {
	return (len(s) + 3) / 4
}

func recordRun(filterName, command, rawOutput, filteredOutput string) {
	db, err := openStatsDB()
	if err != nil {
		return // stats are best-effort
	}
	defer db.Close()

	inputTok := estimateTokens(rawOutput)
	outputTok := estimateTokens(filteredOutput)

	_, _ = db.Exec(
		`INSERT INTO runs (filter_name, command, input_tokens, output_tokens, created_at) VALUES (?, ?, ?, ?, ?)`,
		filterName, command, inputTok, outputTok, time.Now().UTC().Format(time.RFC3339),
	)
}

type gainEntry struct {
	Filter       string
	Runs         int
	InputTokens  int
	OutputTokens int
	Saved        int
	Percent      float64
}

func queryGainTotal() (runs, input, output, saved int, pct float64, err error) {
	db, err := openStatsDB()
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}
	defer db.Close()

	err = db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0) FROM runs`).
		Scan(&runs, &input, &output)
	if err != nil {
		return
	}
	saved = input - output
	if input > 0 {
		pct = float64(saved) / float64(input) * 100
	}
	return
}

func queryGainByFilter() ([]gainEntry, error) {
	db, err := openStatsDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT filter_name, COUNT(*), SUM(input_tokens), SUM(output_tokens) FROM runs GROUP BY filter_name ORDER BY SUM(input_tokens)-SUM(output_tokens) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []gainEntry
	for rows.Next() {
		var e gainEntry
		if err := rows.Scan(&e.Filter, &e.Runs, &e.InputTokens, &e.OutputTokens); err != nil {
			return nil, err
		}
		e.Saved = e.InputTokens - e.OutputTokens
		if e.InputTokens > 0 {
			e.Percent = float64(e.Saved) / float64(e.InputTokens) * 100
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func printGainSummary() {
	runs, input, output, saved, pct, err := queryGainTotal()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: error reading stats: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("rt gain summary\n")
	fmt.Printf("  total runs:     %d\n", runs)
	fmt.Printf("  input tokens:   %d est.\n", input)
	fmt.Printf("  output tokens:  %d est.\n", output)
	fmt.Printf("  tokens saved:   %d est. (%.1f%%)\n", saved, pct)
}

func printGainLog() {
	db, err := openStatsDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: error reading stats: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT filter_name, command, input_tokens, output_tokens, created_at FROM runs ORDER BY id DESC LIMIT 50`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: error reading stats: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	for rows.Next() {
		var filter, cmd, ts string
		var inTok, outTok int
		if err := rows.Scan(&filter, &cmd, &inTok, &outTok, &ts); err != nil {
			continue
		}
		saved := inTok - outTok
		pct := 0.0
		if inTok > 0 {
			pct = float64(saved) / float64(inTok) * 100
		}
		// Trim timestamp to just time
		if len(ts) >= 16 {
			ts = ts[5:16]
		}
		fmt.Printf("  %s  %-18s %-35s %4d → %4d tok (%.0f%%)\n",
			ts, filter, cmd, inTok, outTok, pct)
	}
}

type suggestEntry struct {
	BaseCmd     string
	Runs        int
	TotalTokens int
	AvgTokens   int
}

func querySuggestions(minTokens int) ([]suggestEntry, error) {
	db, err := openStatsDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Extract base command (first 1-3 words) from passthrough runs,
	// group by it, and find those with significant token usage.
	rows, err := db.Query(`
		SELECT
			CASE
				WHEN INSTR(SUBSTR(command, INSTR(command, ' ') + 1), ' ') > 0
				THEN SUBSTR(command, 1, INSTR(command, ' ') + INSTR(SUBSTR(command, INSTR(command, ' ') + 1), ' ') - 1)
				ELSE command
			END AS base_cmd,
			COUNT(*) AS runs,
			SUM(input_tokens) AS total_tokens
		FROM runs
		WHERE filter_name = 'passthrough'
		GROUP BY base_cmd
		HAVING total_tokens >= ?
		ORDER BY total_tokens DESC
	`, minTokens)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []suggestEntry
	for rows.Next() {
		var e suggestEntry
		if err := rows.Scan(&e.BaseCmd, &e.Runs, &e.TotalTokens); err != nil {
			return nil, err
		}
		if e.Runs > 0 {
			e.AvgTokens = e.TotalTokens / e.Runs
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func printGainByFilter() {
	entries, err := queryGainByFilter()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rt: error reading stats: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("rt gain by filter\n")
	for _, e := range entries {
		fmt.Printf("  %-30s runs: %4d  saved: %d est. (%.1f%%)\n",
			e.Filter, e.Runs, e.Saved, e.Percent)
	}
}
