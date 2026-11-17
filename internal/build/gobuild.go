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
