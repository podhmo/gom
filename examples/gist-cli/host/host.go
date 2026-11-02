// Package host is a temporary compatibility package for running this example
// with the current Gom compiler. It should shrink as Gom gains the required
// interop and library support; each definition documents its removal conditions.
// It contains no Gist-specific logic or command-line parsing. Removal conditions
// describe missing capabilities, not commitments to add them to the language.
package host

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// ArgumentCount returns the number of arguments after the executable name.
// Remove when Gom can measure Go slices, or convert os.Args to a List and
// traverse it in the argument parser (TODO P3-3). The replacement must still
// exclude os.Args[0]; no particular slice-access API has been decided.
func ArgumentCount() int { return len(os.Args) - 1 }

// Argument returns an argument using an index that excludes the executable name.
// Remove when Gom can index Go slices, or parse a List converted from os.Args
// (TODO P3-3). List traversal can replace both argument helpers together; Go
// slice indexing itself need not become part of Gom for that approach.
func Argument(index int) string { return os.Args[index+1] }

// ReadText reads a file and rejects content that is not valid UTF-8.
// Remove when Gom can call os.ReadFile and utf8.Valid and convert the resulting
// byte slice to String, preserving read failures and invalid-UTF-8 rejection.
// The current v1 spec excludes Go byte/[]byte, so enabling them needs a separate
// decision. Slice/List work alone (TODO P3-3) is not sufficient to remove this
// helper.
func ReadText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("%s: file is not UTF-8 text", path)
	}
	return string(data), nil
}

// Quote encodes a string as a JSON string, including its surrounding quotes.
// Remove when Gom can use json.Marshal plus byte-to-String conversion, or a
// standard JSON string encoder. A Gom JSON API has not been decided. Preserve
// JSON escaping; Go string quoting is not an interchangeable replacement.
func Quote(value string) string {
	data, _ := json.Marshal(value) // strings always have a JSON representation.
	return string(data)
}

// StringField decodes one JSON object and extracts a nonempty string field.
// Remove when a chosen JSON binding lets Gom perform this decoding and lookup.
// A direct encoding/json replacement needs byte/map access and writable decoding
// targets (for example, an addressable destination); that binding and destination
// construction are not yet decided. Preserve rejection of trailing JSON data,
// missing fields, non-string values, and empty strings when moving this to Gom.
func StringField(document, key string) (string, error) {
	var object map[string]json.RawMessage
	dec := json.NewDecoder(strings.NewReader(document))
	if err := dec.Decode(&object); err != nil {
		return "", err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return "", fmt.Errorf("response has trailing JSON data")
	}
	raw, ok := object[key]
	if !ok {
		return "", fmt.Errorf("response has no %s field", key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("response %s must be a string: %w", key, err)
	}
	if value == "" {
		return "", fmt.Errorf("response %s is empty", key)
	}
	return value, nil
}

// NewClient creates a client with a timeout and no automatic redirects.
// Remove when Gom can configure a Go struct and supply the Go CheckRedirect
// callback, or an HTTP binding provides equivalent configuration. Go struct
// construction/writes remain open in docs/open-questions.md; Go callback interop
// is also unavailable, with no replacement API decided. Preserve both the
// 30-second timeout and http.ErrUseLastResponse behavior, not just client creation.
func NewClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// ReadResponse reads bounded response text and closes Body on every return path.
// Remove when Gom can use io.LimitReader/io.ReadAll and byte-to-String conversion,
// and the read/limit/error paths plus Body cleanup can move into Gom. Response
// field access and Reader interface assignment already work; byte I/O remains
// unsupported and the cleanup structure has not been chosen. Preserve the 2 MiB
// limit and closing Body on failures; adding a language-level defer is not a
// prerequisite if explicit cleanup can express the same behavior.
func ReadResponse(response *http.Response) (string, error) {
	defer response.Body.Close()
	const limit = 2 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return "", err
	}
	if len(data) > limit {
		return "", fmt.Errorf("response exceeds %d bytes", limit)
	}
	return string(data), nil
}
