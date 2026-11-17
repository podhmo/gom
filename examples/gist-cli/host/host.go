// Package host is a temporary compatibility package for running this example
// with the current Gom compiler. It should shrink as Gom gains the required
// interop and library support; each definition documents its removal conditions.
// It contains no Gist-specific logic or command-line parsing. Removal conditions
// describe missing capabilities, not commitments to add them to the language.
package host

import (
	"net/http"
	"time"
)

// NewClient creates a client with a timeout and the supplied CheckRedirect
// callback. Remove when Gom can write the client construction itself — the
// Timeout field is arithmetic on time.Duration, which is outside the current
// numerics surface; see docs/decisions/2026-11-17-struct-construction.md.
// The CheckRedirect callback now comes from Gom via Go.func2 — preserve both
// the 30-second timeout and the caller-supplied redirect behavior, not just
// client creation.
func NewClient(checkRedirect func(*http.Request, []*http.Request) error) *http.Client {
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: checkRedirect}
}
