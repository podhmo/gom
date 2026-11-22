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
//   - the source-dir file stats of every Go package the importer
//     resolved (own dir plus `go list -export`'s dep dirs) — an
//     imported Go API change regenerates;
//   - the nearest go.mod/go.sum and go.work above the package dir —
//     a replace or workspace change redirects resolution;
//   - the compiler's generation version plus the resolved binary's
//     identity — codegen/extern/lowering rule changes invalidate;
//   - the rt/stdlib ABI: file stats of the toolchain packages the
//     generated code calls into;
//   - Go version and the effective GOOS/GOARCH/GOFLAGS/GOWORK —
//     build conditions alter symbol resolution.
//
// Storage mirrors internal/goimport's export cache: one JSON
// manifest per package directory under os.UserCacheDir (or
// GOM_BUILD_CACHE_DIR for tests), an os.Rename'd write, and every
// failure degrading to a plain miss — the cache must never break
// a compile. GOM_BUILD_NOCACHE disables it entirely (the
// GOM_GOIMPORT_NOCACHE precedent).

import (
	"bytes"
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
	"sync"

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
// manifests. notes latches each compiled dir group's inputs until
// its outputs are written (a failed group never commits); seen
// accumulates the dependencies every compile of the dir loaded —
// including the interface pass, which resolves imports of its own.
type buildCache struct {
	dir string
	// mu serializes map access — parallel dir compiles call
	// depSink/note from separate goroutines (design.md section 5
	// item 6). commit/Hit run under it too for a uniform rule.
	mu sync.Mutex
	// key is computed lazily: the `go list`/`go env` subprocesses
	// behind it cost tens of milliseconds, so a run that never
	// validates or writes a manifest (a fresh cache dir) should
	// not pay them.
	key   string
	keyOk bool
	notes map[string][]build.Input
	seen  map[string]*depSeen
}

// depSeen is one dir's observed dependency set: the .gomi files
// the Gom loader read and the source dirs of the Go packages the
// Go importer resolved (each with its `go list -export` dep dirs).
type depSeen struct {
	gomis  []string
	godirs []string
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
	GoDirs  []bcHashed `json:"goDirs,omitempty"`
	Mods    []bcHashed `json:"mods,omitempty"`
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
	return &buildCache{dir: base, notes: map[string][]build.Input{}, seen: map[string]*depSeen{}}
}

// envKeyLocked returns the shared key material, computing it once —
// the lazy key computation is why it must be called with mu held;
// commit/Hit cannot re-take the non-reentrant mutex.
func (c *buildCache) envKeyLocked() string {
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
	if out, err := exec.Command(gobin, "env", "GOOS", "GOARCH", "GOFLAGS", "GOWORK").Output(); err == nil {
		h.Write(out)
	} else {
		for _, k := range []string{"GOOS", "GOARCH", "GOFLAGS", "GOWORK"} {
			fmt.Fprintf(h, "%s=%s\n", k, os.Getenv(k))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// statTreeHash is a dir's file-stat fingerprint — the same
// (name, size, mtime) fold statTree writes, hashed. A Go dep
// dir's API change lands here.
func statTreeHash(dir string) string {
	h := sha256.New()
	statTree(h, dir)
	return hex.EncodeToString(h.Sum(nil))
}

// fileStatHash fingerprints one file by size and mtime — the
// granularity module-context files (go.mod, go.sum, go.work)
// need: an edit flips the mtime, a rewrite that preserves it is
// indistinguishable anyway.
func fileStatHash(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d|%d", fi.Size(), fi.ModTime().UnixNano())
}

// modFiles lists the module-context files that steer dependency
// resolution for dir: the nearest ancestor's go.mod and go.sum
// (replace/exclude/version edits redirect imports) plus the
// nearest go.work and go.work.sum (a workspace wins over the
// module). Only the first of each counts — the innermost module
// is the one `go` resolves in.
func modFiles(dir string) []string {
	var out []string
	foundMod, foundWork := false, false
	for d := dir; ; {
		if !foundMod {
			p := filepath.Join(d, "go.mod")
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				foundMod = true
				out = append(out, p)
				if fi, err := os.Stat(filepath.Join(d, "go.sum")); err == nil && !fi.IsDir() {
					out = append(out, filepath.Join(d, "go.sum"))
				}
			}
		}
		if !foundWork {
			p := filepath.Join(d, "go.work")
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				foundWork = true
				out = append(out, p)
				if fi, err := os.Stat(filepath.Join(d, "go.work.sum")); err == nil && !fi.IsDir() {
					out = append(out, filepath.Join(d, "go.work.sum"))
				}
			}
		}
		parent := filepath.Dir(d)
		if parent == d || (foundMod && foundWork) {
			return out
		}
		d = parent
	}
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

// dedup sorts and deduplicates a path list for manifest entries.
func dedup(ps []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(ps)))
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
	canon := syntax.Canonical(src, drops)
	// Comments leave the canonical token stream unchanged, but
	// `//gom:` directive lines carry semantics (//gom:allow,
	// //gom:noescape) — fold them into the fingerprint so adding or
	// removing one invalidates the entry.
	for _, line := range bytes.Split(src, []byte("\n")) {
		if t := bytes.TrimSpace(line); bytes.HasPrefix(t, []byte("//gom:")) {
			canon = append(canon, '\n')
			canon = append(canon, t...)
		}
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:])
}

// depSink returns dir's dependency accumulator, creating it —
// the compile hooks and the interface pass append into the same
// bucket so every path a package resolves counts.
func (c *buildCache) depSink(dir string) *depSeen {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.seen[dir]
	if s == nil {
		s = &depSeen{}
		c.seen[dir] = s
	}
	return s
}

// note latches a compiled dir group's inputs for a later commit.
// Its dependencies are not arguments: they keep accumulating in
// seen until commit runs, after the interface pass too.
func (c *buildCache) note(dir string, ins []build.Input) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notes[dir] = ins
}

// commit writes the manifest for a fully-written dir group —
// outputs are the .go paths this build produced under `out`.
func (c *buildCache) commit(dir, out string, outputs []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ins, ok := c.notes[dir]
	if !ok {
		return
	}
	m := bcManifest{
		Dir:     dir,
		Out:     out,
		Key:     c.envKeyLocked(),
		Outputs: slices.Clone(outputs),
	}
	for _, in := range ins {
		m.Inputs = append(m.Inputs, bcHashed{Path: in.Path, Hash: canonHash(in.Src, true)})
	}
	// The package's own interfaces are dependencies too — their
	// edit must regenerate just like an imported one.
	gomis := filepathGomis(dir)
	godirs := []string{}
	if s := c.seen[dir]; s != nil {
		gomis = append(gomis, s.gomis...)
		godirs = s.godirs
	}
	// Go-side deps pool at module granularity — the shared
	// goimport.Loader's OnLoad cannot tell which dir triggered a
	// resolve, so every dir in the pool records the union.
	if s := c.seen[goPoolKey(dir)]; s != nil {
		godirs = append(godirs, s.godirs...)
	}
	for _, p := range dedup(gomis) {
		src, err := os.ReadFile(p)
		if err != nil {
			return // a dep that vanished cannot seed a hit
		}
		m.Gomis = append(m.Gomis, bcHashed{Path: p, Hash: canonHash(src, false)})
	}
	for _, d := range dedup(godirs) {
		m.GoDirs = append(m.GoDirs, bcHashed{Path: d, Hash: statTreeHash(d)})
	}
	for _, p := range modFiles(dir) {
		m.Mods = append(m.Mods, bcHashed{Path: p, Hash: fileStatHash(p)})
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
	c.mu.Lock()
	defer c.mu.Unlock()
	var m bcManifest
	data, err := os.ReadFile(c.file(dir))
	if err != nil || json.Unmarshal(data, &m) != nil {
		return nil, false
	}
	if m.Dir != dir || m.Out != out || m.Key != c.envKeyLocked() {
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
	for _, g := range m.GoDirs {
		if statTreeHash(g.Path) != g.Hash {
			return nil, false
		}
	}
	// Module context is recomputed, not just re-stated: a go.mod
	// or go.work appearing where none was recorded redirects
	// resolution exactly like an edit to a recorded one.
	mods := modFiles(dir)
	if len(mods) != len(m.Mods) {
		return nil, false
	}
	for i, p := range mods {
		if m.Mods[i].Path != p || m.Mods[i].Hash != fileStatHash(p) {
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
