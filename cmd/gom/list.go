// runList implements `gom list`: prints the importable stdlib
// short names, one per line. The enumeration source is
// gomimport's stdlib path table — the same table `import "list"`
// resolves through, so the listing cannot drift from what the
// compiler accepts. `-external` instead lists the importable Gom
// packages visible from the working directory's module context —
// the main module plus the post-replace build list — walking the
// filesystem, so it stays opt-in.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/gomimport"
)

func runList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	external := fs.Bool("external", false, "list importable Gom packages instead of the stdlib short names")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "gom list: takes no arguments\n")
		return diag.ExitInternal
	}
	if *external {
		dir, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "gom list: %v\n", err)
			return diag.ExitInternal
		}
		paths, err := gomimport.ExternalPackages(dir, "")
		if err != nil {
			fmt.Fprintf(stderr, "gom list: %v\n", err)
			return diag.ExitInternal
		}
		for _, p := range paths {
			if _, err := fmt.Fprintln(stdout, p); err != nil {
				fmt.Fprintf(stderr, "gom list: %v\n", err)
				return diag.ExitInternal
			}
		}
		return diag.ExitSuccess
	}
	for _, name := range gomimport.StdlibShortNames() {
		if _, err := fmt.Fprintln(stdout, name); err != nil {
			fmt.Fprintf(stderr, "gom list: %v\n", err)
			return diag.ExitInternal
		}
	}
	return diag.ExitSuccess
}
