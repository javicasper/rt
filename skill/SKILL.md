---
name: rt-filter
description: This skill should be used when the user asks to "create a filter", "write an rt filter", "add a filter for <tool>", "how do I filter output", or needs guidance on rt filter TOML format, step types, or placement conventions.
version: 0.1.0
---

# rt Filter Authoring

You are an expert at writing rt filter files. rt (reduce tokens) is a config-driven CLI that compresses command output before it reaches an LLM context. Filters are TOML files that define how to process a command's output.

When the user asks you to create or modify a filter, follow this guide exactly. Produce valid, idiomatic TOML that matches the schema described below.

---

## Section 1 — What a Filter File Is

A filter file is a TOML file that describes:
- Which command(s) it applies to (`command`)
- How to transform the raw output (steps, applied in a fixed order)
- What to emit on success vs. failure

Filters live in two places, searched in priority order:

1. `~/.config/rt/filters/` — user-level overrides and custom filters
2. Built-in library (embedded in the rt binary)

First match wins. Use `rt ls` to see all available filters.

---

## Section 2 — Processing Order

Steps execute in this fixed order — **do not rearrange them**:

1. **`match_output`** — whole-output substring/regex checks; if matched, short-circuits the entire pipeline and emits immediately
2. **`skip`** — line-level filtering (drop lines by regex)
3. **`[[replace]]`** — per-line regex transforms applied to every remaining line, in array order
4. **Exit-code branch** — `[on_success]` or `[on_failure]` depending on exit code

Within `[on_success]` and `[on_failure]`, fields are processed as:
- `head` / `tail` → trim lines
- `output` → final template render (`{output}` = the filtered text)

---

## Section 3 — Top-Level Fields Reference

| Field | Type | Default | Description |
|---|---|---|---|
| `command` | string or array of strings | required | Command pattern(s) to match. Supports `*` wildcard. |
| `run` | string | (same as command) | Override the actual command executed. |
| `match_output` | array of tables | `[]` | Whole-output checks. Short-circuit on first match. |
| `skip` | array of strings (regex) | `[]` | Drop lines matching any regex. |
| `[[replace]]` | array of tables | `[]` | Per-line regex replacements, in order. |
| `strip_ansi` | bool | `false` | Strip ANSI escape sequences before pattern matching. |
| `[on_success]` | table | (absent) | Output branch for exit code 0. |
| `[on_failure]` | table | (absent) | Output branch for non-zero exit. |
| `[[variant]]` | array of tables | `[]` | Context-aware delegation to specialized child filters. |

---

## Section 4 — Step Types

### 4.1 `match_output` — Whole-Output Short-Circuit

Check the entire raw output for a substring or regex. If matched, emit a fixed string and stop.

```toml
match_output = [
  { contains = "Everything up-to-date", output = "ok (up-to-date)" },
  { contains = "rejected", output = "push rejected (try pulling first)" },
]
```

- `contains`: literal substring to search for (case-sensitive)
- `matches`: regex to match against the full output (alternative to `contains`)
- `output`: string to emit if matched

**When to use**: for well-known one-liner outcomes that make the rest of filtering irrelevant.

---

### 4.2 `skip` — Line Filtering

Drop lines matching any regex pattern.

```toml
skip = [
  "^\\s*Compiling ",
  "^\\s*Downloading ",
  "^\\s*$",
]
```

- Array of regex strings
- A line is dropped if it matches **any** regex in the array

**When to use**: for removing known noise patterns (progress output, blank lines, debug info).

---

### 4.3 `[[replace]]` — Per-Line Regex Transforms

Applied to every line, in array order. Use to reformat noisy lines.

```toml
[[replace]]
pattern = '^## (\\S+?)(?:\\.\\.\\.\\S+)?$'
output = "{1}"

[[replace]]
pattern = '^\\s+Compiling (\\S+) v(\\S+)'
output = "compiling {1}@{2}"
```

- `pattern`: Go regex pattern
- `output`: template with `{0}` (full match), `{1}`, `{2}`, … for capture groups
- If the pattern doesn't match a line, that line passes through unchanged

**When to use**: when a line contains useful information but in a verbose format.

---

### 4.4 `[on_success]` / `[on_failure]` — Exit Code Branches

```toml
[on_success]
output = "{output}"

[on_failure]
tail = 20
```

**Fields**:

| Field | Type | Description |
|---|---|---|
| `output` | string | Template for the final output. `{output}` = the filtered output text. |
| `head` | integer | Keep only the first N lines. |
| `tail` | integer | Keep only the last N lines. |

**When to use**: Always. Every filter should have at least `[on_success]` or `[on_failure]`. Use `[on_failure]` with `tail` to show enough context to diagnose errors.

---

### 4.5 `[[variant]]` — Context-Aware Filter Delegation

For commands that wrap different underlying tools (e.g. `npm test` may run Jest or Vitest).

```toml
command = ["npm test", "pnpm test"]

[on_success]
output = "{output}"

[on_failure]
tail = 20

[[variant]]
name = "vitest"
detect.files = ["vitest.config.ts", "vitest.config.js"]
filter = "npm/test-vitest"
```

**Fields**:

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Human-readable identifier |
| `detect.files` | array of strings | yes | File paths to check in CWD |
| `filter` | string | yes | Filter to delegate to (e.g. `"npm/test-vitest"`) |

When a variant matches, the child filter replaces the parent entirely. When no variant matches, the parent filter applies as fallback.

---

## Section 5 — Naming & Placement Conventions

**File naming**:
- `filters/<tool>/<subcommand>.toml` for two-word commands: `filters/git/push.toml`
- `filters/<tool>.toml` for single-word commands: `filters/pytest.toml`
- For wildcards: `filters/npm/run.toml` with `command = "npm run *"`
- Lowercase filenames only, no spaces

**Placement**: `~/.config/rt/filters/` for user filters.

**Command field**:
- Exact match: `command = "git push"` matches `git push` and `git push origin main`
- Wildcard: `command = "npm run *"` matches `npm run dev`, `npm run build`, etc.
- Array: `command = ["cargo test", "cargo t"]` matches either form

---

## Section 6 — Workflow for Creating a New Filter

### Step 1: Understand the command's output

Look for:
- What's signal (errors, results, summaries)
- What's noise (progress bars, compilation lines, download progress, blank lines)

### Step 2: Draft the filter

1. Set `command` to match the command pattern
2. Add `match_output` for well-known short-circuit cases
3. Add `skip` to drop noise lines
4. Add `[[replace]]` to reformat noisy-but-useful lines
5. Write `[on_success]` with the desired output format
6. Write `[on_failure]` with enough context to diagnose (`tail = 20` is a safe default)

### Step 3: Validate and test

```sh
rt check path/to/filter.toml    # validate TOML structure
rt run <command>                 # test with real output
```

### Step 4: Place the file

- `~/.config/rt/filters/<tool>/<subcommand>.toml`

Or install with: `rt add path/to/filter.toml`

---

## Section 7 — Two Annotated Examples

### Example 1: `git push` (simple — match_output + skip)

```toml
# filters/git/push.toml
command = "git push"

match_output = [
  { contains = "Everything up-to-date", output = "ok (up-to-date)" },
  { contains = "rejected", output = "push rejected (try pulling first)" },
]

skip = [
  "^remote:",
  "^To https?://",
  "^Enumerating objects:",
  "^Counting objects:",
  "^Compressing objects:",
  "^Writing objects:",
  "^Delta resolution:",
  "^Total ",
]

[on_success]
output = "{output}"

[on_failure]
tail = 5
```

### Example 2: `git status` (replace — reformat porcelain output)

```toml
# filters/git/status.toml
command = "git status"
run = "git status --porcelain -b"

[[match_output]]
contains = "not a git repository"
output = "Not a git repository"

[[replace]]
pattern = '^## HEAD \(no branch\).*$'
output = "HEAD (detached)"

[[replace]]
pattern = '^## (\S+?)(?:\.\.\.\S+)?\s+\[(.+)\]$'
output = "{1} [{2}]"

[[replace]]
pattern = '^## (\S+?)(?:\.\.\.\S+)?$'
output = "{1}"
```

---

## Section 8 — Common Mistakes to Avoid

1. **Escape backslashes in TOML strings.** `\\d` in regular strings means `\d` in the regex.
2. **`match_output` is a short-circuit.** If it matches, nothing else runs. It always runs first.
3. **Don't over-filter.** Start with `skip` for noise removal. Only add `[[replace]]` if lines need reformatting.
4. **Always include `[on_failure]`.** Users debugging failures need context. `tail = 20` is a safe default.
5. **Test with real output.** A filter that works on a trimmed example may miss edge cases.
