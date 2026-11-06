package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/gomimport"
	"github.com/podhmo/gom/internal/iface"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	gomstd "github.com/podhmo/gom/internal/stdlib"
)

// readOutputs parses every framed JSON-RPC message out of s into raw
// maps — the client's view of the server's whole output stream.
func readOutputs(t *testing.T, s string) []map[string]any {
	t.Helper()
	r := bufio.NewReader(strings.NewReader(s))
	var out []map[string]any
	for {
		length := -1
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return out
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if k, v, ok := strings.Cut(line, ":"); ok &&
				strings.EqualFold(strings.TrimSpace(k), "content-length") {
				length, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		if length < 0 {
			t.Fatal("output message without Content-Length")
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(r, body); err != nil {
			t.Fatalf("output body: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("output JSON: %v", err)
		}
		out = append(out, m)
	}
}

// runSession feeds msgs (each marshaled and framed) into a fresh
// Server and returns every message it wrote.
func runSession(t *testing.T, compile Compiler, msgs ...any) (*Server, []map[string]any) {
	t.Helper()
	var in bytes.Buffer
	for _, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		fmt.Fprintf(&in, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}
	var out bytes.Buffer
	srv := New(compile)
	if err := srv.Serve(&in, &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	return srv, readOutputs(t, out.String())
}

// didOpenMsg builds a didOpen notification for uri carrying text.
func didOpenMsg(uri, text string, version int32) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri": uri, "languageId": "gom",
				"version": version, "text": text,
			},
		},
	}
}

// didChangeMsg builds a full-sync didChange notification.
func didChangeMsg(uri, text string, version int32) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didChange",
		"params": map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": version},
			"contentChanges": []map[string]any{{"text": text}},
		},
	}
}

func requestMsg(id int, method string, params any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	}
}

// initializeMsg builds an initialize request; formats is the hover
// contentFormat list the client offers (nil: none).
func initializeMsg(id int, formats ...string) map[string]any {
	var fs []any
	for _, f := range formats {
		fs = append(fs, f)
	}
	return map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{
				"textDocument": map[string]any{
					"hover": map[string]any{"contentFormat": fs},
				},
			},
		},
	}
}

func notifyMsg(method string, params any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "method": method, "params": params,
	}
}

// tmpURI returns a file:// URI for name inside a fresh per-test
// directory. The server globs a document's directory for sibling
// .gom files, so every didOpen test needs a private directory —
// the real /tmp is not a fixture.
func tmpURI(t *testing.T, name string) (dir, uri string) {
	t.Helper()
	dir = t.TempDir()
	return dir, "file://" + filepath.Join(dir, name)
}

// writeFile drops src at path — a package member the glob sees.
func writeFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// publishes filters a session's output down to its
// publishDiagnostics notifications, as (uri, diagnostics) pairs.
func publishes(t *testing.T, outs []map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, m := range outs {
		if m["method"] == "textDocument/publishDiagnostics" {
			out = append(out, m)
		}
	}
	return out
}

// publishURI extracts the URI of a publishDiagnostics message.
func publishURI(t *testing.T, m map[string]any) string {
	t.Helper()
	params, ok := m["params"].(map[string]any)
	if !ok {
		t.Fatalf("publishDiagnostics params: %v", m["params"])
	}
	uri, _ := params["uri"].(string)
	return uri
}

// publishVersion extracts the optional version of a
// publishDiagnostics message as (v, ok): open documents publish
// their buffer version; members under a synthesized file:// URI
// carry no document version, so the omitempty field stays absent.
func publishVersion(t *testing.T, m map[string]any) (float64, bool) {
	t.Helper()
	params, ok := m["params"].(map[string]any)
	if !ok {
		t.Fatalf("publishDiagnostics params: %v", m["params"])
	}
	v, ok := params["version"].(float64)
	return v, ok
}

// diagnosticsOf extracts the diagnostics array of a
// publishDiagnostics message.
func diagnosticsOf(t *testing.T, m map[string]any) []any {
	t.Helper()
	if m["method"] != "textDocument/publishDiagnostics" {
		t.Fatalf("message is %v, want publishDiagnostics", m["method"])
	}
	params, ok := m["params"].(map[string]any)
	if !ok {
		t.Fatalf("publishDiagnostics params: %v", m["params"])
	}
	ds, ok := params["diagnostics"].([]any)
	if !ok {
		t.Fatalf("diagnostics: %v", params["diagnostics"])
	}
	return ds
}

func TestServeSessionRoundTrip(t *testing.T) {
	_, uri := tmpURI(t, "roundtrip.gom")
	bad := "package main\n\nfn add (x: Int) (y: Int) -> Int = x + y\n\nlet z = add 1 \"oops\"\n"
	good := "package main\n\nfn add (x: Int) (y: Int) -> Int = x + y\n\nlet z = add 1 2\n"

	_, outs := runSession(t, nil,
		initializeMsg(1, "markdown"),
		didOpenMsg(uri, bad, 1),
		requestMsg(2, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 4, "character": 9},
		}),
		didChangeMsg(uri, good, 2),
		requestMsg(3, "shutdown", nil),
		notifyMsg("exit", nil),
	)

	if len(outs) != 5 {
		t.Fatalf("got %d outputs: %v", len(outs), outs)
	}

	// 1: initialize result — capabilities advertise the v1 slice.
	init := outs[0]
	if init["id"] != float64(1) {
		t.Fatalf("initialize response id: %v", init["id"])
	}
	caps := init["result"].(map[string]any)["capabilities"].(map[string]any)
	if caps["hoverProvider"] != true || caps["positionEncoding"] != "utf-16" {
		t.Fatalf("capabilities: %v", caps)
	}
	sync := caps["textDocumentSync"].(map[string]any)
	if sync["openClose"] != true || sync["change"] != float64(1) {
		t.Fatalf("sync options: %v", sync)
	}

	// 2: didOpen pushed one E0401 diagnostic on the "oops" literal.
	ds := diagnosticsOf(t, outs[1])
	if len(ds) != 1 {
		t.Fatalf("diagnostics: %v", ds)
	}
	d0 := ds[0].(map[string]any)
	if d0["code"] != "E0401" || d0["severity"] != float64(SeverityError) || d0["source"] != "gom" {
		t.Fatalf("diagnostic: %v", d0)
	}
	rng := d0["range"].(map[string]any)
	start := rng["start"].(map[string]any)
	if start["line"] != float64(4) || start["character"] != float64(14) {
		t.Fatalf("diagnostic range: %v", rng)
	}

	// 3: hover on `add` reports its curried signature.
	hov := outs[2]
	if hov["id"] != float64(2) {
		t.Fatalf("hover response id: %v", hov["id"])
	}
	contents := hov["result"].(map[string]any)["contents"].(map[string]any)
	if contents["kind"] != "markdown" ||
		contents["value"].(string) != "```gom\nadd : Int -> Int -> Int\n```" {
		t.Fatalf("hover contents: %v", contents)
	}
	hr := hov["result"].(map[string]any)["range"].(map[string]any)
	if hr["start"].(map[string]any)["character"] != float64(8) ||
		hr["end"].(map[string]any)["character"] != float64(11) {
		t.Fatalf("hover range: %v", hr)
	}

	// 4: the didChange recompile cleared every diagnostic.
	ds = diagnosticsOf(t, outs[3])
	if len(ds) != 0 {
		t.Fatalf("diagnostics after fix: %v", ds)
	}

	// 5: shutdown answers with a JSON null result.
	if _, ok := outs[4]["result"]; !ok || outs[4]["result"] != nil {
		t.Fatalf("shutdown result: %v", outs[4])
	}
}

func TestServeUTF16Columns(t *testing.T) {
	// `let s = "😀" + 1` errors with the span covering the whole
	// binary expression. The 😀 inside it is a surrogate pair in
	// UTF-16 — two LSP columns where a code-point count gives one:
	// the end is character 16, not 15.
	_, uri := tmpURI(t, "utf16.gom")
	src := "package main\n\nlet s = \"😀\" + 1\n"

	_, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uri, src, 1),
	)
	if len(outs) != 2 {
		t.Fatalf("got %d outputs: %v", len(outs), outs)
	}
	ds := diagnosticsOf(t, outs[1])
	if len(ds) != 1 {
		t.Fatalf("diagnostics: %v", ds)
	}
	rng := ds[0].(map[string]any)["range"].(map[string]any)
	start := rng["start"].(map[string]any)
	end := rng["end"].(map[string]any)
	if start["line"] != float64(2) || start["character"] != float64(8) {
		t.Fatalf("UTF-16 range start: %v (want line 2, character 8)", rng)
	}
	if end["character"] != float64(16) {
		t.Fatalf("UTF-16 range end: %v (want character 16)", rng)
	}
}

func TestServeHoverVariants(t *testing.T) {
	_, uri := tmpURI(t, "hover.gom")
	src := "package main\n\n" +
		"type Point = { x: Float, y: Float }\n\n" +
		"type Shape = | Rect (w: Float) (h: Float) | Dot\n\n" +
		"let origin = { x = 0.0, y = 0.0 }\n\n" +
		"fn getx (r: Point) -> Float = r.x\n\n" +
		"fn area (s: Shape) -> Float =\n" +
		"  match s {\n" +
		"    Rect wr hr -> wr * hr\n" +
		"    Dot -> origin.x\n" +
		"  }\n"

	hover := func(line, char int) map[string]any {
		return requestMsg(9, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": line, "character": char},
		})
	}
	// at returns the 0-based LSP position of the nth occurrence of
	// sub, advanced by lead bytes (the source is ASCII here).
	at := func(t *testing.T, sub string, n, lead int) (int, int) {
		t.Helper()
		i := -1
		for range n + 1 {
			j := strings.Index(src[i+1:], sub)
			if j < 0 {
				t.Fatalf("%q occurrence %d not in source", sub, n)
			}
			i += j + 1
		}
		i += lead
		line := strings.Count(src[:i], "\n")
		col := i - (strings.LastIndex(src[:i], "\n") + 1)
		return line, col
	}

	sess := []any{
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uri, src, 1),
	}
	cases := []struct {
		name string
		sub  string
		n    int // 0-based occurrence index
		lead int // bytes to advance past the needle's start
		want string
	}{
		{"fn head", "area", 0, 0, "area : Shape -> Float"},
		{"type head", "Shape", 0, 0, "type Shape"},
		{"ctor head", "Rect", 0, 0, "Rect — variant of Shape"},
		{"let binder", "origin", 0, 0, "origin : Point"},
		{"record lit field", "x =", 0, 0, "x : Float"},
		{"field decl", "w:", 0, 0, "w : Float"},
		{"var use", "origin", 1, 0, "origin : Point"},
		{"pat binder", "hr", 0, 0, "hr : Float"},
		{"ctor pat", "Rect", 1, 0, "Rect — variant of Shape"},
		{"member sel", ".x", 1, 1, "x : Float"},
		{"expr type", "0.0", 0, 0, "Float"},
	}
	for _, c := range cases {
		line, char := at(t, c.sub, c.n, c.lead)
		sess = append(sess, hover(line, char))
	}
	sess = append(sess, requestMsg(99, "shutdown", nil), notifyMsg("exit", nil))

	_, outs := runSession(t, nil, sess...)
	// outputs: init, then one hover response per case, then
	// shutdown — the fixture is clean, so no publish fires.
	if len(outs) != 1+len(cases)+1 {
		t.Fatalf("got %d outputs, want %d", len(outs), 1+len(cases)+1)
	}
	for i, c := range cases {
		res := outs[1+i]["result"]
		if res == nil {
			t.Errorf("%s: hover result is null", c.name)
			continue
		}
		v := res.(map[string]any)["contents"].(map[string]any)["value"].(string)
		if !strings.Contains(v, c.want) {
			t.Errorf("%s: hover = %q, want substring %q", c.name, v, c.want)
		}
	}
}

func TestServeBeforeInitialize(t *testing.T) {
	_, outs := runSession(t, nil,
		requestMsg(1, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": "file:///x.gom"},
			"position":     map[string]any{"line": 0, "character": 0},
		}),
		notifyMsg("exit", nil),
	)
	if len(outs) != 1 {
		t.Fatalf("outputs: %v", outs)
	}
	errObj := outs[0]["error"].(map[string]any)
	if errObj["code"] != float64(codeServerNotInitiated) {
		t.Fatalf("error: %v", errObj)
	}
}

// A client offering only plaintext (eglot does) gets bare hover
// text — no fenced block. The spec default with no contentFormat
// stays markdown, covered by the other tests' bare initialize.
func TestServeHoverPlaintext(t *testing.T) {
	_, uri := tmpURI(t, "plain.gom")
	src := "package main\n\nfn add (x: Int) (y: Int) -> Int = x + y\n"
	_, outs := runSession(t, nil,
		initializeMsg(1, "plaintext"),
		didOpenMsg(uri, src, 1),
		requestMsg(2, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 2, "character": 4},
		}),
		notifyMsg("exit", nil),
	)
	// Clean file: no publish, so outs[1] is the hover response.
	hov := outs[1]
	contents := hov["result"].(map[string]any)["contents"].(map[string]any)
	if contents["kind"] != "plaintext" ||
		contents["value"].(string) != "add : Int -> Int -> Int" {
		t.Fatalf("hover contents: %v", contents)
	}
}

func TestServeUnknownAndMalformed(t *testing.T) {
	var in bytes.Buffer
	// A malformed payload first — the loop must survive it.
	in.WriteString("Content-Length: 3\r\n\r\n{x\n")
	for _, m := range []any{
		requestMsg(1, "initialize", map[string]any{}),
		requestMsg(2, "textDocument/completion", map[string]any{}),
		notifyMsg("exit", nil),
	} {
		b, _ := json.Marshal(m)
		fmt.Fprintf(&in, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}
	var out bytes.Buffer
	if err := New(nil).Serve(&in, &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	outs := readOutputs(t, out.String())
	if len(outs) != 3 {
		t.Fatalf("outputs: %v", outs)
	}
	if outs[0]["error"].(map[string]any)["code"] != float64(codeParseError) {
		t.Fatalf("first response: %v", outs[0])
	}
	if outs[1]["id"] != float64(1) {
		t.Fatalf("initialize response missing after parse error: %v", outs[1])
	}
	if outs[2]["error"].(map[string]any)["code"] != float64(codeMethodNotFound) {
		t.Fatalf("unknown method response: %v", outs[2])
	}
}

func TestServeHoverMisses(t *testing.T) {
	_, uri := tmpURI(t, "miss.gom")
	// A file that fails to parse leaves no typed AST — hover is null.
	_, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uri, "package main\n\nfn broken ( = \n", 1),
		requestMsg(2, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 2, "character": 4},
		}),
		requestMsg(3, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": "file:///never-opened.gom"},
			"position":     map[string]any{"line": 0, "character": 0},
		}),
		notifyMsg("exit", nil),
	)
	if len(outs) != 4 {
		t.Fatalf("outputs: %v", outs)
	}
	for i, idx := range []int{2, 3} {
		if outs[idx]["result"] != nil {
			t.Errorf("hover %d: result = %v, want null", i, outs[idx]["result"])
		}
	}
}

func TestServeDidCloseClears(t *testing.T) {
	_, uri := tmpURI(t, "close.gom")
	_, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uri, "package main\n\nlet z = \"a\" + 1\n", 1),
		notifyMsg("textDocument/didClose", map[string]any{
			"textDocument": map[string]any{"uri": uri},
		}),
		notifyMsg("exit", nil),
	)
	if len(outs) != 3 {
		t.Fatalf("outputs: %v", outs)
	}
	if ds := diagnosticsOf(t, outs[1]); len(ds) != 1 {
		t.Fatalf("open diagnostics: %v", ds)
	}
	if ds := diagnosticsOf(t, outs[2]); len(ds) != 0 {
		t.Fatalf("didClose did not clear diagnostics: %v", ds)
	}
}

func TestServeShutdownFlag(t *testing.T) {
	// exit without shutdown leaves ShutdownReceived false — the CLI
	// maps that to exit code 1.
	srv, _ := runSession(t, nil, notifyMsg("exit", nil))
	if srv.ShutdownReceived() {
		t.Fatal("ShutdownReceived true without shutdown request")
	}

	srv, _ = runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		requestMsg(2, "shutdown", nil),
		notifyMsg("exit", nil),
	)
	if !srv.ShutdownReceived() {
		t.Fatal("ShutdownReceived false after shutdown request")
	}
}

func TestServeCompileError(t *testing.T) {
	// An internal compile failure publishes a code-less diagnostic
	// at the document head rather than stalling the client.
	boom := func(inputs []build.Input) ([]*build.Result, error) {
		return nil, fmt.Errorf("boom")
	}
	_, uri := tmpURI(t, "boom.gom")
	_, outs := runSession(t, boom,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uri, "package main\n", 1),
		notifyMsg("exit", nil),
	)
	if len(outs) != 2 {
		t.Fatalf("outputs: %v", outs)
	}
	ds := diagnosticsOf(t, outs[1])
	if len(ds) != 1 {
		t.Fatalf("diagnostics: %v", ds)
	}
	d := ds[0].(map[string]any)
	if !strings.Contains(d["message"].(string), "boom") ||
		d["severity"] != float64(SeverityError) {
		t.Fatalf("diagnostic: %v", d)
	}
}

func TestServeUnknownNotification(t *testing.T) {
	// Notifications the v1 slice does not implement — initialized,
	// $/cancelRequest, didSave, anything else — are ignored and
	// produce no output at all.
	_, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		notifyMsg("initialized", map[string]any{}),
		notifyMsg("$/cancelRequest", map[string]any{"id": 9}),
		notifyMsg("textDocument/didSave", map[string]any{
			"textDocument": map[string]any{"uri": "file:///x.gom"},
		}),
		notifyMsg("exit", nil),
	)
	if len(outs) != 1 {
		t.Fatalf("outputs: %v, want only the initialize response", outs)
	}
}

func TestServeSecondInitialize(t *testing.T) {
	_, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		requestMsg(2, "initialize", map[string]any{}),
		notifyMsg("exit", nil),
	)
	if len(outs) != 2 {
		t.Fatalf("outputs: %v", outs)
	}
	if outs[0]["error"] != nil {
		t.Fatalf("first initialize errored: %v", outs[0])
	}
	if outs[1]["error"].(map[string]any)["code"] != float64(codeInvalidRequest) {
		t.Fatalf("second initialize: %v, want InvalidRequest", outs[1])
	}
}

func TestServeAfterShutdown(t *testing.T) {
	// After shutdown, requests answer InvalidRequest and
	// notifications are ignored — only exit still does something.
	_, uri := tmpURI(t, "post.gom")
	srv, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		requestMsg(2, "shutdown", nil),
		requestMsg(3, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 0, "character": 0},
		}),
		didOpenMsg(uri, "package main\n\nlet z = \"a\" + 1\n", 1), // ignored — no publish
		didChangeMsg(uri, "package main\n", 2),                    // ignored
		requestMsg(4, "shutdown", nil),                            // also rejected
		notifyMsg("exit", nil),
	)
	if len(outs) != 4 {
		t.Fatalf("outputs: %v", outs)
	}
	for _, idx := range []int{2, 3} {
		errObj, ok := outs[idx]["error"].(map[string]any)
		if !ok || errObj["code"] != float64(codeInvalidRequest) {
			t.Errorf("output %d: %v, want InvalidRequest", idx, outs[idx])
		}
	}
	if !srv.ShutdownReceived() {
		t.Fatal("ShutdownReceived false after shutdown")
	}
}

func TestServeDidChangeUnopened(t *testing.T) {
	// didChange on a URI that was never opened is ignored — no
	// publish, no error. A client racing its own didClose is not
	// worth crashing over.
	_, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		didChangeMsg("file:///never.gom", "package main\n", 1),
		notifyMsg("exit", nil),
	)
	if len(outs) != 1 {
		t.Fatalf("outputs: %v", outs)
	}
}

func TestServeCompilePanic(t *testing.T) {
	// A compiler panic becomes the unit's compile error: publish
	// surfaces it as an internal-error diagnostic and the session
	// lives on.
	panics := func(inputs []build.Input) ([]*build.Result, error) {
		panic("unreachable")
	}
	_, uri := tmpURI(t, "panic.gom")
	_, outs := runSession(t, panics,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uri, "package main\n", 1),
		requestMsg(2, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 0, "character": 0},
		}),
		requestMsg(3, "shutdown", nil),
		notifyMsg("exit", nil),
	)
	if len(outs) != 4 {
		t.Fatalf("outputs: %v", outs)
	}
	ds := diagnosticsOf(t, outs[1])
	if len(ds) != 1 ||
		!strings.Contains(ds[0].(map[string]any)["message"].(string), "panic in compile") {
		t.Fatalf("diagnostics: %v", ds)
	}
	if outs[2]["result"] != nil {
		t.Fatalf("hover after panic: %v, want null", outs[2])
	}
	if outs[3]["error"] != nil {
		t.Fatalf("shutdown after panic: %v", outs[3])
	}
}

func TestRequestPanicRecovered(t *testing.T) {
	// A request-side panic answers -32603 rather than escaping. The
	// document below is deliberately half-built (result set, lines
	// nil) so hover's d.lines.OffsetUTF16 panics on it.
	s := New(nil)
	s.inited = true
	s.docs["file:///x.gom"] = &document{
		uri:    "file:///x.gom",
		result: &build.Result{Typed: &check.File{}},
	}
	p, err := json.Marshal(map[string]any{
		"textDocument": map[string]any{"uri": "file:///x.gom"},
		"position":     map[string]any{"line": 0, "character": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := s.request(&message{
		ID:     json.RawMessage("7"),
		Method: "textDocument/hover",
		Params: p,
	})
	if resp == nil || resp.Error == nil || resp.Error.Code != codeInternalError {
		t.Fatalf("resp = %+v, want -32603", resp)
	}
}

func TestServeHoverUnknownFormats(t *testing.T) {
	// A contentFormat offer of only unknown kinds falls back to
	// plaintext — the one markup kind every client renders.
	_, uri := tmpURI(t, "fmt.gom")
	src := "package main\n\nfn add (x: Int) (y: Int) -> Int = x + y\n"
	_, outs := runSession(t, nil,
		initializeMsg(1, "toml", "yaml"),
		didOpenMsg(uri, src, 1),
		requestMsg(2, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 2, "character": 4},
		}),
		notifyMsg("exit", nil),
	)
	contents := outs[1]["result"].(map[string]any)["contents"].(map[string]any)
	if contents["kind"] != "plaintext" ||
		contents["value"].(string) != "add : Int -> Int -> Int" {
		t.Fatalf("hover contents: %v", contents)
	}
}

func TestConvertDiagnostic(t *testing.T) {
	s := New(nil)
	src := []byte("ab\n")
	lines := diag.NewLineIndex(src)

	// A Go-source diagnostic with a note folds the note into the
	// message and maps source/severity.
	dg := diag.Error("E0001", "m").From(diag.SourceGo).AddNote("n1")
	got, ok := s.convertDiagnostic("/x.gom", lines, dg)
	if !ok {
		t.Fatal("convertDiagnostic dropped a spanless diagnostic")
	}
	if got.Severity != SeverityError || got.Source != "go" ||
		got.Code != "E0001" || got.Message != "m\nn1" {
		t.Fatalf("converted: %+v", got)
	}
	if got.Range.Start.Line != 0 || got.Range.Start.Character != 0 {
		t.Fatalf("spanless range: %+v", got.Range)
	}

	// A warning maps to severity 2.
	w := diag.Warning("W0501", "w").At(diag.SpanIn("/x.gom", 1, 2))
	got, _ = s.convertDiagnostic("/x.gom", lines, w)
	if got.Severity != SeverityWarning {
		t.Fatalf("warning severity: %+v", got)
	}
	if got.Range.Start.Line != 0 || got.Range.Start.Character != 1 ||
		got.Range.End.Character != 2 {
		t.Fatalf("warning range: %+v", got.Range)
	}

	// A span naming another file is dropped — it publishes under
	// that file's own URI, not this one's.
	other := diag.Error("E0201", "o").At(diag.SpanIn("other.gom", 0, 1))
	if _, ok := s.convertDiagnostic("/x.gom", lines, other); ok {
		t.Fatal("other-file diagnostic was not dropped")
	}
}

func TestURIPath(t *testing.T) {
	for in, want := range map[string]string{
		"file:///tmp/x.gom":       "/tmp/x.gom",
		"file:///tmp/a%20b.gom":   "/tmp/a b.gom",
		"file://localhost/x.gom":  "/x.gom",
		"untitled:Untitled-1":     "untitled:Untitled-1", // opaque
		"file://host/share/x.gom": "//host/share/x.gom",
	} {
		if got := uriPath(in); got != want {
			t.Errorf("uriPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// diagCodes extracts the code strings of a publishDiagnostics
// message's diagnostics.
func diagCodes(t *testing.T, m map[string]any) []string {
	t.Helper()
	var codes []string
	for _, d := range diagnosticsOf(t, m) {
		codes = append(codes, d.(map[string]any)["code"].(string))
	}
	return codes
}

// Two members of one package — a.gom only exists in the editor (new
// unsaved file), b.gom is on disk — compile as one unit: a.gom sees
// b's top-level helper with no E0201, and both files' own errors
// publish under their own URIs, b's under the synthesized file://
// URI before the client ever opens it.
func TestServePackageTwoFiles(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.gom")
	bPath := filepath.Join(dir, "b.gom")
	bSrc := "package main\n\nfn helper (x: Int) -> Int = x\n\nlet wb = \"oops\" + 1\n"
	writeFile(t, bPath, bSrc)
	aSrc := "package main\n\nlet w = helper 1\n\nlet za = \"a\" + 1\n"
	uriA := "file://" + aPath
	uriB := "file://" + bPath

	var calls [][]build.Input
	rec := func(inputs []build.Input) ([]*build.Result, error) {
		calls = append(calls, append([]build.Input(nil), inputs...))
		return build.CompileFiles(inputs, nil)
	}
	_, outs := runSession(t, rec,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, aSrc, 1),
		didOpenMsg(uriB, bSrc, 1),
	)

	// The first compile already saw both members — the editor's a.gom
	// buffer and the on-disk b.gom.
	if len(calls) < 1 || len(calls[0]) != 2 {
		t.Fatalf("first compile inputs: %v", calls)
	}
	if got := string(calls[0][0].Src); got != aSrc {
		t.Fatalf("member a src = %q, want the editor buffer", got)
	}
	if got := string(calls[0][1].Src); got != bSrc {
		t.Fatalf("member b src = %q, want the disk file", got)
	}

	pubs := publishes(t, outs)
	if len(pubs) != 4 {
		t.Fatalf("publishes: %v", pubs)
	}
	// didOpen(a): a's own E0401 — and no E0201 on the sibling-declared
	// helper, the point of the package unit.
	if publishURI(t, pubs[0]) != uriA {
		t.Fatalf("first publish uri: %v", publishURI(t, pubs[0]))
	}
	if codes := diagCodes(t, pubs[0]); len(codes) != 1 || codes[0] != "E0401" {
		t.Fatalf("a's diagnostics: %v", codes)
	}
	// An open document's publish carries its buffer version.
	if v, ok := publishVersion(t, pubs[0]); !ok || v != 1 {
		t.Fatalf("a's publish version: %v %v", v, ok)
	}
	// b's own error publishes under its (synthesized) URI even though
	// the client never opened it — the error lands on the right file.
	if publishURI(t, pubs[1]) != uriB {
		t.Fatalf("second publish uri: %v", publishURI(t, pubs[1]))
	}
	if codes := diagCodes(t, pubs[1]); len(codes) != 1 || codes[0] != "E0401" {
		t.Fatalf("b's diagnostics: %v", codes)
	}
	// A synthesized-URI member has no document version — the field
	// stays absent rather than reporting a bogus 0.
	if _, ok := publishVersion(t, pubs[1]); ok {
		t.Fatalf("synthesized publish carried a version: %v", pubs[1])
	}
	// didOpen(b): both sets republish, b now under its client URI.
	if publishURI(t, pubs[2]) != uriA || publishURI(t, pubs[3]) != uriB {
		t.Fatalf("republish uris: %v %v", publishURI(t, pubs[2]), publishURI(t, pubs[3]))
	}
	if v, ok := publishVersion(t, pubs[3]); !ok || v != 1 {
		t.Fatalf("b's client-URI publish version: %v %v", v, ok)
	}
	for _, m := range pubs {
		for _, c := range diagCodes(t, m) {
			if c == "E0201" {
				t.Fatalf("E0201 leaked into %v: %v", publishURI(t, m), m)
			}
		}
	}
}

// A didChange on b.gom invalidates the whole package: a.gom's
// diagnostics flip from clean to E0201 without a.gom changing, then
// flip back when b's close restores the on-disk truth.
func TestServePackageChangeFlipsSibling(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.gom")
	bPath := filepath.Join(dir, "b.gom")
	aSrc := "package main\n\nlet z = helper 1\n"
	bGood := "package main\n\nfn helper (x: Int) -> Int = x\n"
	writeFile(t, aPath, aSrc)
	writeFile(t, bPath, bGood)
	uriA := "file://" + aPath
	uriB := "file://" + bPath

	_, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, aSrc, 1),
		didOpenMsg(uriB, bGood, 1),
		// The edit only lives in b's editor buffer — the disk file
		// still declares helper.
		didChangeMsg(uriB, "package main\n", 2),
		notifyMsg("textDocument/didClose", map[string]any{
			"textDocument": map[string]any{"uri": uriB},
		}),
	)

	pubs := publishes(t, outs)
	if len(pubs) != 3 {
		t.Fatalf("publishes: %v", pubs)
	}
	// didChange(b) republishes a with the E0201 its sibling's removed
	// declaration now causes — b itself stays silent.
	if publishURI(t, pubs[0]) != uriA {
		t.Fatalf("flip publish uri: %v", publishURI(t, pubs[0]))
	}
	if codes := diagCodes(t, pubs[0]); len(codes) != 1 || codes[0] != "E0201" {
		t.Fatalf("a's flipped diagnostics: %v", codes)
	}
	// didClose: the explicit clear for b's URI, then a republishes
	// empty — the disk's unedited b.gom declares helper again.
	if publishURI(t, pubs[1]) != uriB || len(diagnosticsOf(t, pubs[1])) != 0 {
		t.Fatalf("close publish: %v", pubs[1])
	}
	if publishURI(t, pubs[2]) != uriA || len(diagnosticsOf(t, pubs[2])) != 0 {
		t.Fatalf("a's restored diagnostics: %v", pubs[2])
	}
}

// didClose keeps the other members' sets: closing b leaves a's
// E0401 published — and b's own set persists as well, republished
// under its (now synthesized) URI, because the file on disk still
// has the error. Diagnostics belong to the file, not to the tab.
func TestServePackageCloseKeepsSets(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.gom")
	bPath := filepath.Join(dir, "b.gom")
	aSrc := "package main\n\nlet za = \"a\" + 1\n"
	bSrc := "package main\n\nlet zb = \"b\" + 1\n"
	writeFile(t, aPath, aSrc)
	writeFile(t, bPath, bSrc)
	uriA := "file://" + aPath
	uriB := "file://" + bPath

	_, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, aSrc, 1),
		didOpenMsg(uriB, bSrc, 1),
		notifyMsg("textDocument/didClose", map[string]any{
			"textDocument": map[string]any{"uri": uriB},
		}),
	)

	pubs := publishes(t, outs)
	if len(pubs) != 7 {
		t.Fatalf("publishes: %v", pubs)
	}
	last := pubs[len(pubs)-1]
	if publishURI(t, last) != uriB {
		t.Fatalf("last publish uri: %v", publishURI(t, last))
	}
	if codes := diagCodes(t, last); len(codes) != 1 || codes[0] != "E0401" {
		t.Fatalf("b's post-close set: %v", codes)
	}
	aLast := pubs[len(pubs)-2]
	if publishURI(t, aLast) != uriA {
		t.Fatalf("a's republish uri: %v", publishURI(t, aLast))
	}
	if codes := diagCodes(t, aLast); len(codes) != 1 || codes[0] != "E0401" {
		t.Fatalf("a's set after b closed: %v", codes)
	}
}

// Hover over a use whose declaration lives in a sibling file works
// because the package compiled as a unit — b.gom here is never even
// opened; the disk read was enough.
func TestServePackageHoverCrossFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "b.gom"),
		"package main\n\nfn helper (x: Int) -> Int = x\n")
	aPath := filepath.Join(dir, "a.gom")
	uriA := "file://" + aPath
	aSrc := "package main\n\nlet z = helper 1\n"

	_, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, aSrc, 1),
		requestMsg(2, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uriA},
			"position":     map[string]any{"line": 2, "character": 9},
		}),
		notifyMsg("exit", nil),
	)
	// Clean package: no publish — outs[1] is the hover response.
	if len(outs) != 2 {
		t.Fatalf("outputs: %v", outs)
	}
	res := outs[1]["result"]
	if res == nil {
		t.Fatal("cross-file hover returned null")
	}
	v := res.(map[string]any)["contents"].(map[string]any)["value"].(string)
	if !strings.Contains(v, "helper : Int -> Int") {
		t.Fatalf("hover = %q, want helper's signature", v)
	}
}

// A sibling with a syntax error degrades the package compile — the
// healthy a.gom simply has nothing to report — while the parse
// error itself lands on the broken file's URI.
func TestServePackageSiblingParseError(t *testing.T) {
	dir := t.TempDir()
	bPath := filepath.Join(dir, "b.gom")
	writeFile(t, bPath, "package main\n\nfn broken ( = \n")
	aPath := filepath.Join(dir, "a.gom")
	uriA := "file://" + aPath
	uriB := "file://" + bPath

	_, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, "package main\n\nlet z = helper 1\n", 1),
		requestMsg(2, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uriA},
			"position":     map[string]any{"line": 2, "character": 9},
		}),
		notifyMsg("exit", nil),
	)
	pubs := publishes(t, outs)
	if len(pubs) != 1 {
		t.Fatalf("publishes: %v", pubs)
	}
	if publishURI(t, pubs[0]) != uriB {
		t.Fatalf("parse error publish uri: %v", publishURI(t, pubs[0]))
	}
	codes := diagCodes(t, pubs[0])
	if len(codes) == 0 || !strings.HasPrefix(codes[0], "E01") {
		t.Fatalf("b's diagnostics: %v", codes)
	}
	// The unit aborted at parse: a's typed AST never materialized,
	// so hover has nothing to answer.
	if outs[len(outs)-1]["result"] != nil {
		t.Fatalf("hover in degraded package: %v", outs[len(outs)-1])
	}
}

// An unopened member's published set retracts when the disk file is
// fixed: a didChange on any member recompiles the package, sees the
// repaired sibling, and pushes an empty set over its stale one.
// Driven through the server's own methods so the disk write lands
// between the two compiles — a static message list cannot do that.
func TestServePackageSiblingRetracts(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.gom")
	bPath := filepath.Join(dir, "b.gom")
	aSrc := "package main\n\nlet z = 1\n"
	writeFile(t, aPath, aSrc)
	writeFile(t, bPath, "package main\n\nlet zb = \"b\" + 1\n")
	uriA := "file://" + aPath
	uriB := "file://" + bPath

	s := New(nil)
	s.inited = true
	var buf bytes.Buffer
	c := &conn{w: &buf}
	u := s.didOpen(DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uriA, Version: 1, Text: aSrc}})
	if err := s.publishUnit(c, u); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// b is fixed on disk between compiles — the server has no file
	// watching, so the next member-triggered recompile notices.
	writeFile(t, bPath, "package main\n\nlet zb = 2\n")
	u = s.didChange(DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uriA, Version: 2},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: aSrc + "\n"}}})
	if err := s.publishUnit(c, u); err != nil {
		t.Fatalf("publish: %v", err)
	}

	pubs := publishes(t, readOutputs(t, buf.String()))
	if len(pubs) != 2 {
		t.Fatalf("publishes: %v", pubs)
	}
	if publishURI(t, pubs[0]) != uriB || len(diagnosticsOf(t, pubs[0])) != 1 {
		t.Fatalf("b's first set: %v", pubs[0])
	}
	if publishURI(t, pubs[1]) != uriB || len(diagnosticsOf(t, pubs[1])) != 0 {
		t.Fatalf("b's retracted set: %v", pubs[1])
	}
}

// A member deleted from disk between compiles leaves the unit
// entirely: the set pushed under its name is retracted with an
// empty publish — the file stopped existing, not just stopped being
// broken. Same drive-by-method shape as the fix-up variant above.
func TestServePackageSiblingDeleteRetracts(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.gom")
	bPath := filepath.Join(dir, "b.gom")
	aSrc := "package main\n\nlet z = 1\n"
	writeFile(t, aPath, aSrc)
	writeFile(t, bPath, "package main\n\nlet zb = \"b\" + 1\n")
	uriA := "file://" + aPath
	uriB := "file://" + bPath

	s := New(nil)
	s.inited = true
	var buf bytes.Buffer
	c := &conn{w: &buf}
	u := s.didOpen(DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uriA, Version: 1, Text: aSrc}})
	if err := s.publishUnit(c, u); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// b.gom vanishes from disk — the next member-triggered recompile
	// gathers a package that no longer contains it.
	if err := os.Remove(bPath); err != nil {
		t.Fatalf("remove: %v", err)
	}
	u = s.didChange(DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uriA, Version: 2},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: aSrc + "\n"}}})
	if err := s.publishUnit(c, u); err != nil {
		t.Fatalf("publish: %v", err)
	}

	pubs := publishes(t, readOutputs(t, buf.String()))
	if len(pubs) != 2 {
		t.Fatalf("publishes: %v", pubs)
	}
	if publishURI(t, pubs[0]) != uriB || len(diagnosticsOf(t, pubs[0])) != 1 {
		t.Fatalf("b's first set: %v", pubs[0])
	}
	if publishURI(t, pubs[1]) != uriB || len(diagnosticsOf(t, pubs[1])) != 0 {
		t.Fatalf("b's ghost-retraction set: %v", pubs[1])
	}
	if _, ok := s.published[bPath]; ok {
		t.Fatal("deleted member still registered in published")
	}
}

// A member whose pushed URI changes spelling has the stale slot
// cleared before the new one fills: here b.gom's diagnostics first
// publish under the synthesized file:/// URI (unopened member),
// then the client opens it under a file://localhost spelling — a
// different URI for the same path.
func TestServePackageURIRespell(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.gom")
	bPath := filepath.Join(dir, "b.gom")
	aSrc := "package main\n\nlet z = 1\n"
	bSrc := "package main\n\nlet zb = \"b\" + 1\n"
	writeFile(t, aPath, aSrc)
	writeFile(t, bPath, bSrc)
	uriA := "file://" + aPath
	synthB := (&url.URL{Scheme: "file", Path: bPath}).String()
	localB := "file://localhost" + bPath
	if synthB == localB {
		t.Fatalf("test needs distinct spellings, got %q", synthB)
	}

	srv, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, aSrc, 1),
		didOpenMsg(localB, bSrc, 1),
	)
	pubs := publishes(t, outs)
	if len(pubs) != 3 {
		t.Fatalf("publishes: %v", pubs)
	}
	// didOpen(a): b's set goes out under the synthesized URI.
	if publishURI(t, pubs[0]) != synthB || len(diagnosticsOf(t, pubs[0])) != 1 {
		t.Fatalf("b's synthesized publish: %v", pubs[0])
	}
	// didOpen(b under the localhost spelling): the stale slot is
	// cleared first…
	if publishURI(t, pubs[1]) != synthB || len(diagnosticsOf(t, pubs[1])) != 0 {
		t.Fatalf("synthesized-slot clear: %v", pubs[1])
	}
	// …then the same set fills under the client's own URI.
	if publishURI(t, pubs[2]) != localB || len(diagnosticsOf(t, pubs[2])) != 1 {
		t.Fatalf("b's client-URI publish: %v", pubs[2])
	}
	if got := srv.published[bPath].uri; got != localB {
		t.Fatalf("registered uri = %q, want %q", got, localB)
	}
}

// A document whose path is not an absolute .gom file path forms a
// singleton unit: an opaque name that happens to end in .gom
// ("untitled:scratch.gom") has no directory to gather, and a .gomi
// interface is its own scope even inside a real package directory.
// The CWD bait matters — before the filepath.IsAbs guard, the
// opaque name's filepath.Dir "." globbed the server's working
// directory into the unit.
func TestServePackageSingletonUnits(t *testing.T) {
	bait := t.TempDir()
	writeFile(t, filepath.Join(bait, "bait.gom"),
		"package main\n\nlet bait = \"b\" + 1\n")
	t.Chdir(bait)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sib.gom"),
		"package main\n\nlet sib = \"s\" + 1\n")

	var calls [][]build.Input
	rec := func(inputs []build.Input) ([]*build.Result, error) {
		calls = append(calls, append([]build.Input(nil), inputs...))
		return build.CompileFiles(inputs, nil)
	}
	_, outs := runSession(t, rec,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg("untitled:scratch.gom", "package main\n\nlet z = 1\n", 1),
		didOpenMsg("file://"+filepath.Join(dir, "i.gomi"),
			"package main\n\nfn f (x: Int) -> Int\n", 1),
		notifyMsg("textDocument/didClose", map[string]any{
			"textDocument": map[string]any{"uri": "untitled:scratch.gom"},
		}),
	)
	if len(calls) != 2 {
		t.Fatalf("compiles = %d, want 2 (one per open, none on close)", len(calls))
	}
	for i, c := range calls {
		if len(c) != 1 {
			t.Fatalf("compile %d inputs = %v, want singleton", i, c)
		}
	}
	// No bait or sibling ever compiled, so nothing published under
	// their names — the only publish is didClose's empty set for the
	// closed untitled URI.
	for _, m := range outs {
		if m["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		if u := publishURI(t, m); strings.HasSuffix(u, "bait.gom") ||
			strings.HasSuffix(u, "sib.gom") {
			t.Fatalf("diagnostics leaked to %v", u)
		}
	}
}

// Rough cost model for the per-change session: each recompile builds
// fresh compileOptions (new goimport/gomimport sessions) and a new
// CompileFiles run, so every `import go` re-runs `go list -export`
// behind the importer. This test logs wall time per recompile so the
// numbers land in the verification sketch. Run with -v and without
// -short — it spawns the real go toolchain.
func TestServePackageCompileCost(t *testing.T) {
	if testing.Short() {
		t.Skip("cost measurement runs the real go toolchain")
	}
	dir := t.TempDir()
	// One member with a Go import (the `go list` path), one without.
	writeFile(t, filepath.Join(dir, "a.gom"),
		"package main\n\nimport go \"fmt\"\n\nfn main () -> Unit = {\n  ignore fmt.Println \"x\"\n  ()\n}\n")
	writeFile(t, filepath.Join(dir, "b.gom"),
		"package main\n\nfn helper (x: Int) -> Int = x\n")
	uriA := "file://" + filepath.Join(dir, "a.gom")

	// lspCompileOptions replicates cmd/gom's compileOptions for one
	// package compile — one goimport+gomimport session pair per call,
	// exactly what the CLI wires into the server.
	lspCompileOptions := func(firstPath string) *build.Options {
		d := filepath.Dir(firstPath)
		if abs, err := filepath.Abs(d); err == nil {
			d = abs
		}
		gl := goimport.New(&goimport.Config{Dir: d})
		mods := map[string]map[string]*resolve.Symbol{}
		ml := gomimport.New(&gomimport.Config{Dir: d, Go: gl, Sigs: gl, Modules: mods})
		return gomstd.Apply(&build.Options{
			Loader:         ml,
			Sigs:           ml,
			Modules:        mods,
			Lower:          &lower.Env{GoSig: gl.GoSigOf, Types: ml, FuncArity: ml.ArityOf},
			Exports:        iface.ExportNames,
			SelfImportPath: ml.EntryPath(),
			CheckOnly:      true,
		})
	}
	var times []time.Duration
	timed := func(inputs []build.Input) ([]*build.Result, error) {
		start := time.Now()
		rs, err := build.CompileFiles(inputs, lspCompileOptions(inputs[0].Path))
		times = append(times, time.Since(start))
		return rs, err
	}

	_, outs := runSession(t, timed,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, "package main\n\nimport go \"fmt\"\n\nfn main () -> Unit = {\n  ignore fmt.Println \"x\"\n  ()\n}\n", 1),
		didChangeMsg(uriA, "package main\n\nimport go \"fmt\"\n\nfn main () -> Unit = {\n  ignore fmt.Println \"y\"\n  ()\n}\n", 2),
		didChangeMsg(uriA, "package main\n\nimport go \"fmt\"\n\nfn main () -> Unit = {\n  ignore fmt.Println \"z\"\n  ()\n}\n", 3),
	)
	if len(outs) != 1 {
		t.Fatalf("outputs: %v", outs)
	}
	if len(times) != 3 {
		t.Fatalf("compiles: %d", len(times))
	}
	t.Logf("package recompile wall times (fresh options, import go): open=%v change1=%v change2=%v",
		times[0], times[1], times[2])

	// Same shape without the import — isolates the `go list` share.
	times = times[:0]
	writeFile(t, filepath.Join(dir, "a.gom"),
		"package main\n\nfn main () -> Unit = ()\n")
	_, outs = runSession(t, timed,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, "package main\n\nfn main () -> Unit = ()\n", 1),
		didChangeMsg(uriA, "package main\n\nfn main () -> Unit = { () }\n", 2),
	)
	if len(outs) != 1 || len(times) != 2 {
		t.Fatalf("second session: outs=%v times=%d", outs, len(times))
	}
	t.Logf("package recompile wall times (fresh options, no imports): open=%v change=%v",
		times[0], times[1])
}
