package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
)

// readAll drains every framed message out of s.
func readAll(t *testing.T, s string) []*message {
	t.Helper()
	c := &conn{r: bufio.NewReader(strings.NewReader(s)), w: io.Discard}
	var out []*message
	for {
		m, err := c.read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out
			}
			t.Fatalf("read: %v", err)
		}
		out = append(out, m)
	}
}

func TestConnReadWrite(t *testing.T) {
	var buf bytes.Buffer
	c := &conn{w: &buf}
	if err := c.write(notification{JSONRPC: "2.0", Method: "m", Params: map[string]int{"x": 1}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "Content-Length: ") || !strings.Contains(got, "\r\n\r\n") {
		t.Fatalf("write produced unframed output: %q", got)
	}
	ms := readAll(t, got)
	if len(ms) != 1 || ms[0].Method != "m" {
		t.Fatalf("read = %+v", ms)
	}
}

func TestConnReadHeaderVariants(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	// Extra headers are skipped; Content-Length is case-insensitive.
	in := "content-type: application/vscode-jsonrpc; charset=utf-8\r\n" +
		"Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
	ms := readAll(t, in)
	if len(ms) != 1 || ms[0].Method != "initialize" {
		t.Fatalf("read = %+v", ms)
	}

	// A message without Content-Length is a framing error.
	c := &conn{r: bufio.NewReader(strings.NewReader("Content-Type: x\r\n\r\n{}"))}
	if _, err := c.read(); err == nil {
		t.Fatal("expected error for missing Content-Length")
	}
}

func TestConnReadErrors(t *testing.T) {
	// Malformed JSON is a parseError — recoverable, answers -32700.
	bad := "Content-Length: 3\r\n\r\n{x\n"
	c := &conn{r: bufio.NewReader(strings.NewReader(bad))}
	_, err := c.read()
	var pe *parseError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want parseError", err)
	}

	// A truncated body is a closed stream, not a parse error.
	trunc := "Content-Length: 30\r\n\r\n{\"jsonrpc\":\"2.0\""
	c = &conn{r: bufio.NewReader(strings.NewReader(trunc))}
	if _, err := c.read(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want ErrUnexpectedEOF", err)
	}
}

func TestConnReadOversized(t *testing.T) {
	// A Content-Length over the cap is a hard transport error — the
	// body cannot be skipped, so the connection is dead either way;
	// what must not happen is the allocation.
	huge := "Content-Length: " + strconv.Itoa(maxMessageSize+1) + "\r\n\r\n"
	c := &conn{r: bufio.NewReader(strings.NewReader(huge))}
	if _, err := c.read(); err == nil ||
		!strings.Contains(err.Error(), "cap") {
		t.Fatalf("err = %v, want the message-cap error", err)
	}
}

func TestResultResponseNull(t *testing.T) {
	// shutdown's result is an explicit JSON null, not an absent key.
	r := resultResponse(json.RawMessage("7"), nil)
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"result":null`) {
		t.Fatalf("response = %s, want result:null", b)
	}
}
