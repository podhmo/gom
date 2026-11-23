// runList implements `gom list`: prints the importable stdlib
// short names, one per line. The enumeration source is
// gomimport's stdlib path table — the same table `import "list"`
// resolves through, so the listing cannot drift from what the
// compiler accepts.
package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/gomimport"
)

func runList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "gom list: takes no arguments\n")
		return diag.ExitInternal
	}
	for _, name := range gomimport.StdlibShortNames() {
		if _, err := fmt.Fprintln(stdout, name); err != nil {
			fmt.Fprintf(stderr, "gom list: %v\n", err)
			return diag.ExitInternal
		}
	}
	return diag.ExitSuccess
}
