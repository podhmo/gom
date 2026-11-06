package lsp

// The LSP wire types of the v1 slice (docs/decisions/2026-11-06-
// lsp-v1-scope.md). Field names follow the LSP specification;
// everything here is protocol shape only — compiler meaning enters
// through the server's conversion functions, never the other way.

// Position is LSP's zero-based line/character pair. Character counts
// UTF-16 code units — the only encoding the v1 server advertises.
type Position struct {
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

// Range is a start/end pair; End is exclusive, matching diag.Span's
// half-open convention.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// TextDocumentIdentifier names an open document.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier names an open document revision.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int32  `json:"version"`
}

// TextDocumentItem is the document payload of didOpen.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int32  `json:"version"`
	Text       string `json:"text"`
}

// DidOpenTextDocumentParams is the params of textDocument/didOpen.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// TextDocumentContentChangeEvent is one didChange event. The server
// advertises full sync, so only Text is meaningful — the whole new
// document text.
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// DidChangeTextDocumentParams is the params of textDocument/didChange.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// DidCloseTextDocumentParams is the params of textDocument/didClose.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// TextDocumentPositionParams is the params shape shared by
// textDocument/hover and other position requests.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// MarkupContent is a hover payload; Kind is "plaintext" or "markdown".
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Hover is the result of textDocument/hover — a marked-up string and
// the range it describes.
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// LSP diagnostic severities (the protocol's enumeration).
const (
	SeverityError       = 1
	SeverityWarning     = 2
	SeverityInformation = 3
	SeverityHint        = 4
)

// Diagnostic is the LSP wire form of one diag.Diagnostic.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Code     string `json:"code,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// PublishDiagnosticsParams is the params of
// textDocument/publishDiagnostics, the server's pushed diagnostics.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     int32        `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// InitializeParams is the params of initialize — only the fields the
// server consults: the markup kinds the client renders for hover.
type InitializeParams struct {
	Capabilities struct {
		TextDocument struct {
			Hover struct {
				ContentFormat []string `json:"contentFormat"`
			} `json:"hover"`
		} `json:"textDocument"`
	} `json:"capabilities"`
}

// InitializeResult is the result of the initialize request: the
// server's capabilities plus identity.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   ServerInfo         `json:"serverInfo"`
}

// ServerCapabilities is the v1 capability advertisement.
type ServerCapabilities struct {
	// PositionEncoding pins "utf-16" — the only coordinate unit the
	// conversions implement.
	PositionEncoding string                  `json:"positionEncoding"`
	TextDocumentSync TextDocumentSyncOptions `json:"textDocumentSync"`
	HoverProvider    bool                    `json:"hoverProvider"`
}

// TextDocumentSyncOptions describes the sync the server supports:
// open/close notifications and full-document changes (Change 1).
type TextDocumentSyncOptions struct {
	OpenClose bool `json:"openClose"`
	Change    int  `json:"change"`
}

// ServerInfo identifies the server in the initialize result.
type ServerInfo struct {
	Name string `json:"name"`
}

// LSP MessageType values — the subset window/logMessage uses.
const (
	MessageError   = 1
	MessageWarning = 2
	MessageInfo    = 3
	MessageLog     = 4
)

// LogMessageParams is the params of window/logMessage — the channel
// the server uses to report non-fatal internal trouble (a panic
// recovered at the notification boundary).
type LogMessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}
