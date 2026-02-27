# rt Filter Step Reference

Exhaustive field-level documentation for every step type. This is the authoritative reference.

---

## `command`

**Type**: `string` or `array of strings`
**Required**: yes

The command pattern this filter matches against. rt compares the beginning of the user's command against this value.

```toml
command = "git push"           # matches: git push, git push origin main
command = "npm run *"          # wildcard: matches npm run dev, npm run build, etc.
command = ["cargo test", "cargo t"]  # array: matches either form
```

**Wildcard rules**:
- `*` matches any remaining arguments
- Only one `*` per command string is supported
- The wildcard always appears at the end

---

## `run`

**Type**: `string`
**Required**: no
**Default**: the matched command is executed as-is

Override the actual command executed.

```toml
command = "git status"
run = "git status --porcelain -b"
```

---

## `match_output`

**Type**: `array of tables`
**Required**: no
**Default**: `[]`

Whole-output checks. Evaluated **before any other processing**. If a check matches, its `output` is emitted and the pipeline stops entirely.

```toml
match_output = [
  { contains = "Everything up-to-date", output = "ok (up-to-date)" },
  { contains = "rejected", output = "push rejected" },
]
```

**Per-entry fields**:

| Field | Type | Required | Description |
|---|---|---|---|
| `contains` | string | no* | Literal substring to search for (case-sensitive) |
| `matches` | string | no* | Regex to match against the full output |
| `output` | string | yes | String to emit if matched |

*At least one of `contains` or `matches` must be set.

**Behavior**:
- Checks are evaluated in array order; first match wins
- Comparison is against the raw, unprocessed output
- If no entry matches, the pipeline continues normally

---

## `skip`

**Type**: `array of strings` (each is a regex)
**Required**: no
**Default**: `[]`

Drop lines matching any of the given regexes.

```toml
skip = [
  "^\\s*Compiling ",
  "^\\s*Downloading ",
  "^\\s*$",
]
```

**Behavior**:
- A line is dropped if it matches **any** regex in the array
- Regexes use Go `regexp` syntax
- Applied before `[[replace]]`

---

## `[[replace]]`

**Type**: array of tables
**Required**: no
**Default**: `[]`

Per-line regex transforms. Applied to every output line, in array order, after `skip`.

```toml
[[replace]]
pattern = '^## (\\S+?)(?:\\.\\.\\.\\S+)?$'
output = "{1}"
```

**Fields**:

| Field | Type | Required | Description |
|---|---|---|---|
| `pattern` | string | yes | Go regex pattern |
| `output` | string | yes | Template. `{0}` = full match. `{1}`, `{2}`, … = capture groups. |

**Behavior**:
- If `pattern` does not match a line, that line passes through unchanged
- If `pattern` matches, the line is replaced with the rendered `output` template
- Multiple `[[replace]]` blocks are applied in sequence
- First matching replace wins per line (subsequent replaces see the original line)

---

## `strip_ansi`

**Type**: `bool`
**Required**: no
**Default**: `false`

Strip ANSI escape sequences from output before pattern matching.

```toml
strip_ansi = true
```

**When to use**: for commands that emit colored output (test runners, linters).

---

## `[on_success]`

**Type**: table
**Required**: no

Output branch executed when the command exits with code 0.

```toml
[on_success]
output = "{output}"
head = 20
tail = 10
```

**Fields**:

| Field | Type | Description |
|---|---|---|
| `output` | string | Template. `{output}` = the filtered output text. |
| `head` | integer | Keep only the first N lines of filtered output. |
| `tail` | integer | Keep only the last N lines of filtered output. |

---

## `[on_failure]`

**Type**: table
**Required**: no

Output branch executed when the command exits with a non-zero code. Same fields as `[on_success]`.

```toml
[on_failure]
tail = 20
```

**Recommendation**: Always include `[on_failure]` with at least `tail = 20`. Users debugging failures need context.

---

## `[[variant]]`

**Type**: array of tables
**Required**: no
**Default**: `[]`

Context-aware delegation to specialized child filters.

```toml
[[variant]]
name = "vitest"
detect.files = ["vitest.config.ts", "vitest.config.js"]
filter = "npm/test-vitest"
```

**Per-entry fields**:

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Human-readable identifier |
| `detect.files` | array of strings | yes | File paths to check in CWD |
| `filter` | string | yes | Filter to delegate to (relative path without `.toml`) |

**Behavior**:
- When a variant matches, the child filter **replaces** the parent entirely
- When no variant matches, the parent filter's own fields apply as fallback
- `[[variant]]` entries must appear **after** all top-level fields in the TOML file
