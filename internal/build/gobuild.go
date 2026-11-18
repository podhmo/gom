package build

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GoBuildRun is one `go build` invocation's outcome.
type GoBuildRun struct {
	// Failed is go build's exit status: true on compile failures —
	// an ordinary result, not an invocation error.
	Failed bool
	// Stderr is the tool's raw stderr — `file:line:col: msg`
	// diagnostics GoToolDiagnostics folds.
	Stderr string
}

// Lines splits Stderr into the diagnostic lines GoToolDiagnostics
// consumes; empty stderr yields nil.
func (r *GoBuildRun) Lines() []string {
	s := strings.TrimRight(r.Stderr, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// RunGoBuild runs `go build` in dir and reports it like RunGoTest:
// compile failures are ordinary results (Failed + Stderr), and the
// error is non-nil only when go could not be invoked at all — the
// driver reports those as E0001 diagnostics with source "go".
// hasMain routes a main package's link step to a temp binary so the
// invocation leaves no artifact in the user's package directory.
func RunGoBuild(gobin, dir string, hasMain bool) (*GoBuildRun, error) {
	if gobin == "" {
		gobin = "go"
	}
	args := []string{"build"}
	var bin string
	if hasMain {
		tmp, err := os.MkdirTemp("", "gom-build-*")
		if err != nil {
			return nil, fmt.Errorf("build: cannot stage go build output: %w", err)
		}
		defer os.RemoveAll(tmp)
		bin = filepath.Join(tmp, "out")
		args = append(args, "-o", bin)
	}
	cmd := exec.Command(gobin, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
	default:
		return nil, fmt.Errorf("build: cannot run go build: %w", err)
	}
	return &GoBuildRun{
		Failed: cmd.ProcessState.ExitCode() != 0,
		Stderr: stderr.String(),
	}, nil
}

// RunGoVet runs `go vet` in dir and reports it like RunGoBuild:
// findings are ordinary results (Failed + Stderr), and the error
// is non-nil only when go could not be invoked at all. vet emits
// the same `file:line:col:` diagnostics as build — catastrophic
// output arrives as raw `vet:`-prefixed lines, which
// GoToolDiagnostics passes through unpositioned.
func RunGoVet(gobin, dir string) (*GoBuildRun, error) {
	if gobin == "" {
		gobin = "go"
	}
	cmd := exec.Command(gobin, "vet", ".")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
	default:
		return nil, fmt.Errorf("vet: cannot run go vet: %w", err)
	}
	return &GoBuildRun{
		Failed: cmd.ProcessState.ExitCode() != 0,
		Stderr: stderr.String(),
	}, nil
}

// RunGoRunBin builds the package in dir — a main package — into a
// fresh temp binary and returns its path for the caller to exec and
// remove (os.RemoveAll on the binary's dir). It is the `gom run`
// link step: unlike RunGoBuild the artifact survives because the
// caller runs it, and unlike `go run` the exit status stays the
// program's own — `go run` collapses any child failure to "exit
// status 1" while the built binary's code and stderr pass through
// untouched. Failure semantics match RunGoBuild: compile failures
// are ordinary results (Failed + Stderr), the error is non-nil only
// when the build could not be staged or invoked.
func RunGoRunBin(gobin, dir string) (string, *GoBuildRun, error) {
	if gobin == "" {
		gobin = "go"
	}
	tmp, err := os.MkdirTemp("", "gom-run-*")
	if err != nil {
		return "", nil, fmt.Errorf("run: cannot stage go build output: %w", err)
	}
	bin := filepath.Join(tmp, "out")
	cmd := exec.Command(gobin, "build", "-o", bin)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
	default:
		os.RemoveAll(tmp)
		return "", nil, fmt.Errorf("run: cannot run go build: %w", err)
	}
	return bin, &GoBuildRun{
		Failed: cmd.ProcessState.ExitCode() != 0,
		Stderr: stderr.String(),
	}, nil
}
