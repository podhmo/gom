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
// (docs/decisions/2026-11-06-lsp-v1-scope.md). The server compiles
// at package granularity: each recompile hands the package's .gom
// members — open documents' buffers shadowing disk, unopened
// siblings read from disk — to build.CompileFiles with the same
// loader-backed CheckOnly options gom check uses. Options rebuild
// per compile (the members' own directory keys them), so a freshly
// edited sibling always loads as it stands on disk.
func runLSP(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lsp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	srv := lsp.New(func(ins []build.Input) ([]*build.Result, error) {
		if len(ins) == 0 {
			return nil, nil
		}
		return build.CompileFiles(ins,
			compileOptions(input{path: ins[0].Path}, true))
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
