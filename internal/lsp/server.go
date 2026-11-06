package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
)

// Compiler is the compile the server runs per package revision —
// inputs are the .gom members of one package, the standard LSP
// overlay shape: open documents contribute their buffer contents
// (unsaved edits compile), unopened members are read from disk.
// Results come back one per input, in input order. cmd/gom wires
// the loader-backed build.CompileFiles driver; tests inject stubs
// or a nil-options CompileFiles.
type Compiler func(inputs []build.Input) ([]*build.Result, error)

// document is one open text document: the buffer the editor sees
// (not necessarily what is on disk), its line index, and the latest
// compile result — kept for hover lookups after every recompile.
type document struct {
	uri     string // the client's URI, also the publishDiagnostics target
	path    string // file-path form — the name spans carry
	version int32
	src     []byte
	lines   *diag.LineIndex
	result  *build.Result // nil while no compile has completed
}

// member is one .gom source of a package unit: an open document's
// buffer shadowing the disk file, or a sibling read from disk whose
// diagnostics still publish (under a synthesized URI) so a broken
// unopened file cannot fail the package silently.
type member struct {
	path string    // the spelling spans carry
	src  []byte    // what the compile saw
	doc  *document // non-nil while the member is open
}

// unit is one gather+compile of a package: the members fed to the
// compiler in input order, the per-input results handed back, the
// scope the unit's published sets retract against, and the internal
// failure of the compile itself, if any.
type unit struct {
	scope   string
	members []member
	results []*build.Result
	err     error
}

// pubEntry records one pushed diagnostics set: the URI it went to
// and the unit scope that owns it, so a later compile of the same
// scope can retract a set whose file left the package.
type pubEntry struct {
	uri   string
	scope string
}

// Server serves the LSP v1 slice over one JSON-RPC connection.
// Messages are handled sequentially in read order — the model is a
// single-threaded request loop, so no locks and no cancellation.
type Server struct {
	compile  Compiler
	docs     map[string]*document
	inited   bool
	shutdown bool
	// published tracks every diagnostics set pushed this session,
	// keyed by member path — a file that leaves its package (closed
	// and deleted, or an unopened sibling that vanished) gets its
	// last set retracted with an empty publish.
	published map[string]pubEntry
	// markup is the hover payload kind negotiated at initialize —
	// "markdown" when the client offers it, "plaintext" otherwise.
	markup string
}

// New returns a Server using compile for each recompile. A nil
// compile falls back to a nil-options build.CompileFiles — builtins
// only, every import E0203 — useful for tests and for a server that
// never sees imports anyway.
func New(compile Compiler) *Server {
	if compile == nil {
		compile = func(inputs []build.Input) ([]*build.Result, error) {
			return build.CompileFiles(inputs, nil)
		}
	}
	return &Server{
		compile:   compile,
		docs:      map[string]*document{},
		published: map[string]pubEntry{},
		markup:    "markdown",
	}
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
		u := s.didOpen(p)
		return false, s.publishUnit(c, u)
	case "textDocument/didChange":
		var p DidChangeTextDocumentParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return false, nil
		}
		u := s.didChange(p)
		if u == nil {
			return false, nil
		}
		return false, s.publishUnit(c, u)
	case "textDocument/didClose":
		var p DidCloseTextDocumentParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return false, nil
		}
		uri := p.TextDocument.URI
		u := s.didClose(p)
		// Clear the client's squiggles for the closed document.
		// When the file stays a package member — still on disk —
		// the republish below replaces this with its disk-state
		// set: diagnostics belong to the file, not to the tab.
		if err := c.write(notification{JSONRPC: "2.0",
			Method: "textDocument/publishDiagnostics",
			Params: PublishDiagnosticsParams{URI: uri, Diagnostics: []Diagnostic{}}}); err != nil {
			return false, err
		}
		if u != nil {
			return false, s.publishUnit(c, u)
		}
		return false, nil
	default:
		// Unknown notifications — initialized, $/setTrace,
		// $/cancelRequest, methods outside v1 — are ignored.
		return false, nil
	}
}

// didOpen stores the document and compiles its unit.
func (s *Server) didOpen(p DidOpenTextDocumentParams) *unit {
	d := &document{
		uri:     p.TextDocument.URI,
		path:    uriPath(p.TextDocument.URI),
		version: p.TextDocument.Version,
		src:     []byte(p.TextDocument.Text),
	}
	s.docs[d.uri] = d
	return s.recompileFor(d)
}

// didChange replaces the document text with the last change event —
// the server advertises full sync, so each event's Text is the whole
// new buffer — and recompiles the document's unit. Nil when the
// document was never opened.
func (s *Server) didChange(p DidChangeTextDocumentParams) *unit {
	d := s.docs[p.TextDocument.URI]
	if d == nil || len(p.ContentChanges) == 0 {
		return nil
	}
	d.src = []byte(p.ContentChanges[len(p.ContentChanges)-1].Text)
	d.version = p.TextDocument.Version
	return s.recompileFor(d)
}

// didClose drops the document and recompiles its package without the
// overlay: remaining open members' diagnostics can flip when the
// buffer that shadowed the disk file goes away. Nil when the
// document was never open or names a singleton unit — then nothing
// depends on it and only the empty publish above is owed.
func (s *Server) didClose(p DidCloseTextDocumentParams) *unit {
	d := s.docs[p.TextDocument.URI]
	delete(s.docs, p.TextDocument.URI)
	if d == nil {
		return nil
	}
	// The caller's empty publish already covers the document's set —
	// drop the record so a same-scope retraction does not push a
	// second clear to the URI the client just closed. When the file
	// stays a package member (still on disk), the republish below
	// refills it under the synthesized URI.
	delete(s.published, d.path)
	if !isPackageMember(d.path) {
		return nil
	}
	dir := filepath.Dir(filepath.Clean(d.path))
	return s.recompileUnit("pkg:"+dir, s.packageMembers(dir))
}

// isPackageMember reports whether path can join a directory's
// package unit: it must look like a .gom file AND carry an absolute
// filesystem path — file:// URIs always resolve to one. The IsAbs
// half is load-bearing, not hygiene: uriPath passes non-file URIs
// through verbatim, and an opaque name like "untitled:scratch.gom"
// still ends in .gom while filepath.Dir of it is "." — without the
// guard the gather would glob the server's own working directory
// into the unit.
func isPackageMember(path string) bool {
	return strings.HasSuffix(path, ".gom") && filepath.IsAbs(path)
}

// recompileFor compiles the unit d belongs to: the package of its
// directory for an absolute .gom path, a singleton for anything
// else — a .gomi interface file is its own scope, and a non-absolute
// path (an untitled buffer's opaque name, a relative URI) has no
// real directory to gather siblings from.
func (s *Server) recompileFor(d *document) *unit {
	if !isPackageMember(d.path) {
		return s.recompileUnit("doc:"+d.path,
			[]member{{path: d.path, src: d.src, doc: d}})
	}
	dir := filepath.Dir(filepath.Clean(d.path))
	return s.recompileUnit("pkg:"+dir, s.packageMembers(dir))
}

// packageMembers gathers the .gom sources of dir into one unit's
// input list, sorted by member path: the directory's *.gom files
// with open documents' buffers shadowing disk contents, plus open
// .gom documents of dir that exist only in the editor (new unsaved
// files the glob cannot see). A sibling that vanishes between the
// glob and the read drops out of the unit.
func (s *Server) packageMembers(dir string) []member {
	open := map[string]*document{}
	for _, d := range s.docs {
		if !strings.HasSuffix(d.path, ".gom") {
			continue
		}
		key := filepath.Clean(d.path)
		if filepath.Dir(key) != dir {
			continue
		}
		open[key] = d
	}
	var ms []member
	names, _ := filepath.Glob(filepath.Join(dir, "*.gom"))
	for _, name := range names {
		if d := open[filepath.Clean(name)]; d != nil {
			ms = append(ms, member{path: d.path, src: d.src, doc: d})
			delete(open, filepath.Clean(name))
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		ms = append(ms, member{path: name, src: src})
	}
	for _, d := range open {
		ms = append(ms, member{path: d.path, src: d.src, doc: d})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].path < ms[j].path })
	return ms
}

// recompileUnit feeds the members to the compiler and hands each
// open member's document its own result — the typed file hover
// reads until the next unit compile.
func (s *Server) recompileUnit(scope string, ms []member) *unit {
	u := &unit{scope: scope, members: ms}
	if len(ms) == 0 {
		return u
	}
	inputs := make([]build.Input, len(ms))
	for i, m := range ms {
		inputs[i] = build.Input{
			Path:      m.path,
			Src:       m.src,
			Interface: build.IsInterfaceFile(m.path),
		}
	}
	results, err := s.guardedCompile(inputs)
	u.results, u.err = results, err
	for i, m := range ms {
		if m.doc == nil {
			continue
		}
		m.doc.lines = diag.NewLineIndex(m.doc.src)
		if i < len(results) {
			m.doc.result = results[i]
		} else {
			m.doc.result = nil
		}
	}
	return u
}

// guardedCompile runs the unit compile behind a panic boundary:
// a compiler panic (a checker gap tripping a house-style assert)
// becomes an ordinary compile error, which publish then surfaces as
// an "internal error" diagnostic — visible to the user without
// killing the session.
func (s *Server) guardedCompile(inputs []build.Input) (r []*build.Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			r, err = nil, fmt.Errorf("panic in compile: %v", p)
		}
	}()
	return s.compile(inputs)
}

// publishUnit pushes every member's diagnostics to the client:
// open documents under their own URI and version, unopened members
// under a synthesized file:// URI — the package's diagnostics
// belong to the files whose spans carry them, not to the document
// whose edit triggered the compile. A member with nothing to say
// and nothing pushed before stays silent; members of this same unit
// scope whose files left the package get their last set retracted
// with an empty publish.
func (s *Server) publishUnit(c *conn, u *unit) error {
	live := make(map[string]bool, len(u.members))
	for i := range u.members {
		m := &u.members[i]
		live[m.path] = true
		uri := memberURI(m)
		ds := s.memberDiagnostics(u, i)
		old, ok := s.published[m.path]
		if ok && old.uri != uri {
			// The member's set moved URIs — an editor-opened URI
			// replaced the synthesized one (or vice versa) — so
			// the stale slot clears before the new one fills, and
			// the record drops: an empty new set below would
			// otherwise leave the dead URI registered forever.
			if err := s.pushDiagnostics(c, old.uri, 0, nil); err != nil {
				return err
			}
			delete(s.published, m.path)
			ok = false
		}
		if !ok && len(ds) == 0 {
			continue // silent member staying silent
		}
		var version int32
		if m.doc != nil {
			version = m.doc.version
		}
		if err := s.pushDiagnostics(c, uri, version, ds); err != nil {
			return err
		}
		if len(ds) == 0 {
			delete(s.published, m.path)
		} else {
			s.published[m.path] = pubEntry{uri: uri, scope: u.scope}
		}
	}
	for path, e := range s.published {
		if e.scope == u.scope && !live[path] {
			if err := s.pushDiagnostics(c, e.uri, 0, nil); err != nil {
				return err
			}
			delete(s.published, path)
		}
	}
	return nil
}

// pushDiagnostics writes one publishDiagnostics notification; a nil
// diagnostics list marshals as the protocol's empty array.
func (s *Server) pushDiagnostics(c *conn, uri string, version int32, ds []Diagnostic) error {
	if ds == nil {
		ds = []Diagnostic{}
	}
	return c.write(notification{JSONRPC: "2.0",
		Method: "textDocument/publishDiagnostics",
		Params: PublishDiagnosticsParams{URI: uri, Version: version, Diagnostics: ds}})
}

// memberDiagnostics renders one member's share of the unit result:
// an internal compile failure lands on open documents only (it is a
// session-level fault, not package truth), while real diagnostics
// convert through the member's own line index — and only those
// whose span names the member; a stray span pointing at another
// file is that file's business, published with its own set.
func (s *Server) memberDiagnostics(u *unit, i int) []Diagnostic {
	m := &u.members[i]
	ds := []Diagnostic{}
	if u.err != nil {
		if m.doc != nil {
			ds = append(ds, Diagnostic{
				Severity: SeverityError,
				Source:   "gom",
				Message:  "internal error: " + u.err.Error(),
			})
		}
		return ds
	}
	var r *build.Result
	if i < len(u.results) {
		r = u.results[i]
	}
	if r == nil {
		return ds
	}
	var lines *diag.LineIndex
	if m.doc != nil {
		lines = m.doc.lines
	} else {
		lines = diag.NewLineIndex(m.src)
	}
	for _, dg := range r.Diagnostics {
		if ld, ok := s.convertDiagnostic(m.path, lines, dg); ok {
			ds = append(ds, ld)
		}
	}
	return ds
}

// memberURI is the publishDiagnostics target of a member: the open
// document's own URI, or a file:// URI synthesized from the
// member's path — good enough for the Problems list; a real
// didOpen supersedes it (publishUnit retracts the stale slot).
func memberURI(m *member) string {
	if m.doc != nil {
		return m.doc.uri
	}
	return (&url.URL{Scheme: "file", Path: m.path}).String()
}

// convertDiagnostic renders one diag.Diagnostic into its LSP form:
// the span resolves through the member's own line index into
// UTF-16 positions, notes fold into the message text, and a
// diagnostic without a location degrades to a point at the
// document's head. Diagnostics whose span names another file are
// dropped — they publish under that file's own URI, not this one.
func (s *Server) convertDiagnostic(path string, lines *diag.LineIndex, dg diag.Diagnostic) (Diagnostic, bool) {
	var rng Range
	if !dg.Span.IsZero() {
		if dg.Span.File != path {
			return Diagnostic{}, false
		}
		rng = lspRange(lines, dg.Span)
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
// source's line index — PosUTF16's 1-based units converted to
// LSP's 0-based line and UTF-16 character.
func lspRange(lines *diag.LineIndex, sp diag.Span) Range {
	return Range{
		Start: lspPosition(lines.PosUTF16(sp.Start)),
		End:   lspPosition(lines.PosUTF16(sp.End)),
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
	rng := lspRange(d.lines, t.sp)
	return &Hover{
		Contents: MarkupContent{Kind: s.markup, Value: text},
		Range:    &rng,
	}
}

// uriPath converts a file:// URI into the path form compile inputs
// carry — the name diagnostics attach to and, for absolute results,
// the name sibling discovery globs from. Non-file URIs pass through
// as opaque names ("untitled:scratch.gom" keeps its scheme-prefixed
// form, which still ends in .gom): they form singleton units because
// the package-unit path guards on filepath.IsAbs, not because the
// glob could never see them.
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
