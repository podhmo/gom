# Gist upload CLI, written in Gom

This example takes all supplied text files and creates **one secret Gist**, or
adds/replaces those files in an existing Gist when `--gist URL` is supplied.
The only successful command output is the Gist's `html_url`, followed by a newline.
An update leaves unspecified remote files unchanged.

This is a larger executable language example: argument parsing, recursive ADTs,
record updates, polymorphic Result conversion, `try`, Go imports and methods,
file traversal, JSON object construction, HTTP request selection, and error
handling work together. It does not require a third-party Go dependency.

## Build and run the tests

Go 1.27+ is required. From the repository root:

```sh
examples/gist-cli/build.sh /tmp/gist-cli
/tmp/gist-cli --help
go test ./examples/gist-cli -v
go test ./...
```

The test compiles `main.gom` using the real Gom compiler pipeline, builds the generated Go
into a CLI, starts local `httptest` servers, and executes the CLI against them.
It uses a dummy token, overrides the API endpoint on every request, and never
contacts GitHub. It tests creation followed by an update, exact file contents,
remaining remote files, headers, URL output, duplicate names, unreadable/missing
inputs, invalid UTF-8, invalid URLs, HTTP/JSON failures, and redirects.
No actual Gist was created or updated while developing this example.

## CLI contract

```text
gist-cli [--api-url URL] [--gist URL] [--] FILE...
```

- `FILE...`: one or more UTF-8 text files; empty files are allowed. The basename
  of each path is the remote filename. Duplicate basenames are rejected before
  sending a request. Use `--` before a filename starting with `-`.
- No `--gist`: `POST /gists`, with `public: false`.
- `--gist https://gist.github.com/OWNER/ID`: `PATCH /gists/ID`; a URL without
  `OWNER` and a trailing slash are also accepted. IDs must be hexadecimal.
  The URL is parsed to identify the Gist; it is never fetched.
- `--api-url URL`: overrides the default `https://api.github.com` for a fake
  server or an API prefix. It is the destination that receives the token.
- `GITHUB_TOKEN`: required when uploading; the tests set a deliberately fake
  value. Help does not require a token.
- Exit 0: success, URL on stdout. Exit 1: input, I/O, transport, HTTP, or response
  error on stderr; no URL is printed. `--help` prints usage and exits 0.
- API errors include status and body. Creation expects 201 and update expects
  200. The response must be one JSON object containing a valid Gist `html_url`.
- Requests have a 30-second timeout, do not follow redirects, and read at most
  2 MiB of response text. These limits are example transport policy, not Gom
  language rules.

For illustration only, these commands would contact GitHub if run with a real
`GITHUB_TOKEN`; the test suite does not run them:

```sh
/tmp/gist-cli notes.txt snippet.go
/tmp/gist-cli --gist https://gist.github.com/OWNER/ID notes.txt
```

The official [Gists REST API](https://docs.github.com/en/rest/gists/gists)
defines the request/response shape. Investigation and compiler gaps are recorded
in [the prototype report](../../docs/sketch/2026-11-03-gist-cli-prototype.md).

## Current host boundary

`main.gom` contains all Gist-specific behavior. `host/host.go` provides small,
generic primitives for the operations Gom cannot yet express directly:

| Primitive | Current reason |
| --- | --- |
| Argument count/index | Go slice → List conversion and indexing are not implemented |
| Read UTF-8 file / response text | `byte`/`[]byte` are not exposed |
| Quote JSON string / decode string field | `encoding/json` needs byte slices and destination pointers |
| String reader as `io.Reader` | Go interface assignability is incomplete |
| Request header / response status | Go struct field access is not implemented |
| HTTP client / stderr writer | Constructing configured Go structs and naming an interface value |

The adapter has no Gist route, file schema, create/update decision, URL validation,
or CLI argument policy. The initial audit records the exact compiler diagnostics
and related TODOs so future interop work can remove these adapters. Generated
Go is kept in a temporary directory; the source of truth remains `main.gom`.
