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
		{"known command is a stub", []string{"build"}, 2, "", "not implemented"},
		{"extra args after known command", []string{"check", "-h"}, 2, "", "not implemented"},
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
	for _, c := range subcommands {
		if !strings.Contains(stdout.String(), c) {
			t.Errorf("usage text does not list subcommand %q", c)
		}
	}
}

// TestAllSubcommandsAreDispatched ensures every declared subcommand is
// recognized (as a stub) rather than reported as unknown.
func TestAllSubcommandsAreDispatched(t *testing.T) {
	for _, c := range subcommands {
		var stderr strings.Builder
		code := run([]string{c}, &strings.Builder{}, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "not implemented") {
			t.Errorf("run(%q) = exit %d, stderr %q; want exit 2 and a not-implemented message",
				c, code, stderr.String())
		}
	}
}
