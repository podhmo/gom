// Command gom is the CLI of the Gom compiler, which transpiles Gom
// sources to Go.
//
// This is a placeholder: it validates the subcommand and prints usage,
// but no command is implemented yet. Real functionality lives in the
// internal packages (see docs/design.md section 3) and is wired up here
// as the compiler grows — this command stays a thin layer over
// internal/build and internal/format.
package main

import (
	"fmt"
	"io"
	"os"
)

// usage is printed when gom is invoked without a valid subcommand.
const usage = `gom is the compiler for the Gom language.

Usage:

	gom <command> [arguments]

The commands are:

	build     compile .gom files and emit .go
	check     type-check without emitting code
	test      run tests written in .gom files
	fmt       format .gom files
	fix       apply safe suggested fixes
	gomi      check or generate .gomi interface files
	explain   explain an error code
	schema    print the JSON Schema of the message format

Run "gom <command> -h" for command-specific help (not implemented yet).
`

// subcommands are the commands gom will eventually support
// (docs/design.md section 3). They are declared now only so the CLI can
// validate its arguments and print usage.
var subcommands = []string{
	"build",
	"check",
	"test",
	"fmt",
	"fix",
	"gomi",
	"explain",
	"schema",
}

// isSubcommand reports whether arg names a known subcommand.
func isSubcommand(arg string) bool {
	for _, c := range subcommands {
		if arg == c {
			return true
		}
	}
	return false
}

// run executes the CLI with the given arguments (excluding the program
// name) and returns the process exit code. Output goes to stdout and
// stderr so tests can capture it.
//
// Exit codes follow the go tool's convention: 0 for success, 2 for usage
// errors and unimplemented commands. Once commands are implemented,
// compiler diagnostics will use the spec's codes (0 success, 1 diagnostic
// errors, 2 internal error — spec section 12).
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	}

	if !isSubcommand(args[0]) {
		fmt.Fprintf(stderr, "gom: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}

	fmt.Fprintf(stderr, "gom %s: not implemented yet\n", args[0])
	return 2
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
