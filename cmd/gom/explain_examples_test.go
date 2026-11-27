package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// executableExplainExamples are documentation snippets meant to compile
// as a complete file after only the standard `package main` prelude.
// Most explain examples are deliberately illustrative fragments instead;
// they name the failing construct without repeating all surrounding type
// and function declarations. The fragment list is explicit so adding an
// example forces a classification instead of silently skipping coverage.
var executableExplainExamples = []diag.Code{
	"E0101", "E0102", "E0103", "E0104", "E0106",
	"E0203", "E0301", "E0401",
	"E0412", "E0413", "E0414", "E0416", "E0417", "E0419",
	"E0509", "E0510", "E0513",
	"E0705",
	"E0801", "E0802", "E0803",
	"W0003",
}

var fragmentExplainExamples = []diag.Code{
	"E0105", "E0201", "E0202", "E0204",
	"E0302", "E0303", "E0304", "E0306", "E0307", "E0308", "E0309",
	"E0402", "E0403", "E0404", "E0405", "E0406", "E0407", "E0408", "E0409", "E0410", "E0411", "E0415", "E0418", "E0420",
	"E0501", "E0502", "E0504", "E0505", "E0506", "E0507", "E0508", "E0511", "E0512",
	"E0605", "E0606", "E0607", "E0608", "E0609",
	"E0701", "E0702", "E0703", "E0704",
	"E0902", "E0903", "E0904", "E0905", "E0906", "E0907",
	"E9001", "E9003", "E9005",
	"W0501", "W0502", "W0602", "W0701", "W0702", "W0801", "W0901",
}

func TestExplainExamplesAreClassified(t *testing.T) {
	executable := codeSet(executableExplainExamples)
	fragment := codeSet(fragmentExplainExamples)
	for code := range executable {
		if fragment[code] {
			t.Errorf("%s is classified as both executable and fragment", code)
		}
	}
	for _, info := range diag.CodeTable() {
		if info.Example == "" || info.Retired {
			continue
		}
		if !executable[info.Code] && !fragment[info.Code] {
			t.Errorf("%s has an explain example but no executable/fragment classification", info.Code)
		}
	}
	for code := range executable {
		info, ok := diag.LookupCode(code)
		if !ok || info.Example == "" || info.Retired {
			t.Errorf("%s is classified as executable but has no live explain example", code)
		}
	}
	for code := range fragment {
		info, ok := diag.LookupCode(code)
		if !ok || info.Example == "" || info.Retired {
			t.Errorf("%s is classified as fragment but has no live explain example", code)
		}
	}
}

func TestExecutableExplainExamplesProduceTheirDiagnostic(t *testing.T) {
	for _, code := range executableExplainExamples {
		info, ok := diag.LookupCode(code)
		if !ok {
			t.Fatalf("%s missing from diagnostic table", code)
		}
		t.Run(string(code), func(t *testing.T) {
			dir := writeFiles(t, map[string]string{
				"main.gom": "package main\n\n" + info.Example + "\n",
			})
			_, stdout, stderr := invoke("build", filepath.Join(dir, "main.gom"))
			if !jsonOutputHasCode(stdout, code) {
				t.Fatalf("example for %s did not emit %s\nstdout:\n%s\nstderr:\n%s", code, code, stdout, stderr)
			}
		})
	}
}

func jsonOutputHasCode(output string, want diag.Code) bool {
	dec := json.NewDecoder(strings.NewReader(output))
	for {
		var line struct {
			Code diag.Code `json:"code"`
		}
		if err := dec.Decode(&line); err != nil {
			return false
		}
		if line.Code == want {
			return true
		}
	}
}

func codeSet(codes []diag.Code) map[diag.Code]bool {
	out := make(map[diag.Code]bool, len(codes))
	for _, code := range codes {
		out[code] = true
	}
	return out
}
