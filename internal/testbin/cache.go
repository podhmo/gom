// Package testbin builds native binaries for compiler integration tests.
// Gom compilation, program execution, output and test verdicts are never cached.
package testbin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

type Result struct {
	Path         string
	Output       []byte
	ReuseEnabled bool
}

// Build obtains a native binary in dir without executing the generated
// program. Go 1.27's go run caches executables by action/content build ID,
// including toolchain, flags and dependency inputs. -trimpath removes the
// otherwise different scratch-directory paths from those inputs.
//
// The -exec adapter copies the executable into this case's own directory.
// Callers then run it directly, retaining their timeout, environment, exit
// classification and number of executions. No dependency model or binary
// store is maintained here; GOCACHE controls Go's ordinary cache location.
// Reuse is opt-in with GOM_TEST_BIN_CACHE=on. An empty value or "off"
// selects the original go build path. Without cp the
// same build fallback applies, so the integration tests remain runnable.
// GOM_TEST_BIN_CACHE=normalize isolates path normalization without retaining
// executables, for performance comparisons.
func Build(ctx context.Context, gobin, dir string, env []string) (Result, error) {
	if gobin == "" {
		gobin = "go"
	}
	path := filepath.Join(dir, "prog.bin")
	mode := os.Getenv("GOM_TEST_BIN_CACHE")
	if mode == "" {
		mode = "off"
	}
	args := []string{"build", "-o", path, "."}
	if mode != "off" {
		args = []string{"build", "-trimpath", "-buildvcs=false", "-o", path, "."}
	}
	reuse := false
	if mode != "off" && mode != "normalize" {
		if cp, err := exec.LookPath("cp"); err == nil {
			// go's -exec value is parsed with cmd/internal/quoted, not a shell.
			args = []string{"run", "-trimpath", "-buildvcs=false", "-exec", strconv.Quote(cp), ".", path}
			reuse = true
		}
	}
	cmd := exec.CommandContext(ctx, gobin, args...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	return Result{Path: path, Output: out, ReuseEnabled: reuse}, err
}
