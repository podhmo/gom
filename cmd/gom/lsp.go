package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/lsp"
)

// runLSP implements `gom lsp`: the LSP v1 server over stdio —
// didOpen/didChange/didClose, hover and pushed diagnostics
// (docs/decisions/2026-11-06-lsp-v1-scope.md). Each open document
// compiles alone with CheckOnly through the same loader-backed
// options gom check uses; the buffer text the client sends is what
// compiles, so unsaved edits are checked.
func runLSP(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lsp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	srv := lsp.New(func(path string, src []byte) (*build.Result, error) {
		return build.Compile(
			build.Input{Path: path, Src: src},
			compileOptions(input{path: path, src: src}, true))
	})
	if err := srv.Serve(os.Stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "gom lsp: %v\n", err)
		return diag.ExitInternal
	}
	// The exit-code contract of the LSP spec: 0 when shutdown
	// preceded exit, 1 otherwise — and the same shutdown rule
	// covers a stream EOF (the client went away without asking):
	// Serve treats EOF as a clean close of the read loop, but the
	// process still reports 1 since no shutdown was received.
	if srv.ShutdownReceived() {
		return 0
	}
	return 1
}
