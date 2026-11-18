package main

import (
	"bytes"
	"path/filepath"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
)

// runGoTools implements `gom build -go` and `-vet`: for every
// directory that received generated output, run `go build` and/or
// `go vet` there and fold the tools' diagnostics back onto .gom
// spans. The directory grouping and the per-dir GoPosIndex
// construction follow runGoTests — the generated files' //line
// directives and anchor tables make gom positions recoverable from
// the tool's output (design.md 2.2,
// decisions/2026-11-17-cli-gotool-invocation.md).
func runGoTools(inputs []input, results []*build.Result, failed map[*build.Result]bool, dsts []string, rep *diag.Report, doBuild, doVet bool) {
	type pkg struct {
		dir     string
		idxs    []int
		pos     *build.GoPosIndex
		hasMain bool
	}
	var dirs []*pkg
	byDir := map[string]*pkg{}
	for i, r := range results {
		if r == nil || r.Output == nil || failed[r] {
			continue
		}
		dir, err := filepath.Abs(filepath.Dir(dsts[i]))
		if err != nil {
			dir = filepath.Dir(dsts[i])
		}
		p := byDir[dir]
		if p == nil {
			p = &pkg{dir: dir}
			byDir[dir] = p
			dirs = append(dirs, p)
		}
		p.idxs = append(p.idxs, i)
		if isMainSource(r.Output.Source) {
			p.hasMain = true
		}
	}
	for _, p := range dirs {
		srcs := make(map[string][]byte, len(p.idxs))
		for _, i := range p.idxs {
			srcs[inputs[i].path] = inputs[i].src
		}
		p.pos = build.NewGoPosIndex(p.dir, srcs)
		for _, i := range p.idxs {
			out := results[i].Output
			p.pos.AddSourceMap(inputs[i].path, out.Source, out.SourceMap)
			if out.TestSourceMap != nil {
				p.pos.AddSourceMap(inputs[i].path, out.TestSource, out.TestSourceMap)
			}
		}
	}
	if doBuild {
		for _, p := range dirs {
			run, err := build.RunGoBuild("", p.dir, p.hasMain)
			if err != nil {
				// The toolchain itself failed — a go-tool error
				// like a build failure, the same reporting
				// runGoTests uses.
				rep.Diagnostics = append(rep.Diagnostics,
					diag.Error("E0001", "go build could not run").
						From(diag.SourceGo).
						Set("tool", "go build").
						Set("error", err.Error()))
				continue
			}
			if !run.Failed {
				continue
			}
			// GoToolDiagnostics' zero-lines fallback is the honest
			// "the tool failed without a parseable line" report — a
			// successful build's silent stderr never reaches it.
			rep.Diagnostics = append(rep.Diagnostics,
				build.GoToolDiagnostics(run.Lines(), "go build", p.pos)...)
		}
	}

	if !doVet {
		return
	}
	for _, p := range dirs {
		run, err := build.RunGoVet("", p.dir)
		if err != nil {
			rep.Diagnostics = append(rep.Diagnostics,
				diag.Error("E0001", "go vet could not run").
					From(diag.SourceGo).
					Set("tool", "go vet").
					Set("error", err.Error()))
			continue
		}
		if !run.Failed {
			continue
		}
		// vet findings fold through the same position table as
		// build errors; lines it cannot parse (the `# pkg`
		// header, `vet:`-prefixed raw output) pass through
		// unpositioned like an unparseable build line.
		rep.Diagnostics = append(rep.Diagnostics,
			build.GoToolDiagnostics(run.Lines(), "go vet", p.pos)...)
	}
}

// isMainSource reports whether generated Go declares `package main`
// — `go build` on a main package links a binary, which the helper
// redirects to a temp file so the invocation writes nothing into
// the user's package directory.
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
