// Command gom is the CLI of the Gom compiler, which transpiles Gom
// sources to Go.
//
// build, check, dump, test, fmt, fix, info, explain, and schema are
// implemented; `gomi` reports its rename to `info`
// (docs/design.md section 3). The command stays a thin layer over
// internal/build, internal/format and internal/diag: cli.go holds
// flag parsing, file walking, and message-stream emission;
// tooling.go holds the fix/explain/schema bodies.
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
	dump      print a compiler phase's IR (developer tool)
	test      run tests written in .gom files
	fmt       format .gom files
	fix       apply the fixes diagnostics mark safe
	info      generate or update .gomi interface drafts
	explain   explain an error code
	schema    print the JSON Schema of the message format

Run "gom <command> -h" for command-specific help.
`

// run executes the CLI with the given arguments (excluding the program
// name) and returns the process exit code. Output goes to stdout and
// stderr so tests can capture it.
//
// Exit codes follow spec section 12: 0 for success, 1 when the source
// had error diagnostics, 2 for usage errors, unreadable inputs, and
// internal failures.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "build":
		return runBuild(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "dump":
		return runDump(args[1:], stdout, stderr)
	case "fmt":
		return runFmt(args[1:], stdout, stderr)
	case "fix":
		return runFix(args[1:], stdout, stderr)
	case "info":
		return runInfo(args[1:], stdout, stderr)
	case "explain":
		return runExplain(args[1:], stdout, stderr)
	case "schema":
		return runSchema(args[1:], stdout, stderr)
	case "test":
		return runTest(args[1:], stdout, stderr)
	case "gomi":
		fmt.Fprintln(stderr, `gom gomi: the command was renamed to "gom info"`)
		return 2
	}

	fmt.Fprintf(stderr, "gom: unknown command %q\n\n", args[0])
	fmt.Fprint(stderr, usage)
	return 2
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
