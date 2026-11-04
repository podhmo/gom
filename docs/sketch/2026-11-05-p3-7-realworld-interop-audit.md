# P3-7 real-world interop audit: what compiles, runs, and what doesn't

- 日付: 2026-11-05
- 状態: 検証済み（`testdata/realworld` e2e + /tmp probes、全て `gom check` → `gom build` → `go build` → `go run` 通し）
- 依存: P3-2..P3-6 完了後の実測。Non-normative.

## The e2e fixture

`testdata/realworld/` is a nested Go module (`example.com/realworld`)
with seven `package main` programs; `cmd/gom/realworld_test.go`
(`TestEndToEnd_RealWorldPrograms`) copies the tree, runs
`gom check`/`gom build` on all of them, `go build`s, and executes the
binaries asserting stdout/stderr/exit code — `httpfetch` against a
loopback `httptest` server, `wc -` against a piped stdin.

| Program | Shape | Result |
| --- | --- | --- |
| `wc` | `os.Open`/`os.CreateTemp`/`os.Remove`, `bufio.Scanner`, `io.Copy`→`io.Discard`, `errors.Is(e, os.ErrNotExist)`, `os.Stdin`/`os.Exit` | works — real line/byte counts, stdin spooled through a temp file, sentinel-aware error + exit 1 |
| `cal` | `os.Args` as `List String` destructured by `match`, `strconv.Atoi` under `\|?>`, `errors.New` peel for fabricated `GoError` | works — add/mul/max, wrapped failures (`cal/main.gom:21: strconv.Atoi: ...`), exit codes |
| `greet` | `flag.Bool`/`flag.Int64` + `flag.Parse` + `flag.Args`, `extern flag.Lookup` → `Option (Ptr Flag)`, `f.Value.String ()` | works — `-shout -times 2` honored; flag values read back via the Flag record |
| `jsoncfg` | `json.NewEncoder`+`SetIndent`+`Encode` on a Gom record into `bytes.NewBufferString`, `json.NewDecoder`+`Token`/`More` decode by `%v` rendering | works — `{"Name": "gom", ...}` out, flat-object parse back, `round-trip ok` |
| `csvtojson` | `os.Open` → `csv.NewReader`/`Read ()` (`Result (List String) GoError`), `io.EOF` sentinel, `json.NewEncoder(os.Stdout)` per row | works — one JSON array per CSV row |
| `httpfetch` | `http.NewRequest`, `req.Header`/`headers.Set`, `http.DefaultClient.Do`, `resp.StatusCode`/`resp.Body`, `io.LimitReader`+`io.Copy(os.Stdout)`, `Body.Close` | works — status 200 + bounded body copy against httptest; refused-connection error path |
| `datemath` | `time.Parse`, `t.AddDate`, `t.Format`, `t.Weekday ().String ()`, `strconv.Atoi` | works — `2024-02-28 +2` → `2024-03-01 (Friday)` |

No compiler changes were needed for the fixture to work. Every
detour below is a boundary fact, not a regression.

## Per-package worked/broke

### Works end to end

- **os** — `Open`/`Create`/`CreateTemp`/`MkdirTemp`/`Stat`/`Remove`/
  `ReadDir` (`List os.DirEntry`, interface methods)/`Getenv`/`Environ`
  (`List String`)/`Args` (`List String`)/`Getwd`/`Hostname`/`Exit`/
  `Stdin`/`Stdout`/`Stderr`, `File.WriteString`/`Close`/`Name`.
  `os.Exit` in tail position generates a statement + `return
  struct{}{}` correctly (the gist-prototype bug is gone).
- **io** — `Copy`/`CopyN`/`Discard`/`LimitReader`/`MultiReader`/
  `TeeReader`/`NopCloser`/`WriteString`/`Pipe` (`(Ptr, Ptr)` tuple).
  Interface assignability works (`*os.File` → `io.Writer`,
  `*strings.Reader`/`io.NopCloser` results → `io.Reader`).
- **bufio** — `NewScanner`+`Scan`/`Text`/`Err`, `NewReader`,
  `NewWriter`+`WriteString`+`Flush`. Scanner is the line reader.
- **strconv** — `Atoi`/`Itoa`/`ParseInt`/`ParseBool`/`FormatInt`/
  `Quote`/`Unquote`.
- **strings** — all string-typed members incl. `NewReader`,
  `NewReplacer` (variadic pairs), `Join` (`List String` → `[]string`
  boundary copy), `CutPrefix` (`(T, bool)` → `(String, Bool)` tuple).
- **errors** — `Is` works against sentinel *vars* (`io.EOF`,
  `os.ErrNotExist`); `fmt.Errorf` `%w` chains verified end to end.
- **fmt** — `Print`/`Fprint`/`Sprintf`/`Errorf` families; `%w` wrap.
- **flag** — `Bool`/`Int64`/`String` define, `Parse`, `Args`
  (`List String`), `Lookup` (+ extern `Option`), `Flag.Value.String`
  reads values back.
- **encoding/json** — `NewEncoder`/`SetIndent`/`Encode(any)` accepts
  Gom records (marshals the generated struct, exported field names)
  and `List` (JSON array); `NewDecoder`/`Decode(any)`/`Token`/`More`.
- **encoding/csv** — `NewWriter`+`Write (List String)`+`Flush`,
  `NewReader`+`Read ()` — full round trip.
- **net/http** (client) — `NewRequest`, `Header` field + `Set`,
  `DefaultClient.Do`, `Response.StatusCode`/`Body`, `NoBody`.
- **time** — `Now`/`Since`/`Unix`/`Parse`/`ParseDuration`/`Date`
  (`time.January` const or `Go.conv n : time.Month`)/`AddDate`/
  `Format`/`Weekday().String`/`Duration` methods;
  `time.Sleep (Go.conv n : time.Duration)`.
- **net/url** — `Parse` → `Ptr url.URL`, `.Scheme`/`.Host`/`.Path`.
- **path/filepath** — `Base`/`Join`/`Glob` (`Result (List String)`).
- **os/exec** — `Command` (variadic), `StdoutPipe`/`Start`/`Wait`;
  `cmd.Args`/`Path`/`Dir` field reads.
- **os/signal** — `signal.Notify (ch, os.Interrupt)` — `Chan
  os.Signal` → `chan<- os.Signal` assignability holds.
- **text/template** — `New`+`Parse`+`Execute(w, any)` renders a Gom
  record through its exported Go fields.
- **context** — `Background`/`TODO`; **log** — `Println` plus
  `log.New` factory; **runtime** — `NumCPU`/`Version`; **math** —
  consts+funcs; **math/big** — `NewInt` methods; **unicode** —
  `IsUpper (Go.conv 'A' : Int32)`; **reflect** — `TypeOf(any)` →
  `Type`, `.String()` reports `int64` for a Gom Int;
  **encoding/gob**-style `Encoder(any)` pattern applies to
  `xml.NewEncoder` too (unverified, same shape as json).

### Exposed but broken / unusable today

- **`os.ReadFile`/`os.WriteFile`/`io.ReadAll`/`json.Marshal`/
  `json.Unmarshal`/`json.Valid`/`bufio.ReadString`/`utf8.Valid`/
  base64/hex/hash writes/`cmd.Output()`** — anything touching
  `[]byte`/`byte` is E0501-hidden (parameter or result). Detours:
  `WriteString`/`io.WriteString`, `io.Copy`+`io.Discard`,
  `bufio.Scanner`, the `json.Encoder`/`Decoder` streaming API.
- **3+-result functions hidden** — `strings.Cut`,
  `bufio.Reader.ReadRune`: E0501 "only 0, 1 or 2 are representable".
  spec 8.3's `(A, B) → (A, B)` row doesn't state a limit. Detour for
  Cut: `strings.Index`/`CutPrefix`-style splits.
- **`Ptr T` is write-only** — no dereference exists anywhere (no op,
  no `Ptr` module). Consequences measured in real programs:
  `flag.Int`/`flag.String` results can only be passed back to other
  `*T`-taking Go calls — values are read via `flag.Lookup` +
  `Value.String`; `json.Decoder.Decode`/`errors.As`/`json.Unmarshal`
  *do* accept a `Ptr` (`any` param), but the only `*T`s obtainable
  are Go-function results (`flag.String`, `os.Open`,
  `http.NewRequest`), so decoding into a *chosen* shape is
  impossible — `errors.As` compiles and panics at runtime ("target
  must be a non-nil pointer" family) since the required `**T`/`*error`
  cannot be produced. See [problems/2026-11-05-ptr-t-no-deref].
- **No composite literals / no field writes** — `http.Client`,
  `strings.Builder`, `sync.Mutex`, `tls.Config` are types-not-values
  (E0201); `r.Method = "POST"` / `cmd.Env = ...` is E0101. Only
  factory functions (`bytes.NewBufferString`, `log.New`,
  `http.NewRequest`) and package vars (`time.UTC`,
  `http.DefaultClient`, `os.Stdout`) can produce named-struct values.
  Corollary: Go functions with *optional* `*T` params are uncallable
  where `nil` is the needed value (`tls.Dial`'s config) — there is no
  nil literal and no way to construct one.
- **`errors.New`/`fmt.Errorf` can't fabricate an error value** — a
  lone `error` result maps to `Result Unit GoError`, so `Err e ->
  e`/`Ok _ -> todo ()` peeling is the only way to originate a
  `GoError` (used in `cal`/`jsoncfg` as `mkerr`). `errors.Is` works;
  `errors.As` unusable (pointer target). Addendum recorded in
  [problems/2026-11-04-goerror-construction].
- **`sort` is a trap** — `sort.Ints` is E0501 (`[]int` element split,
  same recorded gap as `flag.Int`'s `*int`); `sort.Strings` *compiles*
  and silently no-ops — the `List` is copied at the boundary, the
  copy is sorted, the Gom `List` is unchanged. Any Go API whose
  contract is in-place mutation of a `[]T` param does nothing under
  the spec's copy rule. See
  [problems/2026-11-05-go-param-mutation-noop].
- **`Go.Map` is opaque with no ops** — spec 8.3 says "操作は
  `Go.Map` モジュール" but `Go.Map.make` is E0201 ("`Map` has no
  constructor `make`" — `Go.Map` resolves as a *type*, there is no
  member table). A `map[K]V` from Go can only be passed along or
  field-read (`req.Header` is `http.Header` → methods still work
  since it's a *named* type — the named handle carries `Set`). Gap
  ledger candidate.
- **`Go.Any` can't be named in signatures** — E0201 "`Go` has no
  member `Any`" (the `Go` namespace table is `conv`/`Slice`/`Map`).
  Values flow fine through *inferred* positions: `let t = try
  dec.Token ()`, `fmt.Sprintf ("%v", t)`, pass back into `any`
  params — but can never be unwrapped (no type assertion; `Go.conv`
  `any`→`string` is E0507). Already an open-question; the P3-7 data
  point: `json.Decoder.Token` is the only stdlib decode path that
  works, and it only yields `Go.Any`.
- **`rune` maps to `Int32`, not `Char`** — `unicode.IsUpper` wants
  `Int32`; a `Char` literal needs `Go.conv 'A' : Int32`. spec 8.3's
  table says `rune` → `Char`; only *untyped* rune constants map to
  `Char` today (`internal/goimport/mapping.go` basicType). Gap ledger
  candidate.
- **generic functions with structural constraints** — `slices.Contains`
  E0506 (`~[]E`); already recorded. `any`/`comparable` generics work.
- **func-typed params/results** — `http.HandleFunc`,
  `filepath.Walk`/`WalkDir`, `flag.Func`, `signal.NotifyContext`-style
  hooks, `sort.Slice`, `context.WithCancel` (`CancelFunc` result) all
  hidden — no Gom-side servers/callbacks, matching the known v1 limit.
- **`error`-typed return arity aside, `*int` params are dead** —
  `flag.Int` (`*int`) is E0501 via the recorded container
  int/int64 split; `flag.Int64` works.

### Boundary answers (input for the standard-bindings task)

What needed `extern` to be *usable*: nothing in the seven programs
*required* it to compile — `flag.Lookup` needed it to be *safe*
(a `*T` that may be nil is undetectable otherwise: there is no nil
literal, so `Option (Ptr Flag)` via extern is the only nil-check in
the language). Candidates with a clear ergonomic win:

- `os.LookupEnv` — `(T, bool)` → `Option String` verified working.
- `flag.Lookup` — `*T` → `Option (Ptr Flag)` verified; in fixture.
- `strings.CutPrefix`/`CutSuffix` — `(T, bool)` → `Option String` —
  the common "trim if present" idiom, currently a tuple.
- The `*T` → `Option (Ptr T)` pattern is needed by *every* nil-able
  pointer return — this is the strongest standard-bindings argument
  since Gom has no nil literal to compare against.
- `error` → `Result Unit GoError` is correct for *failing* calls but
  wrong for `errors.New`/`fmt.Errorf` (fabrication) — an
  `fn New (msg: String) -> GoError` extern validates today? **Not
  checked** — raw shape `error` admits `Result Unit GoError` per
  spec; a bare `GoError` result is not in the admitted list, so it
  would need spec+impl extension (goerror-construction problem file).

## Verification notes

- `|?>` wrapping: a failing *step result* is wrapped once at the step
  boundary with the step's name — callee name for direct calls
  (`strconv.Atoi: ...`), `file:line` of the `fn` token for lambdas
  (`cal/main.gom:21: ...`). A Go `(T,error)` call used as a segment
  head propagates its error bare until the next boundary.
- `reflect.TypeOf 42` → `"int64"`: the ConvBoxAny materialization
  (P3-2) holds through real calls — Gom `Int` boxes as `int64`.
- `json.Decoder.Decode` *mechanically* works with a `Ptr` — verified
  by decoding into `flag.String`'s `*string` and reading back via
  `flag.Lookup(...).Value.String()`; the flag-name side effect makes
  it a probe, not a program.
- `text/template.Execute(os.Stdout, { name = "gom", age = 3 })`
  renders `{{.Name}} is {{.Age}}` — the `any` path + exported record
  fields.
- `sort.Strings xs` compile-and-silently-noop confirmed by run
  (`[b a c]` in, `[b a c]` out).

## Deferred / not probed

`gom test`/eval behavior of the fixture programs (eval does not run
the Go side); `net` servers, `database/sql` (drivers need
registration — func/struct gaps make it hopeless today); `compress/*`
(`gzip.NewWriter` + `io.Copy` should work — same Writer shape);
`encoding/xml` decode (same `*T` wall as json).
