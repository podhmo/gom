package main

// The incremental build cache behind `gom build` (design.md
// section 5). A package's generated outputs are reusable while its
// own normalized sources and the interfaces of the Gom packages it
// imported are unchanged — the compiler already made .gomi the
// typecheck boundary, so "my source or a dependency's interface
// changed" is exactly "recompile".
//
// The key carries, per design section 5's list:
//
//   - each .gom input's canonical token-stream hash — comments,
//     blanks and positions excluded; `test`/`@test` declarations
//     dropped (test code is not a `gom build` output);
//   - the package's own *.gomi files and every .gomi the import
//     loader read, each canonical-hashed — an interface's semantic
//     change regenerates, an implementation-only dependency edit
//     does not;
//   - the compiler's generation version plus the resolved binary's
//     identity — codegen/extern/lowering rule changes invalidate;
//   - the rt/stdlib ABI: file stats of the toolchain packages the
//     generated code calls into;
//   - Go version and the effective GOOS/GOARCH/GOFLAGS — build
//     conditions alter symbol resolution.
//
// Storage mirrors internal/goimport's export cache: one JSON
// manifest per package directory under os.UserCacheDir (or
// GOM_BUILD_CACHE_DIR for tests), an os.Rename'd write, and every
// failure degrading to a plain miss — the cache must never break
// a compile. GOM_BUILD_NOCACHE disables it entirely (the
// GOM_GOIMPORT_NOCACHE precedent).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
)

const (
	// genVersion bumps whenever codegen, lowering or extern rules
	// change what the same sources emit — part of every manifest's
	// key so a compiler change invalidates every package.
	genVersion = 1

	cacheDirEnv = "GOM_BUILD_CACHE_DIR"
	cacheOffEnv = "GOM_BUILD_NOCACHE"
)

// buildCache binds the shared key material (compiler, ABI, Go
// env) computed once per `gom build` invocation to the on-disk
// manifests. notes latches each compiled dir group's inputs and
// gomi dependencies until its outputs are written (a failed group
// never commits).
type buildCache struct {
	dir string
	// key is computed lazily: the `go list`/`go env` subprocesses
	// behind it cost tens of milliseconds, so a run that never
	// validates or writes a manifest (a fresh cache dir) should
	// not pay them.
	key   string
	keyOk bool
	notes map[string]bcNote
}

// bcNote is one dir group's pending manifest data.
type bcNote struct {
	ins   []build.Input
	gomis []string
}

type bcHashed struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

type bcManifest struct {
	Dir     string     `json:"dir"`
	Out     string     `json:"out"` // the -o flag the outputs were written under
	Key     string     `json:"key"`
	Inputs  []bcHashed `json:"inputs"`
	Gomis   []bcHashed `json:"gomis"`
	Outputs []string   `json:"outputs"`
}

// newBuildCache resolves the cache for this invocation; nil
// disables it (nocache env, no writable cache dir, …).
func newBuildCache() *buildCache {
	if os.Getenv(cacheOffEnv) != "" {
		return nil
	}
	base := os.Getenv(cacheDirEnv)
	if base == "" {
		ucd, err := os.UserCacheDir()
		if err != nil {
			return nil
		}
		base = filepath.Join(ucd, "gom", "build")
	}
	return &buildCache{dir: base, notes: map[string]bcNote{}}
}

// envKey returns the shared key material, computing it once.
func (c *buildCache) envKey() string {
	if !c.keyOk {
		c.key = cacheKey()
		c.keyOk = true
	}
	return c.key
}

// cacheKey hashes the environment material shared by every
// manifest: compiler identity, the rt/stdlib ABI and the effective
// Go build conditions.
func cacheKey() string {
	h := sha256.New()
	fmt.Fprintf(h, "gen=%d go=%s\n", genVersion, runtime.Version())
	if exe, err := os.Executable(); err == nil {
		if fi, err := os.Stat(exe); err == nil {
			fmt.Fprintf(h, "bin=%s|%d|%d\n", exe, fi.Size(), fi.ModTime().UnixNano())
		}
	}
	gobin := "go"
	if p, err := exec.LookPath(gobin); err == nil {
		gobin = p
	}
	// The ABI: every file of the toolchain packages generated
	// code calls into — rt's helpers and the stdlib's generated +
	// handwritten Go and .gomi. `go list` resolves them in the
	// importing module's build list, so a replaced toolchain
	// lands on a different answer.
	if out, err := exec.Command(gobin, "list", "-f", "{{.Dir}}",
		"github.com/podhmo/gom/rt", "github.com/podhmo/gom/stdlib/...").Output(); err == nil {
		for _, d := range strings.Fields(string(out)) {
			statTree(h, d)
		}
	}
	if out, err := exec.Command(gobin, "env", "GOOS", "GOARCH", "GOFLAGS").Output(); err == nil {
		h.Write(out)
	} else {
		for _, k := range []string{"GOOS", "GOARCH", "GOFLAGS"} {
			fmt.Fprintf(h, "%s=%s\n", k, os.Getenv(k))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// statTree folds (name, size, mtime) of every *.go and *.gomi
// file directly under dir into h.
func statTree(h hash.Hash, dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, ".gomi") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s/%s|%d|%d\n", dir, name, fi.Size(), fi.ModTime().UnixNano())
	}
}

// canonHash is the normalized-source fingerprint: the canonical
// token-stream hash. dropTests removes `test`/`@test` declaration
// spans first — for .gom inputs, where test code never reaches a
// `gom build` output. A .gomi is all signature: hashed whole.
func canonHash(src []byte, dropTests bool) string {
	var drops []diag.Span
	if dropTests {
		drops = syntax.TestDeclSpans(src, "")
	}
	sum := sha256.Sum256(syntax.Canonical(src, drops))
	return hex.EncodeToString(sum[:])
}

// note latches a compiled dir group's dependency data for a
// later commit. gomis carries the package's own interfaces plus
// every .gomi the import loader reported.
func (c *buildCache) note(dir string, ins []build.Input, gomis []string) {
	c.notes[dir] = bcNote{ins: ins, gomis: slices.Sorted(slices.Values(gomis))}
}

// commit writes the manifest for a fully-written dir group —
// outputs are the .go paths this build produced under `out`.
func (c *buildCache) commit(dir, out string, outputs []string) {
	n, ok := c.notes[dir]
	if !ok {
		return
	}
	m := bcManifest{
		Dir:     dir,
		Out:     out,
		Key:     c.envKey(),
		Outputs: slices.Clone(outputs),
	}
	for _, in := range n.ins {
		m.Inputs = append(m.Inputs, bcHashed{Path: in.Path, Hash: canonHash(in.Src, true)})
	}
	for _, p := range n.gomis {
		src, err := os.ReadFile(p)
		if err != nil {
			return // a dep that vanished cannot seed a hit
		}
		m.Gomis = append(m.Gomis, bcHashed{Path: p, Hash: canonHash(src, false)})
	}
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(c.dir, ".tmp-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return
	}
	_ = os.Rename(name, c.file(dir))
}

// Hit reports whether dir's manifest still validates — same key,
// same normalized inputs, unchanged recorded gomis, outputs still
// on disk. The recorded gomi set is compared file-by-file: an
// added or removed import changes the input hash first, so the
// set comparison here is exact.
func (c *buildCache) Hit(dir, out string, ins []build.Input) (outputs []string, ok bool) {
	var m bcManifest
	data, err := os.ReadFile(c.file(dir))
	if err != nil || json.Unmarshal(data, &m) != nil {
		return nil, false
	}
	if m.Dir != dir || m.Out != out || m.Key != c.envKey() {
		return nil, false
	}
	if len(m.Inputs) != len(ins) {
		return nil, false
	}
	byPath := make(map[string]string, len(m.Inputs))
	for _, h := range m.Inputs {
		byPath[h.Path] = h.Hash
	}
	for _, in := range ins {
		wh, ok := byPath[in.Path]
		if !ok || wh != canonHash(in.Src, true) {
			return nil, false
		}
	}
	for _, g := range m.Gomis {
		src, err := os.ReadFile(g.Path)
		if err != nil || canonHash(src, false) != g.Hash {
			return nil, false
		}
	}
	if len(m.Outputs) == 0 {
		return nil, false
	}
	for _, o := range m.Outputs {
		if _, err := os.Stat(o); err != nil {
			return nil, false
		}
	}
	return m.Outputs, true
}

func (c *buildCache) file(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".json")
}
