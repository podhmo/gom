package main

// This file holds the `gom run` command body (spec section 12,
// docs/decisions/2026-11-18-gom-run.md): compile and write like
// `gom build`, link the single main package to a temp binary, then
// exec it. Built on `go build` rather than `go run` so the child's
// exit code and output pass through untouched.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
)

// runGomRun implements `gom run [-format F] [FILE|DIR ...] [-- ARGS]`.
// Arguments after "--" go to the program; everything before is flags
// and inputs like `gom build` (the same split `gom test` needs —
// flag parsing would swallow "--" and hand program args to
// expandInputs). The inputs must all land in one package directory
// and that package must be `package main`: `go run` has the same
// restriction and this command is its spelling.
//
// The spec-12 message stream appears only when compilation or the
// link step fails — on success stdout/stderr belong to the program
// alone (a trailing summary would corrupt piped program output) and
// the exit code is the program's own.
func runGomRun(args []string, stdout, stderr io.Writer) int {
	gomArgs, progArgs := args, []string(nil)
	if i := slices.Index(args, "--"); i >= 0 {
		gomArgs, progArgs = args[:i], args[i+1:]
	}
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "diagnostic format: json or text")
	if err := fs.Parse(gomArgs); err != nil {
		return flagError(err)
	}
	if err := checkFormat(*format); err != nil {
		fmt.Fprintf(stderr, "gom run: %v\n", err)
		return diag.ExitInternal
	}
	inPaths, err := expandInputs(fs.Args(), false)
	if err != nil {
		fmt.Fprintf(stderr, "gom run: %v\n", err)
		return diag.ExitInternal
	}
	inputs, err := readInputs(inPaths)
	if err != nil {
		fmt.Fprintf(stderr, "gom run: %v\n", err)
		return diag.ExitInternal
	}
	// One package directory: `go run` runs one package, and the
	// generated main must live where its module resolves.
	dirs := map[string]bool{}
	for _, in := range inputs {
		dirs[filepath.Dir(filepath.Clean(in.path))] = true
	}
	if len(dirs) != 1 {
		fmt.Fprintf(stderr, "gom run: inputs must belong to a single package directory (got %d)\n", len(dirs))
		return diag.ExitInternal
	}
	runDir := ""
	for d := range dirs {
		runDir = d
	}
	// Outside a module, synthesize the invocation-scoped scratch module
	// before dependency discovery so module-aware imports resolve under
	// gom.local/scratch. It is removed when this command returns.
	_, _, inModule := moduleOf(runDir)
	if !inModule {
		// A parent go.work otherwise captures every Go subprocess. The
		// scratch module is deliberately self-contained, so workspace
		// resolution is disabled for this invocation. cmd/gom is a
		// single-command process; tests already pin GOWORK=off for the
		// same isolation.
		restore := setEnv("GOWORK", "off")
		defer restore()
	}
	cleanupScratch, err := ensureScratchModule(runDir)
	if err != nil {
		fmt.Fprintf(stderr, "gom run: %v\n", err)
		return diag.ExitInternal
	}
	defer cleanupScratch()

	inputs, err = expandModuleLocalDeps(inputs)
	if err != nil {
		fmt.Fprintf(stderr, "gom run: %v\n", err)
		return diag.ExitInternal
	}

	rep := diag.Report{Files: inputNames(inputs)}
	srcs := map[string][]byte{}
	for _, in := range inputs {
		srcs[in.path] = in.src
	}
	// No incremental cache: the link step needs the compiled output
	// and a hit supplies none, matching runBuild's -go/-vet opt-out.
	results, err := compileInputs(inputs, false, nil, "", nil)
	if err != nil {
		fmt.Fprintf(stderr, "gom run: %v\n", err)
		return diag.ExitInternal
	}
	for _, r := range results {
		rep.Diagnostics = append(rep.Diagnostics, r.Diagnostics...)
	}
	failed := checkInterfaces(inputs, results, &rep, srcs, nil)

	// Write generated code beside the sources — the same contract
	// `gom build` and `gom test` have, so the package stays the
	// module-resolvable unit the go tool runs.
	var writeErrs []error
	mainDst := runDir
	hasMain := false
	for i, r := range results {
		if r == nil || r.Output == nil || failed[r] {
			continue
		}
		d, err := outputPath(inputs[i].path, "", len(inputs))
		if err != nil {
			writeErrs = append(writeErrs, err)
			continue
		}
		if err := writeGenerated(d, r.Output.Source); err != nil {
			writeErrs = append(writeErrs, fmt.Errorf("%s: %w", d, err))
			continue
		}
		rep.Outputs = append(rep.Outputs, d)
		if !inputs[i].auto {
			mainDst = d
			if isMainSource(r.Output.Source) {
				hasMain = true
			}
		}
	}
	for _, e := range writeErrs {
		rep.Diagnostics = append(rep.Diagnostics,
			diag.Error("E0003", "output could not be written").Set("error", e.Error()))
	}
	if len(writeErrs) > 0 {
		if err := emitReport(stdout, *format, rep, srcs); err != nil {
			fmt.Fprintf(stderr, "gom run: %v\n", err)
		}
		return rep.ExitCode()
	}
	if rep.ExitCode() != 0 {
		if err := emitReport(stdout, *format, rep, srcs); err != nil {
			fmt.Fprintf(stderr, "gom run: %v\n", err)
		}
		return rep.ExitCode()
	}
	if !hasMain {
		fmt.Fprintf(stderr, "gom run: %s is not a package main (no generated file declares `package main`)\n", mainDst)
		return diag.ExitInternal
	}

	// The position table folds `go build` diagnostics onto .gom
	// spans exactly like runGoTools.
	abs, err := filepath.Abs(runDir)
	if err != nil {
		abs = runDir
	}
	pos := build.NewGoPosIndex(abs, srcs)
	for i, r := range results {
		if r == nil || r.Output == nil {
			continue
		}
		pos.AddSourceMap(inputs[i].path, r.Output.Source, r.Output.SourceMap)
	}
	bin, run, err := build.RunGoRunBin("", abs)
	if err != nil {
		rep.Diagnostics = append(rep.Diagnostics,
			diag.Error("E0001", "go build could not run").
				From(diag.SourceGo).
				Set("tool", "go build").
				Set("error", err.Error()))
		if err := emitReport(stdout, *format, rep, srcs); err != nil {
			fmt.Fprintf(stderr, "gom run: %v\n", err)
		}
		return diag.ExitInternal
	}
	defer os.RemoveAll(filepath.Dir(bin))
	if run.Failed {
		rep.Diagnostics = append(rep.Diagnostics,
			build.GoToolDiagnostics(run.Lines(), "go build", pos)...)
		if err := emitReport(stdout, *format, rep, srcs); err != nil {
			fmt.Fprintf(stderr, "gom run: %v\n", err)
		}
		return rep.ExitCode()
	}

	// The program inherits the caller's working directory — the same
	// place `go run ./pkg` would run it — and its stdio and exit
	// code pass through unadorned.
	cmd := exec.Command(bin, progArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "gom run: cannot run %s: %v\n", bin, err)
		return diag.ExitInternal
	}
	// Forward process signals to the child like `go run` does —
	// without this a Ctrl-C kills `gom run` but leaves the program
	// running detached. Registering the signals also stops the
	// default action here: the parent survives the signal and
	// reports the child's own status (a signal death surfaces as
	// 128+signal below).
	sigc := make(chan os.Signal, 8)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM,
		syscall.SIGHUP, syscall.SIGQUIT)
	relay := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sigc:
				_ = cmd.Process.Signal(s)
			case <-relay:
				return
			}
		}
	}()
	err = cmd.Wait()
	close(relay)
	signal.Stop(sigc)
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			if code := ee.ExitCode(); code >= 0 {
				return code
			}
			// A signal-killed child has no exit code: ExitCode()
			// is -1, which os.Exit would truncate to 255. Report
			// the shell-conventional 128+signal instead.
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				return 128 + int(ws.Signal())
			}
			return diag.ExitInternal
		}
		fmt.Fprintf(stderr, "gom run: cannot run %s: %v\n", bin, err)
		return diag.ExitInternal
	}
	return 0
}
