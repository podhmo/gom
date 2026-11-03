package build

import (
	"strings"
	"testing"
)

// TestFoldGoTest folds the event stream of an ordinary run — pass,
// fail with assertion output, skip — checking verdicts, elapsed, and
// that ===/--- framing is stripped from outputs.
func TestFoldGoTest(t *testing.T) {
	events := []GoTestEvent{
		{Action: "start", Package: "shapes"},
		{Action: "run", Package: "shapes", Test: "TestRectArea"},
		{Action: "output", Package: "shapes", Test: "TestRectArea", Output: "=== RUN   TestRectArea\n", OutputType: "frame"},
		{Action: "output", Package: "shapes", Test: "TestRectArea", Output: "--- PASS: TestRectArea (0.00s)\n", OutputType: "frame"},
		{Action: "pass", Package: "shapes", Test: "TestRectArea", Elapsed: 0.001},
		{Action: "run", Package: "shapes", Test: "TestFails"},
		{Action: "output", Package: "shapes", Test: "TestFails", Output: "=== RUN   TestFails\n", OutputType: "frame"},
		{Action: "output", Package: "shapes", Test: "TestFails", Output: "    area.gom:18: gom: assertion failed:\n", OutputType: "error"},
		{Action: "output", Package: "shapes", Test: "TestFails", Output: "          actual:   1\n"},
		{Action: "output", Package: "shapes", Test: "TestFails", Output: "--- FAIL: TestFails (0.00s)\n", OutputType: "frame"},
		{Action: "fail", Package: "shapes", Test: "TestFails", Elapsed: 0.002},
		{Action: "run", Package: "shapes", Test: "TestSkips"},
		{Action: "skip", Package: "shapes", Test: "TestSkips", Elapsed: 0.0},
		{Action: "output", Package: "shapes", Output: "FAIL\tshapes\t0.003s\n", OutputType: "frame"},
		{Action: "fail", Package: "shapes", Elapsed: 0.003},
	}

	res := FoldGoTest(events)
	if res.BuildFailed {
		t.Error("ordinary run reported a build failure")
	}
	if len(res.Tests) != 3 {
		t.Fatalf("folded %d tests, want 3: %+v", len(res.Tests), res.Tests)
	}
	pass, fail, skip := res.Tests[0], res.Tests[1], res.Tests[2]
	if pass.Func != "TestRectArea" || !pass.OK || pass.Skipped || pass.Elapsed != 0.001 {
		t.Errorf("pass outcome = %+v", pass)
	}
	if fail.Func != "TestFails" || fail.OK || fail.Elapsed != 0.002 {
		t.Errorf("fail outcome = %+v", fail)
	}
	if !strings.Contains(fail.Output, "area.gom:18: gom: assertion failed:") ||
		!strings.Contains(fail.Output, "actual:   1") {
		t.Errorf("fail output lacks the assertion text: %q", fail.Output)
	}
	if strings.Contains(fail.Output, "=== RUN") || strings.Contains(fail.Output, "--- FAIL") {
		t.Errorf("fail output kept go test framing: %q", fail.Output)
	}
	if !skip.OK || !skip.Skipped {
		t.Errorf("skip outcome = %+v", skip)
	}
}

// TestFoldGoTestBuildFail: a package whose tests do not compile
// reports build-output lines and the build-fail event, and the
// package-level fail reaches no test bucket.
func TestFoldGoTestBuildFail(t *testing.T) {
	events := []GoTestEvent{
		{ImportPath: "shapes [shapes.test]", Action: "build-output", Output: "# shapes [shapes.test]\n"},
		{ImportPath: "shapes [shapes.test]", Action: "build-output", Output: "./broken_test.go:6:2: undefined: x\n"},
		{ImportPath: "shapes [shapes.test]", Action: "build-fail"},
		{Action: "start", Package: "shapes"},
		{Action: "output", Package: "shapes", Output: "FAIL\tshapes [build failed]\n", OutputType: "frame"},
		{Action: "fail", Package: "shapes", FailedBuild: "shapes [shapes.test]"},
	}
	res := FoldGoTest(events)
	if !res.BuildFailed {
		t.Error("build failure not detected")
	}
	if len(res.Tests) != 0 {
		t.Errorf("build-failed run reported tests: %+v", res.Tests)
	}
	joined := strings.Join(res.BuildOutput, "")
	if !strings.Contains(joined, "broken_test.go:6:2: undefined: x") {
		t.Errorf("build output lost the error line: %v", res.BuildOutput)
	}
}

// TestFoldGoTestDiedMidRun: a test the binary abandoned — a panic
// that killed the process — reports as a failure carrying whatever
// output it produced before dying.
func TestFoldGoTestDiedMidRun(t *testing.T) {
	events := []GoTestEvent{
		{Action: "start", Package: "p"},
		{Action: "run", Package: "p", Test: "TestPanics"},
		{Action: "output", Package: "p", Test: "TestPanics", Output: "panic: boom\n"},
		{Action: "output", Package: "p", Output: "FAIL\tp\t0.001s\n", OutputType: "frame"},
		{Action: "fail", Package: "p", Elapsed: 0.001},
	}
	res := FoldGoTest(events)
	if len(res.Tests) != 1 {
		t.Fatalf("abandoned test not reported: %+v", res.Tests)
	}
	o := res.Tests[0]
	if o.OK || o.Func != "TestPanics" {
		t.Errorf("abandoned test outcome = %+v, want failure", o)
	}
	if !strings.Contains(o.Output, "panic: boom") {
		t.Errorf("abandoned test lost its output: %q", o.Output)
	}
}

// TestFoldGoTestPackageOutput: output with no test attribution — a
// panic in package init — lands in PackageOutput, not a test bucket.
func TestFoldGoTestPackageOutput(t *testing.T) {
	events := []GoTestEvent{
		{Action: "start", Package: "p"},
		{Action: "output", Package: "p", Output: "panic: init died\n"},
		{Action: "output", Package: "p", Output: "FAIL\tp\t0.001s\n", OutputType: "frame"},
		{Action: "fail", Package: "p"},
	}
	res := FoldGoTest(events)
	if !strings.Contains(res.PackageOutput, "panic: init died") {
		t.Errorf("package output = %q", res.PackageOutput)
	}
	if strings.Contains(res.PackageOutput, "FAIL\tp") {
		t.Errorf("package output kept framing: %q", res.PackageOutput)
	}
}
