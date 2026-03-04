package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tiktoken-go/tokenizer"
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

var tokenCodec *tokenizer.Codec

func init() {
	codec, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		// Fallback: will use len/4 if codec is nil
		return
	}
	tokenCodec = &codec
}

func estimateTokens(s string) int {
	if tokenCodec != nil {
		ids, _, _ := (*tokenCodec).Encode(s)
		return len(ids)
	}
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

// extractBaseCmd returns the first 1-2 words of the last meaningful command
// in a shell chain. For "cd /path && kubectl logs foo", it returns "kubectl logs".
// For "grep -r pattern .", it returns "grep -r".
func extractBaseCmd(command string) string {
	// Find the rightmost shell chain operator (&&, ||, ;, |) and take
	// the command after it. We scan right-to-left for the last operator.
	best := -1
	bestLen := 0
	for _, sep := range []string{"&&", "||", "; ", "| "} {
		if idx := strings.LastIndex(command, sep); idx > best {
			best = idx
			bestLen = len(sep)
		}
	}

	last := command
	if best >= 0 {
		candidate := strings.TrimSpace(command[best+bestLen:])
		if candidate != "" {
			last = candidate
		}
	}

	// Take first 2 words as base command
	fields := strings.Fields(last)
	if len(fields) == 0 {
		return command
	}
	// Skip words that look like shell artifacts (start with quote, redirect, etc.)
	start := 0
	for start < len(fields) && (fields[start] == "" || fields[start][0] == '\'' || fields[start][0] == '"' || fields[start][0] == '(' || fields[start] == "2>/dev/null" || fields[start] == "2>&1") {
		start++
	}
	if start >= len(fields) {
		start = 0
	}
	fields = fields[start:]

	if len(fields) == 0 {
		return command
	}
	// Strip directory from absolute paths: /usr/bin/kubectl → kubectl
	if strings.Contains(fields[0], "/") {
		fields[0] = filepath.Base(fields[0])
	}
	if len(fields) == 1 {
		return fields[0]
	}
	return fields[0] + " " + fields[1]
}

func querySuggestions(minTokens int) ([]suggestEntry, error) {
	db, err := openStatsDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT command, input_tokens
		FROM runs
		WHERE filter_name = 'passthrough'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Group by extracted base command in Go
	type accum struct {
		runs        int
		totalTokens int
	}
	groups := make(map[string]*accum)

	for rows.Next() {
		var cmd string
		var tokens int
		if err := rows.Scan(&cmd, &tokens); err != nil {
			return nil, err
		}
		base := extractBaseCmd(cmd)
		// Skip entries that don't look like a valid command
		if base == "" || (base[0] != '/' && (base[0] < 'a' || base[0] > 'z') && (base[0] < 'A' || base[0] > 'Z') && base[0] != '.') {
			continue
		}
		a, ok := groups[base]
		if !ok {
			a = &accum{}
			groups[base] = a
		}
		a.runs++
		a.totalTokens += tokens
	}

	var entries []suggestEntry
	for base, a := range groups {
		if a.totalTokens < minTokens {
			continue
		}
		avg := 0
		if a.runs > 0 {
			avg = a.totalTokens / a.runs
		}
		entries = append(entries, suggestEntry{
			BaseCmd:     base,
			Runs:        a.runs,
			TotalTokens: a.totalTokens,
			AvgTokens:   avg,
		})
	}

	// Sort by total tokens descending
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].TotalTokens > entries[j].TotalTokens
	})

	return entries, nil
}

//go:embed suggest-ignore
var embeddedSuggestIgnore string

func suggestIgnorePath() string {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".config", "rt", "suggest-ignore")
	}
	return filepath.Join(cfg, "rt", "suggest-ignore")
}

func parseSuggestIgnore(data string) []string {
	var patterns []string
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

func loadSuggestIgnore() ([]string, error) {
	// Start with built-in defaults
	seen := make(map[string]bool)
	var patterns []string
	for _, p := range parseSuggestIgnore(embeddedSuggestIgnore) {
		if !seen[p] {
			seen[p] = true
			patterns = append(patterns, p)
		}
	}

	// Merge user patterns
	data, err := os.ReadFile(suggestIgnorePath())
	if err != nil && !os.IsNotExist(err) {
		return patterns, err
	}
	if err == nil {
		for _, p := range parseSuggestIgnore(string(data)) {
			if !seen[p] {
				seen[p] = true
				patterns = append(patterns, p)
			}
		}
	}

	return patterns, nil
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
