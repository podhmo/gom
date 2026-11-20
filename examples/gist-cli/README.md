# Gist upload CLI, written in Gom

This example takes all supplied text files and creates **one secret Gist**, or
adds/replaces those files in an existing Gist when `--gist URL` is supplied.
The only successful command output is the Gist's `html_url`, followed by a newline.
An update leaves unspecified remote files unchanged.

This is a larger executable language example: a multi-file package, list
patterns, sum types with payloads, record updates, polymorphic Result
conversion, `try`, Gom `test` blocks, Go imports and methods, Go maps for JSON,
regexp captures, HTTP request construction, and error handling work together.
It does not require a third-party Go dependency.

## Build and run the tests

Go 1.27+ is required. From the repository root:

```sh
examples/gist-cli/build.sh /tmp/gist-cli
/tmp/gist-cli --help
go test ./examples/gist-cli -v
go test ./...
```

The test compiles every `.gom` file of this directory as one package using the
real Gom compiler pipeline, runs the Gom `test` blocks, builds the generated Go
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

## Layout

The program is one Gom package split along the operation tree
(`parse → load → build request → send → decode → validate`):

| File | Responsibility | Knows about Gists? |
| --- | --- | --- |
| `main.gom` | `main`/`run` orchestration, exit status | only through `gist.gom` |
| `args.gom` | `Command`/`Options`, argument grammar, usage | the `--gist` URL |
| `files.gom` | basenames, duplicate check, UTF-8 reads | no |
| `gist.gom` | `Target`, routes, headers, body, status, `html_url` check | yes |
| `transport.gom` | `HttpRequest`/`HttpResponse`, client policy, bounded read | no |
| `json.gom` | one-object, nonempty string-field decoding | no |

Pure parts (argument grammar, duplicate detection, URL parsing, request body,
JSON field decoding) carry `test` blocks next to their definitions; process
and network behaviour is covered by `integration_test.go`.

There is no host package. The HTTP client is built in Gom with `Ptr.new` plus
field writes, the zero-value-construction pattern chosen over Go composite
literals (docs/decisions/2026-11-19-struct-field-write.md); the redirect
callback is `Go.func2`; the request body is a `Go.Map` passed to
`json.Marshal`. Generated Go is kept in a temporary directory; the source of
truth remains the `.gom` files. Findings from restructuring the example are in
[the refactoring report](../../docs/sketch/ja/2026-11-21-gist-cli-refactor.md);
the history of the former host adapter is in
[the prototype report](../../docs/sketch/2026-11-03-gist-cli-prototype.md) and
[the HTTP boundary verification](../../docs/sketch/2026-11-03-go-http-boundary-verification.md).
