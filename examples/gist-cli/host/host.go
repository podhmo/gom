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

// NewClient creates a client with a timeout and no automatic redirects.
// Remove when Gom can supply the Go CheckRedirect callback — Go function
// interop (a Gom callable passed as a Go func) is not decided, and the
// struct-literal surface alone cannot set a func-typed field. The Timeout
// field is arithmetic on time.Duration, which is also outside the current
// numerics surface; see docs/decisions/2026-11-17-struct-construction.md.
// Preserve both the 30-second timeout and http.ErrUseLastResponse behavior,
// not just client creation.
func NewClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
