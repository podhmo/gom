package lsp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
)

// GoTool is the pass recompileUnit runs after a successful unit
// compile when the server surfaces go-tool diagnostics: it stages
// the unit's generated output and folds the tool's report onto
// .gom positions (docs/decisions/2026-11-17-lsp-gotool-invocation.md).
// Nil on Server means no go-tool pass — the default.
type GoTool func(inputs []build.Input, results []*build.Result) []diag.Diagnostic

// NewGoTool returns the standard go-tool pass: `go build` in a
// temp directory inside the unit's own module, its diagnostics
// folded through the same GoPosIndex + GoToolDiagnostics machinery
// `gom build -go` uses.
//
// The temp dir lives under the module root so the unit's real
// go.mod — and its `require github.com/podhmo/gom` — resolves the
// generated imports; no synthetic module is written. The pass is
// all-or-nothing: it emits nothing while the unit has error
// diagnostics, a member without generated output, generated code
// that imports a sibling Gom package of the same module (its
// compiled form does not exist on disk), or no governing go.mod —
// gom diagnostics cover the first two, and the last is reported
// once per compile as a positionless go-tool diagnostic so an
// opted-in session is not silently inert.
func NewGoTool() GoTool {
	return func(inputs []build.Input, results []*build.Result) []diag.Diagnostic {
		if len(inputs) == 0 {
			return nil
		}
		dir := filepath.Dir(filepath.Clean(inputs[0].Path))
		modRoot, modPath, err := findModule(dir)
		if err != nil || modRoot == "" {
			return []diag.Diagnostic{
				diag.Error("E0001", "go build skipped: no go.mod above "+dir).
					From(diag.SourceGo).
					Set("tool", "go build"),
			}
		}
		selfPrefix := `"` + modPath + `/`
		selfExact := `"` + modPath + `"`
		var stages []struct {
			path string
			src  []byte
			out  *build.Result
		}
		for i, in := range inputs {
			if i >= len(results) || results[i] == nil || results[i].Output == nil ||
				results[i].Diagnostics.HasError() {
				return nil // partial package — gom diagnostics cover it
			}
			gen := results[i].Output.Source
			if bytes.Contains(gen, []byte(selfPrefix)) ||
				bytes.Contains(gen, []byte(selfExact)) {
				// The generated code imports a sibling Gom package,
				// which has no compiled form inside the module — a
				// real `go build` would report import errors the
				// .gom source did not cause.
				return nil
			}
			stages = append(stages, struct {
				path string
				src  []byte
				out  *build.Result
			}{in.Path, in.Src, results[i]})
		}
		tmp, err := os.MkdirTemp(modRoot, ".gomlsp-*")
		if err != nil {
			return []diag.Diagnostic{
				diag.Error("E0001", "go build could not run").
					From(diag.SourceGo).
					Set("tool", "go build").
					Set("error", err.Error()),
			}
		}
		defer os.RemoveAll(tmp)
		// A package directory's real .go files build alongside the
		// generated ones in `gom build -go` — stage them the same
		// way (generated names win on collision).
		stagedNames := map[string]bool{}
		for _, s := range stages {
			name := strings.TrimSuffix(filepath.Base(s.path), ".gom") + ".go"
			stagedNames[name] = true
		}
		if siblings, err := filepath.Glob(filepath.Join(dir, "*.go")); err == nil {
			for _, name := range siblings {
				base := filepath.Base(name)
				if stagedNames[base] {
					continue
				}
				data, rerr := os.ReadFile(name)
				if rerr != nil {
					continue
				}
				_ = os.WriteFile(filepath.Join(tmp, base), data, 0o644)
			}
		}
		hasMain := false
		for _, s := range stages {
			name := strings.TrimSuffix(filepath.Base(s.path), ".gom") + ".go"
			if err := os.WriteFile(filepath.Join(tmp, name), s.out.Output.Source, 0o644); err != nil {
				return []diag.Diagnostic{
					diag.Error("E0001", "go build could not run").
						From(diag.SourceGo).
						Set("tool", "go build").
						Set("error", err.Error()),
				}
			}
			if isMainSource(s.out.Output.Source) {
				hasMain = true
			}
		}
		srcs := make(map[string][]byte, len(stages))
		for _, s := range stages {
			srcs[s.path] = s.src
		}
		// The index's dir is the package dir, not the staging dir:
		// names the tool prints come from `//line` directives, which
		// spell .gom paths relative to the directory they were
		// generated for.
		pos := build.NewGoPosIndex(dir, srcs)
		for _, s := range stages {
			pos.AddSourceMap(s.path, s.out.Output.Source, s.out.Output.SourceMap)
		}
		run, err := build.RunGoBuild("", tmp, hasMain)
		if err != nil {
			return []diag.Diagnostic{
				diag.Error("E0001", "go build could not run").
					From(diag.SourceGo).
					Set("tool", "go build").
					Set("error", err.Error()),
			}
		}
		if !run.Failed {
			return nil
		}
		return build.GoToolDiagnostics(run.Lines(), "go build", pos)
	}
}

// findModule walks dir upward to the nearest go.mod and reports its
// directory and module path ("module example.com/m"). Zero values
// mean none was found; a malformed go.mod's error is returned.
func findModule(dir string) (root, path string, err error) {
	for {
		gm := filepath.Join(dir, "go.mod")
		data, rerr := os.ReadFile(gm)
		if rerr == nil {
			for line := range strings.Lines(string(data)) {
				if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
					return dir, strings.Trim(rest, ` "`), nil
				}
			}
			return "", "", nil // go.mod without a module directive
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", nil
		}
		dir = parent
	}
}

// isMainSource reports whether generated Go declares `package main`
// — `go build` on a main package links a binary, which RunGoBuild
// redirects to a temp file so the invocation leaves no artifact.
func isMainSource(src []byte) bool {
	for len(src) > 0 {
		var line []byte
		line, src, _ = bytes.Cut(src, []byte("\n"))
		if rest, ok := bytes.CutPrefix(line, []byte("package ")); ok {
			return string(rest) == "main"
		}
	}
	return false
}
