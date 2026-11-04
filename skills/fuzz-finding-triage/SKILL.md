---
name: fuzz-finding-triage
description: Triage `fuzz-finding` GitHub issues — the CI fuzz advisory job auto-files one issue per new failure signature. This skill walks the full triage loop — list open findings, recreate the corpus file, register the bug in the package's knownBugs map, add the TODO.md entry, verify, commit, then comment + close the issue. Use whenever asked to process fuzz findings, or when `fuzz-finding` issues are open.
---

# Fuzz-finding triage

The CI `fuzz (advisory)` job files one `fuzz-finding` issue per new
failure signature (decision:
[docs/decisions/2026-11-04-fuzz-failures-file-issues.md](../../docs/decisions/2026-11-04-fuzz-failures-file-issues.md)).
An **open** issue means "found but not registered" — the source of
truth is the `knownBugs` registry + TODO.md, so the loop ends with
the issue **closed**.

Process **one issue at a time** — one finding = one commit.

## 1. List open findings

```sh
gh issue list --label fuzz-finding --state open \
  --json number,title --jq '.[] | "\(.number)\t\(.title)"'
```

## 2. Extract the report fields

```sh
gh issue view <N> --json body --jq .body > /tmp/fuzz-issue-<N>.md
```

From the body, collect:

- `- target:` → the fuzz target (`FuzzCompile`, `FuzzParse`,
  `FuzzFormat`, `FuzzFormatGen`) and its package in parens
  (`internal/build`, `internal/syntax`, `internal/format`).
- `- corpus:` → the committed corpus path
  (`<pkg>/testdata/fuzz/<target>/<id>`), or `(none)` for hangs /
  crashes where the fuzzer wrote no file.
- `## input (Go %q)` → the fenced block holding ONE Go-quoted
  string (e.g. `"package a\nfn a()->A={0(().A)}"`). This is the
  raw reproducer, quoted — including `\\`, `\"`, `\n`, `\xNN`
  escapes.
- `## 失敗出力` → the failure block; you pick `Match` substrings
  from it in step 5.

## 3. Recreate the corpus file

The fuzzer's corpus file is just the quoted input wrapped in the
`go test fuzz v1` header:

```sh
printf 'go test fuzz v1\n[]byte(%s)\n' '<quoted input>' \
  > <pkg>/testdata/fuzz/<target>/<id>
```

Use the `<id>` from the issue's corpus path. If the issue says
`corpus: (none)`, mint any filename (e.g. `issue<N>`) — corpus
filenames are arbitrary; `-run='FuzzXxx/<name>'` selects by name.

## 4. Unquote the input, compute SHA-256

The `knownBugs` key is `Sum256Hex` of the *raw* bytes. Unquote
with Go (handles every `%q` escape correctly):

```sh
mkdir -p /tmp/unquote && cat > /tmp/unquote/main.go <<'EOF'
package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func main() {
	b, _ := io.ReadAll(os.Stdin)
	s, err := strconv.Unquote(strings.TrimSpace(string(b)))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%x", sha256.Sum256([]byte(s)))
}
EOF
printf '%s' '<quoted input>' | go run /tmp/unquote/main.go
```

## 5. Register in the package's knownBugs map

Add one entry to the map in `<pkg>/fuzz_test.go`:

| target | package | registry |
| --- | --- | --- |
| `FuzzParse` | `internal/syntax` | `knownParseBugs` |
| `FuzzCompile` | `internal/build` | `knownCompileBugs` |
| `FuzzFormat`, `FuzzFormatGen` | `internal/format` | `knownFormatBugs` |

```go
fuzzutil.Sum256Hex("<sha256 hex>"): {
	Match: []string{ /* ... */ },
	TODO:  "<substring of the TODO.md item>",
	Desc:  "<short description; mention issue #N>",
},
```

**Choosing `Match`:** every substring must appear in the failure
text, and the set must be specific enough not to mask a different
bug reported alongside it.

- With an `E####` diagnostic: the code plus the stable message
  head — e.g. `[]string{"E0901", "core invariant violation"}`.
- Without a code: the fixed failure line —
  `[]string{"Source is not idempotent"}`.
- Hangs / crashes: `[]string{"compile did not return"}` /
  `[]string{"sandbox helper crashed"}` — no durations or byte
  counts (they vary per input).
- **Never** match input-dependent identifiers (`~amount`,
  variable names) — variants of the same bug must keep matching
  the signature.

`Skip: true` is ONLY for inputs that provably hang the run itself
(the leaked-goroutine OOM family); a diagnostic failure runs fine
and stays `Skip: false` (the default — omit the field).

## 6. Add the TODO.md item

Append `- [ ]` under `## Bugs`:

```markdown
- [ ] **[bug] <title>**: <1–3 sentences: symptom, repro shape,
  suspected area>. fuzz 検出（issue #N、corpus `<corpus path>`）
```

`TODO` in the registry entry = a substring of THIS line (the
`**[bug] <title>**` text is a natural pick). `CheckRegistry`
fails if no open (`- [ ]` / `- [-]`) line contains it.

## 7. Verify

```sh
go test ./<pkg> -run='Fuzz<X>/<id>'   # logs "known bug, not reporting"; PASS
go test ./<pkg> -run='TestKnownBugs'  # CheckRegistry passes
make check                            # full suite green
```

## 8. Commit, comment, close

```sh
git add <corpus> <pkg>/fuzz_test.go TODO.md
git commit -m "test: register fuzz finding #<N> (<target> — <sig>)"
gh issue comment <N> --body "knownBugs/TODO.md に登録しました（<commit sha>)。以降の正はレジストリ側です"
gh issue close <N>
```

Regression seeds: after the bug is FIXED, the stale-entry check
forces removal of the registry entry and TODO item together — the
corpus file stays as a live regression seed (see
docs/decisions/2026-11-02-fuzz-strategy.md).
