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
	"strings"
	"time"
)

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
