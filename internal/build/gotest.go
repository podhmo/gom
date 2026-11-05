package build

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// GoTestEvent is one JSON object of `go test -json` output (go doc
// cmd/test2json). Only the fields gom consumes are decoded — Time is
// kept because a zero-value test duration (Elapsed) is meaningful.
type GoTestEvent struct {
	Action      string  `json:"Action"`
	Package     string  `json:"Package,omitempty"`
	Test        string  `json:"Test,omitempty"`
	Elapsed     float64 `json:"Elapsed,omitempty"`
	Output      string  `json:"Output,omitempty"`
	OutputType  string  `json:"OutputType,omitempty"`
	ImportPath  string  `json:"ImportPath,omitempty"`
	FailedBuild string  `json:"FailedBuild,omitempty"`
}

// GoTestRun is the outcome of one `go test -json` invocation.
type GoTestRun struct {
	// Events are the decoded stream in order — empty when go test
	// never reached the test binary (missing go.mod, flag errors),
	// in which case Stderr holds the tool's complaint.
	Events []GoTestEvent
	// Failed is go test's exit status: true on test or build
	// failures — an ordinary result, not an invocation error.
	Failed bool
	// Stderr is the tool's stderr, for reporting a run that produced
	// no events.
	Stderr string
}

// RunGoTest runs `go test -json` in dir and decodes the event stream.
// args carries extra `go test` flags — `-race`, `-run`, `-count` —
// appended verbatim after -json (`gom test`'s `--` passthrough,
// docs/decisions/2026-11-05-gom-test-go-test-flags.md). Test and
// build failures are ordinary results — they set GoTestRun.Failed,
// not the returned error. The error is non-nil only when go test
// could not be invoked at all (missing go binary, dead dir) or
// broke its output contract (undecodable stream) — the driver
// reports those as E0001 diagnostics with source "go", like a
// build failure.
func RunGoTest(gobin, dir string, args ...string) (*GoTestRun, error) {
	if gobin == "" {
		gobin = "go"
	}
	cmd := exec.Command(gobin, append([]string{"test", "-json"}, args...)...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
	default:
		return nil, fmt.Errorf("build: cannot run go test: %w", err)
	}
	run := &GoTestRun{
		Failed: cmd.ProcessState.ExitCode() != 0,
		Stderr: stderr.String(),
	}
	sc := bufio.NewScanner(&stdout)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var ev GoTestEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil, fmt.Errorf("build: go test -json: undecodable output line %d: %q", line, raw)
		}
		run.Events = append(run.Events, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("build: go test -json: reading output: %w", err)
	}
	return run, nil
}

// TestOutcome folds one Go test function's go test -json events into
// the fields gom's "test" message needs.
type TestOutcome struct {
	// Func is the Go test function name as go test reports it —
	// "TestRectArea", "TestX/sub" for subtests.
	Func string
	// OK is true on a "pass" action. A test that started but the
	// binary died before a terminal action reports OK false with
	// whatever output it produced.
	OK bool
	// Skipped is true on a "skip" action; Skipped implies OK.
	Skipped bool
	// Elapsed is the terminal action's seconds.
	Elapsed float64
	// Output is the test's captured output — assertion text, log
	// lines, panic traces — with go test's ===/--- framing removed.
	Output string
}

// GoTestResults reduces one package's go test -json stream into the
// pieces gom reports.
type GoTestResults struct {
	// Tests are the per-test outcomes in completion order; tests
	// that started but never finished — the test binary died mid-run
	// — are appended as failures in run order.
	Tests []TestOutcome
	// BuildFailed is true when the stream carried a build-fail event
	// — the package or its tests did not compile.
	BuildFailed bool
	// BuildOutput holds the build-error lines the build-output
	// events carried ("# pkg [pkg.test]" headers and "file:line:col:
	// msg" entries).
	BuildOutput []string
	// PackageOutput is output unattributed to a test — a panic in
	// TestMain or package init — framing lines removed.
	PackageOutput string
}

// FoldGoTest reduces events into GoTestResults.
func FoldGoTest(events []GoTestEvent) GoTestResults {
	var res GoTestResults
	// Per-test accumulation: outputs merge into the test's bucket so
	// a result reports everything the test wrote before its verdict.
	pending := map[string]*TestOutcome{}
	var order []string
	var pkg strings.Builder
	for _, ev := range events {
		switch {
		case ev.Action == "build-output":
			res.BuildOutput = append(res.BuildOutput, ev.Output)
		case ev.Action == "build-fail":
			res.BuildFailed = true
		case ev.Test == "":
			if ev.Action == "output" && ev.OutputType != "frame" {
				pkg.WriteString(ev.Output)
			}
		default:
			t := pending[ev.Test]
			if t == nil {
				t = &TestOutcome{Func: ev.Test}
				pending[ev.Test] = t
				order = append(order, ev.Test)
			}
			switch ev.Action {
			case "output":
				if ev.OutputType != "frame" {
					t.Output += ev.Output
				}
			case "pass":
				t.OK, t.Elapsed = true, ev.Elapsed
				res.Tests = append(res.Tests, *t)
				delete(pending, ev.Test)
			case "skip":
				t.OK, t.Skipped, t.Elapsed = true, true, ev.Elapsed
				res.Tests = append(res.Tests, *t)
				delete(pending, ev.Test)
			case "fail":
				t.Elapsed = ev.Elapsed
				res.Tests = append(res.Tests, *t)
				delete(pending, ev.Test)
			}
		}
	}
	// Tests left pending ran but never reached a terminal action —
	// the test binary died. They are failures with the output they
	// produced before dying.
	for _, name := range order {
		if t, ok := pending[name]; ok {
			res.Tests = append(res.Tests, *t)
		}
	}
	res.PackageOutput = pkg.String()
	return res
}
