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
//   - the recorded max *.go mtime of the package's source dir must
//     still hold (covers edits under the same export path being
//     reported while the toolchain has not rebuilt yet).
//
// The key carries the whole resolution context `go list` would see:
// the package path, the effective Dir, the go binary's identity,
// GOOS/GOARCH/GOFLAGS/GOWORK (plus the workspace file's mtime), and
// the nearest go.mod/go.sum above Dir with their mtimes — a change
// in any of them lands on a different key.
//
// Every failure is a plain miss or a skipped write: the cache must
// never break a compile.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	Pkg         string `json:"pkg"`
	Dir         string `json:"dir"` // source dir of the package
	Export      string `json:"export"`
	ExportSize  int64  `json:"exportSize"`
	ExportMtime int64  `json:"exportMtime"` // unix nano
	SrcMtime    int64  `json:"srcMtime"`    // max *.go mtime under Dir, unix nano
	SrcCount    int    `json:"srcCount"`    // *.go files directly under Dir
}

// exportCache binds the lookup key material of one Loader (dir +
// gobin) to the on-disk entries.
type exportCache struct {
	dir string // "" disables the cache
	pre string // the per-loader key prefix
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
	gobinAbs, err := filepath.Abs(gobin)
	if err != nil {
		gobinAbs = gobin
	}
	var b strings.Builder
	b.WriteString(effDir)
	b.WriteByte(0)
	b.WriteString(gobinAbs)
	if fi, err := os.Stat(gobinAbs); err == nil {
		fmt.Fprintf(&b, "|%d|%d", fi.Size(), fi.ModTime().UnixNano())
	}
	for _, k := range []string{"GOOS", "GOARCH", "GOFLAGS"} {
		b.WriteByte(0)
		b.WriteString(k + "=" + os.Getenv(k))
	}
	if w := os.Getenv("GOWORK"); w != "" {
		b.WriteByte(0)
		b.WriteString("GOWORK=" + w)
		if fi, err := os.Stat(w); err == nil {
			fmt.Fprintf(&b, "|%d", fi.ModTime().UnixNano())
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
	sm, sc := srcStat(e.Dir)
	if sm != e.SrcMtime || sc != e.SrcCount {
		return e, false
	}
	return e, true
}

// put writes the entry for pkg (export/dir are the `go list`
// answers); failures are ignored — a cache that cannot write just
// behaves like no cache.
func (c *exportCache) put(pkg, export, dir string) {
	fi, err := os.Stat(export)
	if err != nil {
		return
	}
	sm, sc := srcStat(dir)
	e := exportEntry{
		Pkg:         pkg,
		Dir:         dir,
		Export:      export,
		ExportSize:  fi.Size(),
		ExportMtime: fi.ModTime().UnixNano(),
		SrcMtime:    sm,
		SrcCount:    sc,
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
