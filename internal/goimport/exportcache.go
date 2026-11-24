package goimport

// exportCache is the process-spanning file cache in front of
// `go list -export` (decision: docs/decisions/2026-11-18-golist-
// export-cache.md). One `go list` subprocess costs ~80ms and the
// set a Gom program imports barely changes between CLI runs, so the
// (path → export .a path, package dir) answer is stored under
// os.UserCacheDir and validated cheaply on every hit:
//
//   - the recorded export file must still exist with the same size
//     and mtime (GOCACHE is content-addressed — a rebuild writes a
//     new path and the old entry lingers until the cache trims it,
//     so existence alone cannot prove freshness);
//   - the recorded max *.go mtime of the package's source dir AND of
//     every dependency's source dir must still hold (covers edits
//     under the same export path being reported while the toolchain
//     has not rebuilt yet — and a dependency's types are embedded
//     in the importer's archive, so dep edits must miss too).
//
// The key carries the whole resolution context `go list` would see:
// the package path, the effective Dir, the resolved go binary's
// identity, GOOS/GOARCH/GOFLAGS/GOWORK, the workspace file actually
// in effect (explicit GOWORK or a go.work found above Dir —
// GOWORK=off disables either), and the nearest go.mod/go.sum above
// Dir with their mtimes — a change in any of them lands on a
// different key.
//
// Every failure is a plain miss or a skipped write: the cache must
// never break a compile.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// exportCacheEnv holds the environment names the cache honors:
// GOM_GOIMPORT_CACHE_DIR overrides the storage directory (tests),
// GOM_GOIMPORT_NOCACHE disables the cache entirely.
const (
	exportCacheDirEnv = "GOM_GOIMPORT_CACHE_DIR"
	exportCacheOffEnv = "GOM_GOIMPORT_NOCACHE"
)

// exportEntry is one cached `go list -export` answer.
type exportEntry struct {
	Pkg         string    `json:"pkg"`
	Dir         string    `json:"dir"` // source dir of the package
	Export      string    `json:"export"`
	ExportSize  int64     `json:"exportSize"`
	ExportMtime int64     `json:"exportMtime"` // unix nano
	SrcMtime    int64     `json:"srcMtime"`    // max *.go mtime under Dir, unix nano
	SrcCount    int       `json:"srcCount"`    // *.go files directly under Dir
	Deps        []depStat `json:"deps,omitempty"`
}

// depStat records one dependency's source-dir freshness — the same
// (max *.go mtime, count) pair the package's own Dir uses. A
// dependency's exported API is baked into the importer's archive,
// so an edit there must invalidate the entry even though the
// package's own source is untouched.
type depStat struct {
	Dir      string `json:"dir"`
	SrcMtime int64  `json:"srcMtime"`
	SrcCount int    `json:"srcCount"`
}

// exportCache binds the lookup key material of one Loader (dir +
// gobin) to the on-disk entries.
type exportCache struct {
	dir string // "" disables the cache
	pre string // the per-loader key prefix

	// stats memoizes srcStat per dir for this cache's lifetime — a
	// package's dep dirs recur across every imported package that
	// shares them (the stdlib tree in particular), so each stat walk
	// would otherwise repeat several times per build. The memo is
	// per cache, not global: a *new* Loader must re-read sources,
	// which is what lets an in-process source edit invalidate.
	statsMu sync.Mutex
	stats   map[string][2]int64
}

// newExportCache resolves the cache for cfg; a nil result disables
// it (nocache env, no writable cache dir, …).
func newExportCache(gobin, dir string) *exportCache {
	if os.Getenv(exportCacheOffEnv) != "" {
		return nil
	}
	base := os.Getenv(exportCacheDirEnv)
	if base == "" {
		ucd, err := os.UserCacheDir()
		if err != nil {
			return nil
		}
		base = filepath.Join(ucd, "gom", "goexport")
	}
	c := &exportCache{dir: base}

	effDir, err := filepath.Abs(dir)
	if err != nil {
		effDir = dir
	}
	var b strings.Builder
	b.WriteString(effDir)
	b.WriteByte(0)
	b.WriteString(goEnvSuffix(gobin))
	// The workspace file actually in effect: an explicit GOWORK, a
	// go.work discovered above Dir when it is unset, or nothing when
	// it is `off`.
	gw := os.Getenv("GOWORK")
	switch {
	case gw == "off":
		b.WriteByte(0)
		b.WriteString("GOWORK=off")
	case gw != "":
		b.WriteByte(0)
		b.WriteString("GOWORK=" + gw)
		if fi, err := os.Stat(gw); err == nil {
			fmt.Fprintf(&b, "|%d", fi.ModTime().UnixNano())
		}
	default:
		if w := findGoWork(effDir); w != "" {
			b.WriteByte(0)
			b.WriteString("GOWORK=" + w)
			if fi, err := os.Stat(w); err == nil {
				fmt.Fprintf(&b, "|%d", fi.ModTime().UnixNano())
			}
		}
	}
	gomod := findGoMod(effDir)
	if gomod != "" {
		b.WriteByte(0)
		b.WriteString("GOMOD=" + gomod)
		if fi, err := os.Stat(gomod); err == nil {
			fmt.Fprintf(&b, "|%d", fi.ModTime().UnixNano())
		}
		if fi, err := os.Stat(strings.TrimSuffix(gomod, ".mod") + ".sum"); err == nil {
			fmt.Fprintf(&b, "|%d", fi.ModTime().UnixNano())
		}
	}
	c.pre = b.String()
	return c
}

// goEnvSuffix renders the gobin-derived part of the cache key —
// the resolved binary, its identity stat, and its effective build
// settings — and memoizes it per process so each pooled Loader
// does not pay for a PATH probe, a binary stat, and a `go env`
// subprocess again.
//
// Effective build settings come from `go env` rather than
// os.Getenv: `go env` folds the GOENV file (`go env -w` settings)
// under the process environment, exactly as a later `go list`
// sees it — reading only os.Getenv would keep serving exports
// selected under different tags after a `go env -w` change. The
// memo means a mid-process `go env -w` is not picked up, which is
// acceptable: a build already assumes a stable toolchain.
var goEnvSuffixCache sync.Map // gobin -> key suffix string

func goEnvSuffix(gobin string) string {
	if v, ok := goEnvSuffixCache.Load(gobin); ok {
		return v.(string)
	}
	// Resolve a bare name ("go") to the executable `go list` would
	// actually run — Abs alone would stat a working-directory file
	// that usually does not exist, leaving every toolchain on PATH
	// sharing one identity.
	gobinAbs := gobin
	if p, err := exec.LookPath(gobin); err == nil {
		gobinAbs = p
	}
	if p, err := filepath.Abs(gobinAbs); err == nil {
		gobinAbs = p
	}
	var b strings.Builder
	b.WriteString(gobinAbs)
	if fi, err := os.Stat(gobinAbs); err == nil {
		fmt.Fprintf(&b, "|%d|%d", fi.Size(), fi.ModTime().UnixNano())
	}
	if out, err := exec.Command(gobinAbs, "env", "GOOS", "GOARCH", "GOFLAGS").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			b.WriteByte(0)
			b.WriteString(line)
		}
	} else {
		for _, k := range []string{"GOOS", "GOARCH", "GOFLAGS"} {
			b.WriteByte(0)
			b.WriteString(k + "=" + os.Getenv(k))
		}
	}
	v, _ := goEnvSuffixCache.LoadOrStore(gobin, b.String())
	return v.(string)
}

// findGoWork walks dir upward to the nearest go.work — the
// workspace `go list` auto-discovers when GOWORK is unset
// ("" = none found).
func findGoWork(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.work")); err == nil {
			return filepath.Join(d, "go.work")
		}
		p := filepath.Dir(d)
		if p == d {
			return ""
		}
		d = p
	}
}

// findGoMod walks dir upward to the nearest directory holding a
// go.mod ("" = none found).
func findGoMod(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return filepath.Join(d, "go.mod")
		}
		p := filepath.Dir(d)
		if p == d {
			return ""
		}
		d = p
	}
}

// file is the on-disk path of pkg's entry.
func (c *exportCache) file(pkg string) string {
	sum := sha256.Sum256([]byte(c.pre + "\x00" + pkg))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".json")
}

// get returns the entry for pkg when it still validates, or false.
func (c *exportCache) get(pkg string) (exportEntry, bool) {
	var e exportEntry
	data, err := os.ReadFile(c.file(pkg))
	if err != nil || json.Unmarshal(data, &e) != nil {
		return e, false
	}
	if e.Pkg != pkg || e.Export == "" {
		return e, false
	}
	fi, err := os.Stat(e.Export)
	if err != nil || fi.Size() != e.ExportSize || fi.ModTime().UnixNano() != e.ExportMtime {
		return e, false
	}
	sm, sc := c.srcStat(e.Dir)
	if sm != e.SrcMtime || sc != e.SrcCount {
		return e, false
	}
	for _, d := range e.Deps {
		sm, sc := c.srcStat(d.Dir)
		if sm != d.SrcMtime || sc != d.SrcCount {
			return e, false
		}
	}
	return e, true
}

// put writes the entry for pkg (export/dir/depDirs are the `go
// list` answers); failures are ignored — a cache that cannot write
// just behaves like no cache.
func (c *exportCache) put(pkg, export, dir string, depDirs []string) {
	fi, err := os.Stat(export)
	if err != nil {
		return
	}
	sm, sc := c.srcStat(dir)
	e := exportEntry{
		Pkg:         pkg,
		Dir:         dir,
		Export:      export,
		ExportSize:  fi.Size(),
		ExportMtime: fi.ModTime().UnixNano(),
		SrcMtime:    sm,
		SrcCount:    sc,
	}
	for _, d := range depDirs {
		if d == dir {
			continue
		}
		sm, sc := c.srcStat(d)
		e.Deps = append(e.Deps, depStat{Dir: d, SrcMtime: sm, SrcCount: sc})
	}
	data, err := json.Marshal(e)
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
	_ = os.Rename(name, c.file(pkg)) // a lost rename still validates next time
}

// srcStat reports (max *.go mtime, *.go count) of the files
// directly under dir — the pair is what proves a cached export
// path still describes the same source (a same-nanosecond mtime
// on a coarse-granularity filesystem cannot add or remove a file
// without moving the count). (-1,-1) when the dir cannot be read.
func (c *exportCache) srcStat(dir string) (int64, int) {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	if c.stats == nil {
		c.stats = map[string][2]int64{}
	}
	if s, ok := c.stats[dir]; ok {
		return s[0], int(s[1])
	}
	m, n := srcStat(dir)
	c.stats[dir] = [2]int64{m, int64(n)}
	return m, n
}

func srcStat(dir string) (int64, int) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return -1, -1
	}
	var max int64 = -1
	count := 0
	for _, ent := range ents {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		fi, err := ent.Info()
		if err != nil {
			return -1, -1
		}
		count++
		if mt := fi.ModTime().UnixNano(); mt > max {
			max = mt
		}
	}
	return max, count
}
