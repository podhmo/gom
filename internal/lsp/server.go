package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
)

// Compiler is the compile the server runs per document revision —
// path is the file name diagnostics attach to and src the buffer
// contents. cmd/gom wires the loader-backed driver; tests inject
// stubs or a nil-options build.Compile.
type Compiler func(path string, src []byte) (*build.Result, error)

// document is one open text document: the buffer the editor sees
// (not necessarily what is on disk), its line index, and the latest
// compile result — kept for hover lookups after every recompile.
type document struct {
	uri        string // the client's URI, also the publishDiagnostics target
	path       string // file-path form — the name spans carry
	version    int32
	src        []byte
	lines      *diag.LineIndex
	result     *build.Result // nil while no compile has completed
	compileErr error         // internal failure of the last compile, if any
}

// Server serves the LSP v1 slice over one JSON-RPC connection.
// Messages are handled sequentially in read order — the model is a
// single-threaded request loop, so no locks and no cancellation.
type Server struct {
	compile  Compiler
	docs     map[string]*document
	inited   bool
	shutdown bool
	// markup is the hover payload kind negotiated at initialize —
	// "markdown" when the client offers it, "plaintext" otherwise.
	markup string
}

// New returns a Server using compile for each recompile. A nil
// compile falls back to a nil-options build.Compile — builtins only,
// every import E0203 — useful for tests and for a server that never
// sees imports anyway.
func New(compile Compiler) *Server {
	if compile == nil {
		compile = func(path string, src []byte) (*build.Result, error) {
			return build.Compile(build.Input{Path: path, Src: src}, nil)
		}
	}
	return &Server{compile: compile, docs: map[string]*document{}, markup: "markdown"}
}

// ShutdownReceived reports whether the shutdown request arrived
// before Serve returned — the `exit` exit-code contract (0 with
// shutdown, 1 without) is for the CLI wrapper to apply.
func (s *Server) ShutdownReceived() bool { return s.shutdown }

// Serve reads JSON-RPC messages from r and writes responses and
// notifications to w until the exit notification or the stream's
// end. A malformed payload answers -32700 and the loop continues;
// any write or framing failure ends the loop with the error.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	c := &conn{r: bufio.NewReader(r), w: w}
	for {
		m, err := c.read()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil // client closed the stream
			}
			var pe *parseError
			if errors.As(err, &pe) {
				if werr := c.write(errResponse(nil, codeParseError, pe.Error())); werr != nil {
					return werr
				}
				continue
			}
			return err
		}
		switch {
		case m.isRequest():
			if err := c.write(s.request(m)); err != nil {
				return err
			}
		case m.Method != "":
			stop, err := s.notify(c, m)
			if err != nil {
				return err
			}
			if stop {
				return nil
			}
		default:
			// A response to a request this server never sends —
			// dropped silently.
		}
	}
}

// request answers one request message. A panic in the handler — a
// checker gap tripping a house-style panic("unreachable"), a hover
// bug — is recovered into a -32603 InternalError reply rather than
// letting one bad request kill the server process.
func (s *Server) request(m *message) (resp *response) {
	defer func() {
		if r := recover(); r != nil {
			resp = errResponse(m.ID, codeInternalError,
				fmt.Sprintf("lsp: panic handling %s: %v", m.Method, r))
		}
	}()
	if !s.inited && m.Method != "initialize" {
		return errResponse(m.ID, codeServerNotInitiated,
			"the server has not been initialized")
	}
	if s.shutdown {
		// Per the LSP spec, requests after shutdown are rejected —
		// InvalidRequest, not silent service.
		return errResponse(m.ID, codeInvalidRequest,
			"request after shutdown: "+m.Method)
	}
	switch m.Method {
	case "initialize":
		if s.inited {
			return errResponse(m.ID, codeInvalidRequest,
				"the server is already initialized")
		}
		var p InitializeParams
		// Params the server does not read are ignored; a malformed
		// body just leaves the defaults.
		_ = json.Unmarshal(m.Params, &p)
		s.inited = true
		// contentFormat is the client's ordered preference list —
		// take the first kind the server renders. An absent list
		// keeps the server-chosen default (markdown; the spec does
		// not pin a hover default). A list of only unknown kinds
		// falls back to plaintext, the one kind every client renders.
		if fs := p.Capabilities.TextDocument.Hover.ContentFormat; len(fs) > 0 {
			s.markup = "plaintext"
			for _, k := range fs {
				if k == "markdown" || k == "plaintext" {
					s.markup = k
					break
				}
			}
		}
		return resultResponse(m.ID, InitializeResult{
			Capabilities: ServerCapabilities{
				PositionEncoding: "utf-16",
				TextDocumentSync: TextDocumentSyncOptions{OpenClose: true, Change: 1},
				HoverProvider:    true,
			},
			ServerInfo: ServerInfo{Name: "gom"},
		})
	case "shutdown":
		s.shutdown = true
		return resultResponse(m.ID, nil)
	case "textDocument/hover":
		var p TextDocumentPositionParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return errResponse(m.ID, codeInvalidParams, err.Error())
		}
		return resultResponse(m.ID, s.hover(p))
	default:
		return errResponse(m.ID, codeMethodNotFound,
			"method not found: "+m.Method)
	}
}

// notify handles one notification message. The result reports
// whether the loop should stop (the exit notification) and the first
// write failure. A panic in the handler — outside the compile
// boundary guardedCompile already covers — is reported to the
// client's log (window/logMessage) and the loop keeps serving; the
// offending message is dropped.
func (s *Server) notify(c *conn, m *message) (stop bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			stop = false
			err = c.write(notification{JSONRPC: "2.0",
				Method: "window/logMessage",
				Params: LogMessageParams{
					Type:    MessageError,
					Message: fmt.Sprintf("gom lsp: panic handling %s: %v", m.Method, r),
				}})
		}
	}()
	switch m.Method {
	case "exit":
		return true, nil
	}
	if !s.inited {
		// Notifications before initialize are dropped — the client
		// never had a chance to open a document.
		return false, nil
	}
	if s.shutdown {
		// Notifications after shutdown are ignored per spec — only
		// exit (handled above) still means something.
		return false, nil
	}
	switch m.Method {
	case "textDocument/didOpen":
		var p DidOpenTextDocumentParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return false, nil
		}
		s.didOpen(p)
		return false, s.publish(c, p.TextDocument.URI)
	case "textDocument/didChange":
		var p DidChangeTextDocumentParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return false, nil
		}
		s.didChange(p)
		return false, s.publish(c, p.TextDocument.URI)
	case "textDocument/didClose":
		var p DidCloseTextDocumentParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return false, nil
		}
		uri := p.TextDocument.URI
		delete(s.docs, uri)
		// Clear the client's squiggles for the closed document.
		return false, c.write(notification{JSONRPC: "2.0",
			Method: "textDocument/publishDiagnostics",
			Params: PublishDiagnosticsParams{URI: uri, Diagnostics: []Diagnostic{}}})
	default:
		// Unknown notifications — initialized, $/setTrace,
		// $/cancelRequest, methods outside v1 — are ignored.
		return false, nil
	}
}

// didOpen stores the document and compiles it.
func (s *Server) didOpen(p DidOpenTextDocumentParams) {
	d := &document{
		uri:     p.TextDocument.URI,
		path:    uriPath(p.TextDocument.URI),
		version: p.TextDocument.Version,
		src:     []byte(p.TextDocument.Text),
	}
	s.docs[d.uri] = d
	s.recompile(d)
}

// didChange replaces the document text with the last change event —
// the server advertises full sync, so each event's Text is the whole
// new buffer — and recompiles.
func (s *Server) didChange(p DidChangeTextDocumentParams) {
	d := s.docs[p.TextDocument.URI]
	if d == nil || len(p.ContentChanges) == 0 {
		return
	}
	d.src = []byte(p.ContentChanges[len(p.ContentChanges)-1].Text)
	d.version = p.TextDocument.Version
	s.recompile(d)
}

// recompile refreshes the document's line index and compile result.
func (s *Server) recompile(d *document) {
	d.lines = diag.NewLineIndex(d.src)
	r, err := s.guardedCompile(d.path, d.src)
	d.result = r
	d.compileErr = err
}

// guardedCompile runs the document compile behind a panic boundary:
// a compiler panic (a checker gap tripping a house-style assert)
// becomes an ordinary compile error, which publish then surfaces as
// an "internal error" diagnostic — visible to the user without
// killing the session.
func (s *Server) guardedCompile(path string, src []byte) (r *build.Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			r, err = nil, fmt.Errorf("panic in compile: %v", p)
		}
	}()
	return s.compile(path, src)
}

// publish pushes the document's diagnostics to the client.
func (s *Server) publish(c *conn, uri string) error {
	d := s.docs[uri]
	if d == nil {
		return nil
	}
	ds := []Diagnostic{}
	if d.compileErr != nil {
		// An internal compile failure is transport-visible, not a
		// compiler diagnostic — reported without a code at the
		// document's head.
		ds = append(ds, Diagnostic{
			Severity: SeverityError,
			Source:   "gom",
			Message:  "internal error: " + d.compileErr.Error(),
		})
	}
	if d.result != nil {
		for _, dg := range d.result.Diagnostics {
			if ld, ok := s.convertDiagnostic(d, dg); ok {
				ds = append(ds, ld)
			}
		}
	}
	return c.write(notification{JSONRPC: "2.0",
		Method: "textDocument/publishDiagnostics",
		Params: PublishDiagnosticsParams{URI: uri, Version: d.version, Diagnostics: ds}})
}

// convertDiagnostic renders one diag.Diagnostic into its LSP form:
// the span resolves through the document's own line index into
// UTF-16 positions, notes fold into the message text, and a
// diagnostic without a location degrades to a point at the
// document's head. Diagnostics whose span names another file are
// dropped — single-file compiles cannot produce them, and pushing
// them under this document's URI would misplace them.
func (s *Server) convertDiagnostic(d *document, dg diag.Diagnostic) (Diagnostic, bool) {
	var rng Range
	if !dg.Span.IsZero() {
		if dg.Span.File != d.path {
			return Diagnostic{}, false
		}
		rng = s.lspRange(d, dg.Span)
	}
	sev := SeverityInformation
	switch dg.Severity {
	case diag.SeverityError:
		sev = SeverityError
	case diag.SeverityWarning:
		sev = SeverityWarning
	case diag.SeverityNote:
		sev = SeverityInformation
	}
	msg := dg.Message
	for _, n := range dg.Notes {
		msg += "\n" + n
	}
	src := "gom"
	if dg.Source == diag.SourceGo {
		src = "go"
	}
	return Diagnostic{
		Range:    rng,
		Severity: sev,
		Code:     string(dg.Code),
		Source:   src,
		Message:  msg,
	}, true
}

// lspRange resolves a .gom byte span to an LSP range through the
// document's line index — PosUTF16's 1-based units converted to
// LSP's 0-based line and UTF-16 character.
func (s *Server) lspRange(d *document, sp diag.Span) Range {
	return Range{
		Start: lspPosition(d.lines.PosUTF16(sp.Start)),
		End:   lspPosition(d.lines.PosUTF16(sp.End)),
	}
}

// lspPosition converts a resolved Pos to the 0-based LSP form.
func lspPosition(p diag.Pos) Position {
	return Position{
		Line:      uint32(max(p.Line-1, 0)),
		Character: uint32(max(p.Col-1, 0)),
	}
}

// hover answers textDocument/hover: the innermost typed node's
// rendered type under the position, or nil.
func (s *Server) hover(p TextDocumentPositionParams) *Hover {
	d := s.docs[p.TextDocument.URI]
	if d == nil || d.result == nil || d.result.Typed == nil {
		return nil
	}
	off := d.lines.OffsetUTF16(
		int(p.Position.Line)+1, int(p.Position.Character)+1)
	t := findHover(d.result.Typed, off)
	if t == nil {
		return nil
	}
	text := t.text
	if s.markup == "markdown" {
		text = "```gom\n" + text + "\n```"
	}
	rng := s.lspRange(d, t.sp)
	return &Hover{
		Contents: MarkupContent{Kind: s.markup, Value: text},
		Range:    &rng,
	}
}

// uriPath converts a file:// URI into the path form compile inputs
// carry — the name diagnostics attach to. Non-file URIs pass through
// as opaque names: a document's path is only a span identity in this
// in-memory model, never a filesystem promise.
func uriPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	p := u.Path
	if u.Host != "" && u.Host != "localhost" {
		p = "//" + u.Host + p
	}
	return p
}
