package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// JSON-RPC 2.0 error codes used by the server (the protocol's own
// constants — LSP reserves -32700..-32600 and -32099..-32000).
const (
	codeParseError         = -32700
	codeInvalidRequest     = -32600
	codeMethodNotFound     = -32601
	codeInvalidParams      = -32602
	codeInternalError      = -32603
	codeServerNotInitiated = -32002
)

// maxMessageSize caps one framed message's body. LSP payloads are
// small — a full-document sync is still kilobytes — so anything
// larger is a corrupt or hostile header; the cap keeps a bogus
// Content-Length from sizing an OOM allocation.
const maxMessageSize = 64 << 20 // 64 MiB

// message is one incoming JSON-RPC envelope. A Method with an ID is
// a request, a Method without an ID is a notification, and an
// envelope without a Method is a response — the v1 server sends no
// server-to-client requests, so responses are ignored.
type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// isRequest reports whether m is a request the server must answer.
// An explicit `"id": null` counts as a request — discouraged by the
// spec but legal.
func (m *message) isRequest() bool { return m.Method != "" && m.ID != nil }

// response is one outgoing JSON-RPC response envelope. Result is the
// marshaled payload (nil when the response carries an Error).
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

// responseError is the error member of a response.
type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// notification is one outgoing server-to-client notification.
type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// resultResponse marshals v into a success response for id.
func resultResponse(id json.RawMessage, v any) *response {
	b, err := json.Marshal(v)
	if err != nil {
		return errResponse(id, codeInternalError, err.Error())
	}
	return &response{JSONRPC: "2.0", ID: id, Result: b}
}

// errResponse builds an error response for id.
func errResponse(id json.RawMessage, code int, msg string) *response {
	if id == nil {
		id = json.RawMessage("null")
	}
	return &response{JSONRPC: "2.0", ID: id,
		Error: &responseError{Code: code, Message: msg}}
}

// conn is one JSON-RPC connection over the LSP base protocol:
// a "Content-Length: N" header block followed by N bytes of JSON.
// Only Content-Length is significant; other headers are skipped.
type conn struct {
	r *bufio.Reader
	w io.Writer
}

// parseError wraps a JSON unmarshal failure so read callers can tell
// a malformed body (answer -32700 and keep serving) from a transport
// failure (stop serving).
type parseError struct{ err error }

func (e *parseError) Error() string { return "lsp: malformed JSON-RPC payload: " + e.err.Error() }
func (e *parseError) Unwrap() error { return e.err }

// read consumes one message. It returns io.EOF (or
// io.ErrUnexpectedEOF) when the stream ends before a message starts
// or mid-message — the caller treats both as a closed connection.
func (c *conn) read() (*message, error) {
	length := -1
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && line != "" {
				return nil, io.ErrUnexpectedEOF // unterminated header line
			}
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // blank line ends the header block
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue // header without a colon — ignored like unknown headers
		}
		if strings.EqualFold(strings.TrimSpace(k), "content-length") {
			length, err = strconv.Atoi(strings.TrimSpace(v))
			if err != nil || length < 0 {
				return nil, fmt.Errorf("lsp: bad Content-Length %q", strings.TrimSpace(v))
			}
			if length > maxMessageSize {
				// The body cannot be trusted or skipped — the only
				// honest answer is to end the connection.
				return nil, fmt.Errorf("lsp: Content-Length %d exceeds the %d-byte message cap",
					length, maxMessageSize)
			}
		}
	}
	if length < 0 {
		return nil, errors.New("lsp: message without Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return nil, err // EOF or ErrUnexpectedEOF — a closed stream
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, &parseError{err}
	}
	return &m, nil
}

// write emits one framed message.
func (c *conn) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("lsp: cannot marshal message: %w", err)
	}
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(b)); err != nil {
		return err
	}
	_, err = c.w.Write(b)
	return err
}
