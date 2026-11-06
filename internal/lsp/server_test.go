package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
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
	const uri = "file:///tmp/roundtrip.gom"
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
	const uri = "file:///tmp/utf16.gom"
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
	const uri = "file:///tmp/hover.gom"
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
	// outputs: init, publish, then one hover response per case, then shutdown.
	if len(outs) != 2+len(cases)+1 {
		t.Fatalf("got %d outputs, want %d", len(outs), 2+len(cases)+1)
	}
	for i, c := range cases {
		res := outs[2+i]["result"]
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
	const uri = "file:///tmp/plain.gom"
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
	hov := outs[2]
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
	const uri = "file:///tmp/miss.gom"
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
	const uri = "file:///tmp/close.gom"
	_, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uri, "package main\n\nlet z = \"a\" ++ 1\n", 1),
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
	boom := func(path string, src []byte) (*build.Result, error) {
		return nil, fmt.Errorf("boom")
	}
	const uri = "file:///tmp/boom.gom"
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
	const uri = "file:///tmp/post.gom"
	srv, outs := runSession(t, nil,
		requestMsg(1, "initialize", map[string]any{}),
		requestMsg(2, "shutdown", nil),
		requestMsg(3, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 0, "character": 0},
		}),
		didOpenMsg(uri, "package main\n\nlet z = \"a\" ++ 1\n", 1), // ignored — no publish
		didChangeMsg(uri, "package main\n", 2),                     // ignored
		requestMsg(4, "shutdown", nil),                             // also rejected
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
	// A compiler panic is funneled to compileErr: publish surfaces
	// it as an internal-error diagnostic and the session lives on.
	panics := func(path string, src []byte) (*build.Result, error) {
		panic("unreachable")
	}
	const uri = "file:///tmp/panic.gom"
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
	const uri = "file:///tmp/fmt.gom"
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
	contents := outs[2]["result"].(map[string]any)["contents"].(map[string]any)
	if contents["kind"] != "plaintext" ||
		contents["value"].(string) != "add : Int -> Int -> Int" {
		t.Fatalf("hover contents: %v", contents)
	}
}

func TestConvertDiagnostic(t *testing.T) {
	s := New(nil)
	src := []byte("ab\n")
	d := &document{
		uri:   "file:///x.gom",
		path:  "/x.gom",
		src:   src,
		lines: diag.NewLineIndex(src),
	}

	// A Go-source diagnostic with a note folds the note into the
	// message and maps source/severity.
	dg := diag.Error("E0001", "m").From(diag.SourceGo).AddNote("n1")
	got, ok := s.convertDiagnostic(d, dg)
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
	got, _ = s.convertDiagnostic(d, w)
	if got.Severity != SeverityWarning {
		t.Fatalf("warning severity: %+v", got)
	}
	if got.Range.Start.Line != 0 || got.Range.Start.Character != 1 ||
		got.Range.End.Character != 2 {
		t.Fatalf("warning range: %+v", got.Range)
	}

	// A span naming another file is dropped — single-file compiles
	// cannot produce it and publishing it here would misplace it.
	other := diag.Error("E0201", "o").At(diag.SpanIn("other.gom", 0, 1))
	if _, ok := s.convertDiagnostic(d, other); ok {
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
