package main

import (
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string // substring expected in stdout
		wantErr  string // substring expected in stderr
	}{
		{"no args shows usage and fails", nil, 2, "", "Usage:"},
		{"-h shows usage", []string{"-h"}, 0, "Usage:", ""},
		{"--help shows usage", []string{"--help"}, 0, "Usage:", ""},
		{"help shows usage", []string{"help"}, 0, "Usage:", ""},
		{"unknown command fails", []string{"bogus"}, 2, "", `unknown command "bogus"`},
		{"check -h prints its usage", []string{"check", "-h"}, 0, "", "Usage of check"},
		{"check -format typo is a usage error", []string{"check", "-fmt"}, 2, "", "flag provided but not defined"},
		{"missing input file", []string{"check", "no/such/file.gom"}, 2, "", "no such file"},
		{"nonexistent dir", []string{"check", "no/such/dir/"}, 2, "", "no such file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			code := run(tt.args, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("run(%v) exit code = %d, want %d", tt.args, code, tt.wantCode)
			}
			if got := stdout.String(); tt.wantOut != "" && !strings.Contains(got, tt.wantOut) {
				t.Errorf("run(%v) stdout = %q, want substring %q", tt.args, got, tt.wantOut)
			}
			if got := stderr.String(); tt.wantErr != "" && !strings.Contains(got, tt.wantErr) {
				t.Errorf("run(%v) stderr = %q, want substring %q", tt.args, got, tt.wantErr)
			}
		})
	}
}

// TestUsageListsAllSubcommands ensures the usage text stays in sync with
// the subcommands the CLI claims to support.
func TestUsageListsAllSubcommands(t *testing.T) {
	var stdout strings.Builder
	if code := run([]string{"-h"}, &stdout, &strings.Builder{}); code != 0 {
		t.Fatalf("run(-h) exit code = %d, want 0", code)
	}
	for _, c := range []string{"build", "check", "dump", "test", "fmt", "fix", "gomi", "explain", "schema"} {
		if !strings.Contains(stdout.String(), c) {
			t.Errorf("usage text does not list subcommand %q", c)
		}
	}
}

// TestGomiReportsTheRename ensures the old `gomi` subcommand points
// users at its new name rather than dispatching or reporting an
// unknown command. Every other usage-listed subcommand is
// implemented now, so no "not implemented" dispatch remains.
func TestGomiReportsTheRename(t *testing.T) {
	var stderr strings.Builder
	code := run([]string{"gomi"}, &strings.Builder{}, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), `renamed to "gom info"`) {
		t.Errorf("run(gomi) = exit %d, stderr %q; want the rename notice",
			code, stderr.String())
	}
}
