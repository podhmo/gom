# Gist CLI prototype: API contract and compiler gaps

- 日付: 2026-11-03
- 状態: 検証済み

## Primary source

[GitHub REST Gists documentation](https://docs.github.com/en/rest/gists/gists)
was read on 2026-11-03. Create uses POST `/gists` with a filename-keyed `files`
object containing string `content`, and returns 201. Update uses PATCH
`/gists/{gist_id}` and returns 200. Omitted files remain unchanged; this CLI
never sends null deletion entries. The response includes `html_url`.
The documented request example recommends GitHub JSON Accept media type,
Bearer authorization and the `2026-03-10` API version. Only local fake servers
will be called during implementation and tests.

## Initial compiler probes (base daf5ee4)

```gom
package main
import go "net/http"
import go "os"
import go "strings"
fn fetch () -> Result String GoError = {
  let content = try os.ReadFile "hello.txt"
  let req = try http.NewRequest ("POST", "http://localhost/gists", strings.NewReader "{}")
  req.Header.Set ("Accept", "application/vnd.github+json")
  let response = try http.DefaultClient.Do req
  Ok response.Status
}
```

`gom check` reports:

- E0501 on `os.ReadFile`: `byte/uint8 is not supported yet`.
- E0401 on `strings.NewReader`: expected `io.Reader`, got `Ptr strings.Reader`.
- E0401 on `req.Header`: `Ptr net/http.Request has no field or method Header`.
- E0401 on `response.Status`: `Ptr net/http.Response has no field or method Status`.

HTTP also needs `response.StatusCode` and `response.Body`. Header access is
read-only (a returned named `http.Header` value already has Set). Request and
response fields need mapping of the concrete Go field type plus direct selector
lowering, not Go struct construction. Reader assignability needs Go's named
interface assignment relation; a concrete `*strings.Reader` implements
`io.Reader` in Go. JSON marshal/unmarshal and `os.ReadFile` require byte slices.
JSON decoding into a Gom record additionally needs a pointer/address operation
and a defined Go field/tag mapping, so a generic JSON object string-field adapter
is sufficient for this prototype.

A second probe `fn args () -> List String = Go.conv os.Args : List String`
reports E0507: cannot convert `Go.Slice String` to `List String`. Argument parsing
can still live in Gom using a count/index host primitive until slice/List copying
is implemented (P3-3).

## Additional executable finding: void Go call in tail position

```gom
package main
import go "os"
fn stop () -> Unit = os.Exit 1
fn main () -> Unit = ()
```

`gom build` succeeds but `go build` rejects the emitted `return os.Exit(tmp1)`
with `no value used as value`. Go zero-result calls must be represented separately
from a call that returns the Go representation of Gom Unit. The CLI temporarily
writes `ignore (os.Exit 1); ()` to avoid tail-position use.

The prototype uses a simple recursive `Files` ADT for its input queue and seen
filenames. `[x, ..rest]` is available as a pattern but rejected as a list literal
by the current parser, and list construction helpers are not yet in stdlib.

## Prototype and executable verification

The CLI is implemented in `examples/gist-cli/main.gom` (140 lines after
`gom fmt`). All Gist-specific behavior lives there: options, filename keys,
duplicate detection, text-file traversal, JSON object assembly, POST/PATCH route
selection, headers, response status, URL validation, and CLI error/output policy.
`host/host.go` is 79 lines of generic byte/JSON/HTTP/interface/argument primitives.
It contains no Gist route or request schema. This temporary boundary is adopted
until the independently tracked interop work can remove it.

`examples/gist-cli/build.sh /tmp/gist-cli-example-built` successfully compiles
the source with `gom build`, then compiles generated Go. Running `--help` prints
the documented usage. No real service is called by the build script.

The root-discovered integration suite compiles the real source with the compiler
pipeline, vets and builds the generated Go, then executes that binary against
local `httptest` servers with a dummy token. Embedding `main.gom` and `host/*.go`
tracks their changes in Go's test cache; direct compiler imports track compiler
implementation dependencies.

Verified cases:

- Create all specified files in one POST, exact Unicode/escape/empty contents
  and escaped filename keys, secret visibility, expected API/auth/media headers.
- Feed the returned URL to PATCH and verify omitted remote files remain intact.
- Gist URLs with/without an owner and a trailing slash.
- Input failures make zero requests: no files, missing files, directories, invalid
  UTF-8, duplicate basenames, bad URL/ID/query, missing/empty/repeated options,
  invalid endpoint, unknown option, literal dash filename, missing token.
- HTTP 401/422/500 and unexpected success status, malformed/trailing JSON,
  missing/null/non-string/invalid `html_url`, redirect refusal, transport failure.
- Help exits successfully without a token or an HTTP request.

`go test -race ./examples/gist-cli` passed. The first full-suite run found only
the expected stale generated decisions README; it is regenerated with the final
accepted decision. Full final checks are recorded below after regeneration.
No actual Gist was created, changed, or fetched.

Final verification after adopting the decision and regenerating its index:
`gofmt -l .` produced no output; `go vet ./...`, `go test ./...`,
`go test -race ./examples/gist-cli`, `gom fmt -check` on the example, and
`git diff --check` passed. The integration test also runs `go vet` on the
actual generated CLI before building and executing it.

## Independent review and implementation response

The fresh `review_gist_cli` agent reviewed `979e062` and reported no findings
requiring a change. The review confirmed the requested behavior, the Gom-owned
application policy and generic host boundary, execution of the generated CLI
against fake servers, and test-cache dependency tracking. Its formatting, vet,
full test suite, and diff checks passed.

The reviewer also independently rechecked the official
[GitHub Gists REST API documentation](https://docs.github.com/en/rest/gists/gists):
POST creates a Gist, PATCH updates the selected Gist, and the documented API
version header is `2026-03-10`. This check read documentation only; no live
Gist API request was made.

As the original implementer, I judge that no further change is required to this
branch: the independent review corroborates the executable results and API
contract, and identified compiler limitations are already explicit TODOs.
The separate Go field/interface implementation should subsequently remove the
corresponding host primitives and rerun this integration suite. That follow-up
does not block adopting this reviewed example. This response changes only the
verification report, so the full test suite was not rerun; `git diff --check`
passed.
