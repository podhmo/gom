// Package gomimport loads Gom packages for `import "path"`: the
// import path names the Go package the directory's generated code
// occupies, the directory holds exactly one <pkg>.gomi, and that
// interface alone supplies the member table — signatures
// requalified so the package's own type names carry the import path
// (docs/decisions/2026-11-05-package-compilation-unit.md section 2).
//
// One Loader is one session: member symbols it hands out are shared
// across every file that imports the package, which is what keeps
// symbol — and therefore type — identity meaningful downstream.
// The loader answers every consumer of imported-Gom metadata:
//
//   - resolve.Loader: Load returns the exported member table.
//   - check.SignatureSource: SignatureOf returns a member's
//     signature; a member that is a type answers its constructor.
//   - check.TypeDeclSource: TypeDecls returns the (requalified) type
//     declarations so `.field`, record literals and constructor
//     payload names type against the real declaration.
//   - resolve.CtorFieldSource (CtorFieldsOf): the payload field
//     tables `{ name = pat }` and `~label:` arguments of
//     constructor patterns resolve against.
//   - lower.TypeOracle: Variants and Fields expand wildcard match
//     arms over a foreign ADT and order record literals.
//   - FuncArity hook (ArityOf): declared arities for value
//     references and partial application.
//
// Directory lookup: the import path must equal the
// main module's path or sit under it (the generated package's Go
// import path); packages outside the main module resolve through
// `go list` in the importing file's module context — a dependency,
// a replaced or workspace sibling that carries a .gomi is a Gom
// package, everything else reports the ordinary load error that
// surfaces as E0203.
package gomimport

import (
	"encoding/json"
	"fmt"
	gotypes "go/types"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/iface"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// Config configures a Loader.
type Config struct {
	// Dir is the directory module resolution starts from — the
	// importing file's own directory. "" means the process working
	// directory.
	Dir string

	// Go is the `import go` oracle the loader delegates to — both
	// for the importing file's own `import go` clauses and for ones
	// inside loaded interfaces. It must be the same instance that
	// supplies those symbols' signatures through build.Options.Sigs,
	// or members the interfaces mint resolve nowhere. Nil makes
	// `import go` fail with E0203.
	Go resolve.Loader

	// Sigs supplies signatures for symbols this loader does not
	// mint — the Go loader's SignatureSource and the stdlib's
	// module members — consulted both for the importing file (via
	// SignatureOf's fallback) and inside loaded interfaces (their
	// own compiles get this loader as their signature source).
	Sigs check.SignatureSource

	// Modules supplies the builtin-scope module tables interface
	// compilation needs (internal/stdlib's Apply contribution).
	Modules map[string]map[string]*resolve.Symbol

	// GoBin is the go binary used to resolve external-module
	// package directories (`go list` — "" = "go"). It is only
	// consulted for import paths outside the main module.
	GoBin string

	// OnLoadGomi, when set, is called with the path of every .gomi
	// file this loader reads — the incremental build cache records
	// them as the package's interface dependencies (design.md
	// section 5). Transitive .gomi imports load through here too,
	// so one hook covers the whole dependency graph.
	OnLoadGomi func(path string)
}

// Loader is the Gom import oracle; see the package comment.
type Loader struct {
	cfg Config

	// modOnce resolves the main module's path and root directory
	// from the nearest go.mod once; failures latch into modErr.
	modOnce         sync.Once
	modPath, modDir string
	modErr          error

	// goflagsOnce reads the effective GOFLAGS (`go env GOFLAGS`,
	// falling back to the process env) once for vendorEnabled.
	goflagsOnce sync.Once
	// vendPkgsOnce parses the effective vendor tree's modules.txt
	// into vlisted — the package-path set the go command builds
	// against under vendoring.
	vendPkgsOnce sync.Once
	vlisted      map[string]bool
	goflags      string

	// workOnce resolves the workspace root holding go.work —
	// workspace vendoring materializes at <workroot>/vendor and
	// overrides the per-module vendor/ entirely. vend caches the
	// vendor directory imports resolve through (workspace first,
	// then module); empty when neither exists.
	workOnce sync.Once
	workRoot string
	// workFile is the go.work file itself — GOWORK may name a
	// file other than `go.work` (e.g. `go.work.dev`).
	workFile string
	vendOnce sync.Once
	vend     string

	// pkgs caches loaded member tables; errs the failures behind
	// them; loading marks in-progress paths (as their index into
	// stack) so a cyclic `import` reports instead of recursing —
	// stack keeps the in-flight order, which is what a re-entered
	// path's cycle chain is reconstructed from.
	pkgs    map[string]*resolve.Package
	errs    map[string]error
	loading map[string]int
	stack   []string

	// The cross-phase tables every loaded package contributes to —
	// keyed by the same requalified symbols the member tables hand
	// out.
	sigs       map[*resolve.Symbol]check.Signature
	arities    map[*resolve.Symbol]int
	tdecls     []*check.TypeDecl
	byCon      map[conKey]*check.TypeDecl
	ctorFields map[*resolve.Symbol]map[string]*resolve.Symbol
	typeCtors  map[*resolve.Symbol]map[string]*resolve.Symbol
	// methods records each imported record type's @method set —
	// the GomMethodSource view of the home checker's method table.
	methods map[conKey][]check.GomMethod

	// The implementation-import cycle check (E0606): .gom files of
	// loaded packages may import back toward the entry package —
	// a cycle the in-flight guard cannot see (their .gom files are
	// never compiled through Load) and `go build` would only reject
	// after codegen. The mirror-image case, a cycle through .gomi
	// edges alone, is the in-flight guard's job: it cannot appear
	// in this static scan either way, because .gomi imports never
	// become generated Go imports. implEdges caches each package's
	// .gom import paths; the DFS seeded at the entry package runs
	// once.
	entry        string
	implScanned  bool
	implEdges    map[string]map[string]bool
	implDone     map[string]bool
	implCycleErr *resolve.ImportCycleError
	implCycleSet map[string]bool // the package paths *on* the found cycle

	// extDirs caches external-module package directories resolved
	// through `go list -m`; extErrs the failures behind them.
	// extMods holds the importing module's build list, fetched
	// once; extErr its error.
	extDirs map[string]string
	extErrs map[string]error
	extOnce sync.Once
	extMods map[string]string
	extErr  error
}

// conKey indexes loaded type declarations by nominal identity —
// the same shape check's type tables use.
type conKey struct {
	qual, name string
}

// New returns a Loader per cfg; a nil cfg means the zero-value
// configuration (module lookup from the working directory, no Go
// oracle, no external signatures).
func New(cfg *Config) *Loader {
	c := Config{}
	if cfg != nil {
		c = *cfg
	}
	return &Loader{
		cfg:        c,
		pkgs:       map[string]*resolve.Package{},
		errs:       map[string]error{},
		loading:    map[string]int{},
		sigs:       map[*resolve.Symbol]check.Signature{},
		arities:    map[*resolve.Symbol]int{},
		byCon:      map[conKey]*check.TypeDecl{},
		methods:    map[conKey][]check.GomMethod{},
		ctorFields: map[*resolve.Symbol]map[string]*resolve.Symbol{},
		typeCtors:  map[*resolve.Symbol]map[string]*resolve.Symbol{},
		implEdges:  map[string]map[string]bool{},
		implDone:   map[string]bool{},
		extDirs:    map[string]string{},
		extErrs:    map[string]error{},
		extMods:    map[string]string{},
	}
}

// Load returns the exported member table of the package at path —
// the resolve.Loader entry. goPkg routes `import go` clauses to the
// configured Go oracle; a plain `import` loads the package's .gomi.
func (l *Loader) Load(path string, goPkg bool) (*resolve.Package, error) {
	if goPkg {
		if l.cfg.Go == nil {
			return nil, fmt.Errorf("no Go package loader configured")
		}
		return l.cfg.Go.Load(path, true)
	}
	if p, ok := l.pkgs[path]; ok {
		return p, nil
	}
	if err := l.errs[path]; err != nil {
		return nil, err
	}
	// The implementation-import scan needs no load state — it reads
	// .gom import clauses straight from disk — so it runs before the
	// first Gom load. Its verdict applies only to loads *on* the
	// found cycle: a clean import of a package that is not part of
	// the loop is not a cycle edge and must not report E0606.
	l.ensureImplScan()
	if l.implCycleErr != nil && l.implCycleSet[path] {
		return nil, l.implCycleErr
	}
	if i, inFlight := l.loading[path]; inFlight {
		// A load re-entered a package mid-load: only .gomi edges
		// recurse through Load (a loaded package's .gom files are
		// never compiled here), so the in-flight suffix is an
		// interface-only import cycle — the same E0606 shape the
		// .gom-edge scan reports, just detected dynamically.
		chain := append(append([]string{}, l.stack[i:]...), path)
		return nil, &resolve.ImportCycleError{Chain: chain}
	}
	l.loading[path] = len(l.stack)
	l.stack = append(l.stack, path)
	defer func() {
		delete(l.loading, path)
		l.stack = l.stack[:len(l.stack)-1]
	}()

	// The package compiles and extracts under its canonical path
	// (a stdlib short name unfolds to the toolchain path) — the
	// generated Go import and the members' package identity are
	// always the full path.
	p, err := l.load(canonicalPath(path))
	if err != nil {
		l.errs[path] = err
		return nil, err
	}
	l.pkgs[path] = p
	return p, nil
}

// load resolves path to a directory, compiles its .gomi, and
// surfaces the members.
func (l *Loader) load(path string) (*resolve.Package, error) {
	dir, err := l.dirOf(path)
	if err != nil {
		return nil, err
	}
	gomi, err := oneGomi(dir)
	if err != nil {
		return nil, err
	}
	if l.cfg.OnLoadGomi != nil {
		l.cfg.OnLoadGomi(gomi)
	}
	src, err := os.ReadFile(gomi)
	if err != nil {
		return nil, err
	}
	// The interface compiles against this same loader: its `import
	// go` clauses delegate to Config.Go, nested Gom imports load
	// through here, and both kinds of symbols find their signatures
	// in the shared tables (SignatureOf falls back to Config.Sigs).
	r, err := build.Compile(build.Input{Path: gomi, Src: src, Interface: true},
		&build.Options{Loader: l, Sigs: l, Modules: l.cfg.Modules})
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", gomi, err)
	}
	if r.Diagnostics.HasError() {
		// A cycle detected deeper in the graph surfaced inside this
		// compile as E0606 — propagate the typed error so the next
		// importer (and ultimately the entry file) reports the same
		// cycle instead of the opaque "does not check" wrap. The
		// chain round-trips through data.cycle: E0606 renders it
		// `a -> b -> a` and no import path contains " -> ".
		for _, d := range r.Diagnostics {
			if d.Code == "E0606" {
				if s, ok := d.Data["cycle"].(string); ok {
					return nil, &resolve.ImportCycleError{Chain: strings.Split(s, " -> ")}
				}
			}
		}
		return nil, fmt.Errorf("%s does not check: %d diagnostic(s)", gomi, len(r.Diagnostics))
	}
	p, err := l.extract(path, r.Typed)
	if err != nil {
		return nil, err
	}
	l.applyNoescape(p, iface.ScanNoescape(src))
	l.applyNotaint(p, iface.ScanNotaint(src))
	l.applyMutable(p, iface.ScanMutable(src))
	return p, nil
}

// applyNoescape applies a loaded interface's `//gom:noescape`
// directives to the package's members (spec section 6.1, SC-2) —
// the parsing lives in internal/iface, shared with the embedded
// stdlib's env.
func (l *Loader) applyNoescape(p *resolve.Package, dirs map[string][]int) {
	if p == nil {
		return
	}
	iface.ApplyNoescape(p.Members,
		func(sym *resolve.Symbol) (int, bool) { n, ok := l.arities[sym]; return n, ok },
		func(sym *resolve.Symbol) (check.Signature, bool) { sg, ok := l.sigs[sym]; return sg, ok },
		dirs)
}

// applyNotaint applies a loaded interface's `//gom:result-notaint`
// directives to the package's members (spec section 6.1) — the
// parsing lives in internal/iface, shared with the embedded
// stdlib's env.
func (l *Loader) applyNotaint(p *resolve.Package, dirs map[string][]int) {
	if p == nil {
		return
	}
	iface.ApplyNotaint(p.Members,
		func(sym *resolve.Symbol) (int, bool) { n, ok := l.arities[sym]; return n, ok },
		func(sym *resolve.Symbol) (check.Signature, bool) { sg, ok := l.sigs[sym]; return sg, ok },
		dirs)
}

// applyMutable applies a loaded interface's `//gom:mutable`
// directives to the package's type members (spec section 10) —
// the parsing lives in internal/iface, shared with the embedded
// stdlib's env.
func (l *Loader) applyMutable(p *resolve.Package, names []string) {
	if p == nil {
		return
	}
	iface.ApplyMutable(p.Members, names)
}

// stdlibPaths maps the toolchain's Gom-written standard-library
// modules to their full Go import paths — `import "list"` names
// github.com/podhmo/gom/stdlib/list in every module context, so
// an external module reaches the toolchain's stdlib without
// spelling the path (mirrored by hand from internal/stdlib's
// `packages` table — gomimport cannot import the wiring package
// without a cycle).
var stdlibPaths = map[string]string{
	"list":   "github.com/podhmo/gom/stdlib/list",
	"option": "github.com/podhmo/gom/stdlib/option",
	"result": "github.com/podhmo/gom/stdlib/result",
	"cmp":    "github.com/podhmo/gom/stdlib/cmp",
}

// StdlibShortNames returns the importable stdlib short names,
// sorted — the keys of stdlibPaths. `gom list` prints them; the
// table is the enumeration source because `cmp` has no embedded
// .gomi and an FS walk of gomstdlib.Gomis would miss it.
func StdlibShortNames() []string {
	names := make([]string, 0, len(stdlibPaths))
	for name := range stdlibPaths {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ExternalPackages enumerates the importable Gom packages visible
// from dir's module context: every module in `go list -m all` whose
// source directory is known — the main module, replace targets and
// workspace `use` members included, modules still absent from the
// module cache skipped. A directory counts when it holds exactly
// one .gomi — the same package contract `import "path"` resolves
// through — and its import path is the owning module's path plus
// the directory's relative path. When vendor mode is in effect
// (vendorEnabled), <modRoot>/vendor is walked the same way — a
// vendored directory carrying a .gomi is itself the resolution
// proof (dirOf consults vendor/ first), so its import path is the
// slash path relative to vendor/. The result is sorted and
// deduplicated.
func ExternalPackages(dir, gobin string) ([]string, error) {
	l := New(&Config{Dir: dir, GoBin: gobin})
	// `go list -m all` answers the post-replace build list — main
	// module included — so it is the whole candidate set on its
	// own; when it fails (a broken module graph), external imports
	// would fail the same way, so the listing propagates rather
	// than emit a silently partial set.
	mods, err := l.extModules()
	mainPath, mainDir, _ := l.module()
	vend := ""
	if l.vendorEnabled() {
		vend = l.vendorDir()
	}
	// Under vendoring the vendored set is the importable set — a
	// module graph that cannot be listed (offline, broken
	// metadata) cannot gate what `go build` finds in vendor/, so
	// emit the vendored walk instead of propagating.
	if err != nil && vend == "" {
		return nil, err
	}
	candidates := map[string]string{}
	for p, d := range mods {
		if d != "" {
			candidates[p] = d
		}
	}
	// Workspace `use` members are importable whatever the module
	// graph reports — under workspace vendoring `go list -m all`
	// fails outright and siblings would otherwise vanish from the
	// listing. Read them off go.work + each member's go.mod.
	for mp, md := range l.workUse() {
		if _, ok := candidates[mp]; !ok {
			candidates[mp] = md
		}
	}
	// The main module's path, when it can be read — best-effort,
	// unresolvable simply leaves every candidate on the
	// longest-prefix rule. It is added to the candidates on top of
	// `go list -m all`: inside a workspace that does not `use` this
	// module the build list omits it, while imports in its files
	// still resolve against its own go.mod (dirOf's rule — its
	// paths also win ahead of any external prefix match).
	if mainPath != "" && mainDir != "" {
		candidates[mainPath] = mainDir
	}
	// resolvesTo reports which candidate module's directory dirOf
	// would consult for imp — the importing module's own path
	// first, then the longest candidate prefix. An imp derived
	// under one module but routed to another's directory must not
	// be advertised: the routed directory lacks the .gomi.
	resolvesTo := func(imp string) string {
		if mainPath != "" && (imp == mainPath || strings.HasPrefix(imp, mainPath+"/")) {
			return mainPath
		}
		best := ""
		for p := range candidates {
			if p != mainPath && len(p) > len(best) &&
				(imp == p || strings.HasPrefix(imp, p+"/")) {
				best = p
			}
		}
		return best
	}
	// The vendor manifest's package set decides which paths the
	// vend walk may advertise — and which the module walk must
	// not: a manifest-listed path resolves to its vendor copy
	// even when that copy carries no .gomi, so advertising it
	// from the module tree would name an unloadable import.
	var listed map[string]bool
	if vend != "" {
		listed = l.vendPkgs(vend)
	}
	var out []string
	seen := map[string]bool{}
	for mp, md := range candidates {
		mp, md := mp, md
		err := filepath.WalkDir(md, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil {
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if p != md {
				base := d.Name()
				if strings.HasPrefix(base, ".") || base == "vendor" {
					return filepath.SkipDir
				}
				// A nested module's packages belong to its own
				// module path, not this one's — the nested root
				// appears as its own candidate when it is in the
				// build list.
				if _, serr := os.Stat(filepath.Join(p, "go.mod")); serr == nil {
					return filepath.SkipDir
				}
			}
			if _, gerr := oneGomi(p); gerr != nil {
				return nil
			}
			rel, rerr := filepath.Rel(md, p)
			if rerr != nil {
				return nil
			}
			imp := mp
			if rel != "." {
				imp = mp + "/" + filepath.ToSlash(rel)
			}
			if resolvesTo(imp) != mp {
				return nil
			}
			// A manifest-listed path resolves to its vendor copy —
			// the vend walk advertises it iff that copy carries a
			// .gomi. The main module's own paths resolve locally
			// first in dirOf, so the manifest can never shadow them.
			if mp != mainPath && listed[imp] {
				return nil
			}
			if !seen[imp] {
				seen[imp] = true
				out = append(out, imp)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	// Vendored packages: the go command resolves them through
	// vendor/ ahead of every module path, so a .gomi-bearing
	// directory there is importable by its vendored path
	// regardless of which module the path belongs to — no
	// resolvesTo check. A vendored imp already advertised via a
	// module candidate collapses into one entry.
	if vend != "" {
		err := filepath.WalkDir(vend, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil || !d.IsDir() {
				return nil
			}
			if _, gerr := oneGomi(p); gerr != nil {
				return nil
			}
			rel, rerr := filepath.Rel(vend, p)
			if rerr != nil || rel == "." {
				return nil
			}
			imp := filepath.ToSlash(rel)
			// modules.txt is the go command's vendored set — a
			// stray dir with a .gomi but no listing is not
			// importable under vendoring. Only filter when the
			// manifest parsed to anything.
			if len(listed) > 0 && !listed[imp] {
				return nil
			}
			if !seen[imp] {
				seen[imp] = true
				out = append(out, imp)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

// canonicalPath resolves a clause path to the package's real
// import path: the stdlib short names, otherwise the path itself.
// The canonical path is what the .gomi's members and the
// generated Go import record — the clause's short spelling stays
// the caller's cache key and bound module name.
func canonicalPath(path string) string {
	return CanonicalPath(path)
}

// CanonicalPath resolves a clause path to the package's real
// import path — the stdlib short names unfold, everything else
// passes through. Exported for cmd/gom, which needs the same
// mapping for its import-dependency ordering without reaching
// the unexported table.
func CanonicalPath(path string) string {
	if p, ok := stdlibPaths[path]; ok {
		return p
	}
	return path
}

// dirOf maps an import path to its package directory: the main
// module's root (found by walking up from Config.Dir to go.mod)
// plus the module-relative remainder of the path, or — for paths
// outside the main module — the directory `go list` reports in
// the importing file's module context (a dependency's directory
// in the module cache, a replaced or workspace sibling). The
// external case is a plain directory lookup: oneGoMi decides
// whether the package is a Gom package at all.
func (l *Loader) dirOf(path string) (string, error) {
	modPath, modDir, modErr := l.module()
	if modErr == nil {
		switch {
		case path == modPath:
			return modDir, nil
		case strings.HasPrefix(path, modPath+"/"):
			return filepath.Join(modDir, filepath.FromSlash(strings.TrimPrefix(path, modPath+"/"))), nil
		}
	}
	if d, ok := l.extDirs[path]; ok {
		return d, nil
	}
	if err := l.extErrs[path]; err != nil {
		return "", err
	}
	d, err := l.pkgDir(path)
	if err != nil {
		l.extErrs[path] = err
		return "", err
	}
	l.extDirs[path] = d
	return d, nil
}

// pkgDir resolves an external-module package path to its
// directory by asking `go list -m` for the module that owns it
// (longest module-path prefix wins) and appending the remainder —
// the same shape the main-module rule has. Package-level `go
// list` would refuse a Gom-only directory: before `gom build` it
// holds no .go files at all, and the .gomi is what makes it a
// package here anyway. The module list is fetched once per
// loader and run in Config.Dir, so the importing file's requires,
// replaces and workspace are the resolution context — the same
// visibility the generated Go import will have.
func (l *Loader) pkgDir(path string) (string, error) {
	// A vendored package lives at <modRoot>/vendor/<import path> —
	// prefer it directly: `go list -m` cannot see the build list
	// under vendoring (the -mod=mod retry below is a fallback), and
	// the module cache may never have held the source. Only when
	// vendor mode is actually in effect, though: `GOFLAGS=-mod=mod`
	// (or =readonly) makes the go command ignore vendor/ entirely,
	// and the generated Go import must see the same source the go
	// tool will use.
	if l.vendorEnabled() {
		if vd := l.vendorDir(); vd != "" {
			v := filepath.Join(vd, filepath.FromSlash(path))
			if st, serr := os.Stat(v); serr == nil && st.IsDir() {
				// modules.txt is the go command's vendored set — a
				// listed path resolves to the vendored copy even
				// without a .gomi (loading then reports no interface,
				// matching what `go build` would compile against).
				// An unlisted dir answers only when it carries the
				// interface — a mere parent of deeper vendored
				// packages must not mask a workspace member
				// resolving the same path.
				if l.vendPkgs(vd)[path] {
					return v, nil
				}
				if _, gerr := oneGomi(v); gerr == nil {
					return v, nil
				}
			}
		}
	}
	mods, err := l.extModules()
	if err != nil {
		// Workspace `use` members stay importable when the module
		// graph cannot be listed — `go work` resolves them from
		// their use dirs directly (workspace vendoring, offline),
		// and ExternalPackages advertises them from the same
		// source. Fall back to that reading before giving up.
		best, bdir := "", ""
		for mp, md := range l.workUse() {
			if len(mp) > len(best) && (path == mp || strings.HasPrefix(path, mp+"/")) {
				best, bdir = mp, md
			}
		}
		if best == "" {
			return "", err
		}
		if best == path {
			return bdir, nil
		}
		return filepath.Join(bdir, filepath.FromSlash(strings.TrimPrefix(path, best+"/"))), nil
	}
	best := ""
	dir := ""
	for p, d := range mods {
		if path == p || strings.HasPrefix(path, p+"/") {
			if len(p) > len(best) {
				best, dir = p, d
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("package %q is not importable from %q — no module in the build list provides it", path, l.cfg.Dir)
	}
	if dir == "" {
		dir, err = l.moduleDir(best)
		if err != nil {
			return "", err
		}
	}
	if best == path {
		return dir, nil
	}
	return filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(path, best+"/"))), nil
}

// extModules lists the importing module's build list — every
// module it can see, with the directory it occupies (module
// cache, replace target, or workspace member).
func (l *Loader) extModules() (map[string]string, error) {
	l.extOnce.Do(func() {
		out, err := l.goListModules()
		// A vendoring module rejects `go list -m all` outright
		// ("can't compute 'all' using the vendor directory") — the
		// go.mod build list is still authoritative, so retry with
		// -mod=mod when a vendor directory is present. The retry
		// sits behind the failure so workspace modules (where -mod
		// is constrained) are untouched.
		if err != nil {
			if vd := l.vendorDir(); vd != "" {
				if _, serr := os.Stat(filepath.Join(vd, "modules.txt")); serr == nil {
					// Workspace mode rejects -mod=mod; the
					// go.work build list is already exact,
					// so readonly bypasses vendor the same
					// way there.
					flag := "-mod=mod"
					if l.workRootFn() != "" {
						flag = "-mod=readonly"
					}
					out, err = l.goListModules(flag)
				}
			}
		}
		if err != nil {
			l.extErr = err
			return
		}
		for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
			p, d, _ := strings.Cut(line, "|")
			if p != "" {
				// An empty Dir means the module is in the
				// build list but its source is not in the
				// module cache yet; keep the entry so the
				// longest-prefix match still works and let
				// pkgDir fetch it on demand.
				l.extMods[p] = d
			}
		}
	})
	return l.extMods, l.extErr
}

// goListModules runs `go list -m -f '{{.Path}}|{{.Dir}}' all` in
// Config.Dir. Note `go list -m` only reads the module graph — it
// does not download module sources, so an entry's .Dir can be
// empty until something fetches it.
func (l *Loader) goListModules(flags ...string) (string, error) {
	args := append([]string{"list"}, flags...)
	args = append(args, "-m", "-f", "{{.Path}}|{{.Dir}}", "all")
	cmd := exec.Command(l.gobin(), args...)
	if l.cfg.Dir != "" {
		cmd.Dir = l.cfg.Dir
	}
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("go list -m: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("go list -m: %w", err)
	}
	return string(out), nil
}

// moduleDir fetches the source directory of a module in the build
// list via `go mod download` — needed when `go list -m` reported
// an empty Dir because nothing had downloaded the module yet
// (a Gom-only dependency no Go package references). `go mod
// download` works on the go.mod graph directly, so it is also
// the answer under vendoring.
func (l *Loader) moduleDir(path string) (string, error) {
	cmd := exec.Command(l.gobin(), "mod", "download", "-json", path)
	if l.cfg.Dir != "" {
		cmd.Dir = l.cfg.Dir
	}
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("go mod download %s: %s", path, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("go mod download %s: %w", path, err)
	}
	var dl struct {
		Dir string
	}
	if err := json.Unmarshal(out, &dl); err != nil || dl.Dir == "" {
		return "", fmt.Errorf("go mod download %s: no directory in result", path)
	}
	return dl.Dir, nil
}

func (l *Loader) gobin() string {
	if l.cfg.GoBin != "" {
		return l.cfg.GoBin
	}
	return "go"
}

// vendorDir returns the vendor directory the go command
// resolves imports through — <workroot>/vendor when a workspace
// governs (its modules.txt must carry the "## workspace" marker
// or the tree is a stale module vendor the go command ignores),
// else the module's own vendor/. Empty when neither applies. In
// workspace mode a member module's vendor/ is never consulted.
func (l *Loader) vendorDir() string {
	l.vendOnce.Do(func() {
		if wr := l.workRootFn(); wr != "" {
			mt := filepath.Join(wr, "vendor", "modules.txt")
			if b, err := os.ReadFile(mt); err == nil && strings.Contains(string(b), "## workspace") {
				l.vend = filepath.Join(wr, "vendor")
			}
			return
		}
		if _, modDir, err := l.module(); err == nil {
			v := filepath.Join(modDir, "vendor")
			if st, serr := os.Stat(v); serr == nil && st.IsDir() {
				l.vend = v
			}
		}
	})
	return l.vend
}

// vendPkgs returns the package paths the vendor tree's
// modules.txt lists — the set the go command actually builds
// against under vendoring.
func (l *Loader) vendPkgs(vend string) map[string]bool {
	l.vendPkgsOnce.Do(func() {
		l.vlisted = map[string]bool{}
		if vend == "" {
			return
		}
		b, err := os.ReadFile(filepath.Join(vend, "modules.txt"))
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			l.vlisted[line] = true
		}
	})
	return l.vlisted
}

// workUse parses the go.work at wr for `use` member dirs and
// maps each member's module path to its directory — all
// filesystem reads, so workspace siblings stay enumerable when
// the module graph cannot be listed (workspace vendoring,
// offline). GOWORK may name a file other than `go.work` — read
// the one the loader resolved.
func (l *Loader) workUse() map[string]string {
	out := map[string]string{}
	wr, wf := l.workRootFn(), l.workFile
	if wr == "" || wf == "" {
		return out
	}
	b, err := os.ReadFile(wf)
	if err != nil {
		return out
	}
	inUse := false
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "//", 2)[0])
		if line == "" {
			continue
		}
		if inUse {
			if strings.HasPrefix(line, ")") {
				inUse = false
				continue
			}
		} else if rest, ok := strings.CutPrefix(line, "use"); ok {
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "(") {
				inUse = true
				continue
			}
			line = rest
		} else {
			continue
		}
		usePath := filepath.FromSlash(strings.Trim(line, `"`))
		dir := usePath
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(wr, dir)
		}
		gm, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err != nil {
			continue
		}
		for _, gl := range strings.Split(string(gm), "\n") {
			gl = strings.TrimSpace(strings.SplitN(gl, "//", 2)[0])
			f := strings.Fields(gl)
			if len(f) >= 2 && f[0] == "module" {
				out[f[1]] = dir
				break
			}
		}
	}
	return out
}

// workRoot returns the directory holding the go.work governing
// Config.Dir — "" when there is none or GOWORK disables it.
// `go env GOWORK` answers it; the env var alone is not enough
// because an unset GOWORK still auto-discovers a go.work walking
// up the tree.
func (l *Loader) workRootFn() string {
	l.workOnce.Do(func() {
		cmd := exec.Command(l.gobin(), "env", "GOWORK")
		if l.cfg.Dir != "" {
			cmd.Dir = l.cfg.Dir
		}
		out, err := cmd.Output()
		if err != nil {
			return
		}
		wf := strings.TrimSpace(string(out))
		if wf != "" && wf != "off" {
			l.workRoot = filepath.Dir(wf)
			l.workFile = wf
		}
	})
	return l.workRoot
}

// vendorEnabled reports whether the go command would resolve
// imports through vendor/ under this loader's effective GOFLAGS
// — `go env GOFLAGS` folds the persisted `go env -w` file in
// below the process env, so a saved -mod value applies too.
func (l *Loader) vendorEnabled() bool {
	l.goflagsOnce.Do(func() {
		cmd := exec.Command(l.gobin(), "env", "GOFLAGS")
		if l.cfg.Dir != "" {
			cmd.Dir = l.cfg.Dir
		}
		out, err := cmd.Output()
		if err != nil {
			l.goflags = os.Getenv("GOFLAGS")
			return
		}
		l.goflags = strings.TrimSpace(string(out))
	})
	return vendorEnabled(l.goflags)
}

// vendorEnabled parses the -mod rule out of a GOFLAGS value: the
// go command vendores unless an explicit -mod other than vendor
// is in effect (an explicit -mod=mod or -mod=readonly makes Go
// ignore vendor/ outright). Mirrors the go command's vendoring
// rule for modules with go >= 1.14, which Gom requires anyway.
func vendorEnabled(goflags string) bool {
	// Repeated -mod flags apply in order — the last value wins, as
	// with the go command's own flag parsing.
	mod := ""
	fields := strings.Fields(goflags)
	for i, f := range fields {
		f = strings.TrimLeft(f, "-")
		if f == "mod" {
			if i+1 < len(fields) {
				mod = fields[i+1]
			}
		} else if v, ok := strings.CutPrefix(f, "mod="); ok {
			mod = v
		}
	}
	return mod == "" || mod == "vendor"
}

// module reports the main module's path and root directory, read
// from the go.mod nearest Config.Dir. The module line is parsed
// directly — `go list -m` would answer the same for the main module
// without dragging workspace state in, and the .gomi contract is
// module-local anyway.
func (l *Loader) module() (string, string, error) {
	l.modOnce.Do(func() {
		dir := l.cfg.Dir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		dir, err := filepath.Abs(dir)
		if err != nil {
			l.modErr = err
			return
		}
		for {
			gm := filepath.Join(dir, "go.mod")
			b, err := os.ReadFile(gm)
			if err == nil {
				for line := range strings.SplitSeq(string(b), "\n") {
					fields := strings.Fields(line)
					if len(fields) == 2 && fields[0] == "module" {
						l.modPath, l.modDir = fields[1], dir
						return
					}
				}
				l.modErr = fmt.Errorf("%s has no module directive", gm)
				return
			}
			if !os.IsNotExist(err) {
				l.modErr = err
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				l.modErr = fmt.Errorf("no go.mod found above %s", l.cfg.Dir)
				return
			}
			dir = parent
		}
	})
	return l.modPath, l.modDir, l.modErr
}

// oneGomi returns the single .gomi file directly inside dir — the
// unit's public interface. Zero or several both fail: a missing one
// is the ordinary "not a Gom package" case, and several means the
// directory does not conform to the one-interface-per-package rule.
func oneGomi(dir string) (string, error) {
	ms, err := filepath.Glob(filepath.Join(dir, "*.gomi"))
	if err != nil {
		return "", err
	}
	switch len(ms) {
	case 0:
		return "", fmt.Errorf("no .gomi interface file in %s", dir)
	case 1:
		return ms[0], nil
	default:
		return "", fmt.Errorf("several .gomi files in %s — a package has exactly one interface", dir)
	}
}

// extract turns a compiled interface's typed declarations into the
// importable member table plus this loader's cross-phase tables.
// Symbols are mutated in place — requalified to path — which is
// sound because the .gomi compile is private to this load: the same
// symbol objects then serve the member table, the signature table,
// the type-declaration tables and the pattern-time constructor
// tables, so identity stays consistent across every consumer.
func (l *Loader) extract(path string, tf *check.File) (*resolve.Package, error) {
	if tf == nil {
		return nil, fmt.Errorf("interface produced no typed declarations")
	}
	own := map[string]bool{}
	for _, d := range tf.Decls {
		if td, ok := d.(*check.TypeDecl); ok && td.Sym != nil {
			own[td.Sym.Name] = true
		}
	}
	p := &resolve.Package{Path: path, Members: map[string]*resolve.Symbol{}}
	for _, d := range tf.Decls {
		switch d := d.(type) {
		case *check.FnDecl:
			sym := d.Sym
			if sym == nil || sym.TestOnly {
				continue
			}
			sym.Package = path
			p.Members[sym.Name] = sym
			sig := check.Signature{Type: requalify(d.Sig, own, path)}
			if !d.Thunk && d.Sig != nil {
				sig.DeclArity = len(d.Sig.Params)
				l.arities[sym] = len(d.Sig.Params)
			}
			l.sigs[sym] = sig
			if d.Method && d.Sig != nil && len(d.Sig.Params) > 0 {
				ptrRecv := false
				base := types.Chase(d.Sig.Params[0].Type)
				if p, ok := base.(*types.Con); ok && p.Qual == "" && p.Name == "Ptr" && len(p.Args) == 1 {
					ptrRecv = true
					base = types.Chase(p.Args[0])
				}
				if rc, ok := base.(*types.Con); ok {
					var ps []types.FuncParam
					for _, fp := range d.Sig.Params[1:] {
						if types.Equal(fp.Type, types.Unit) {
							continue
						}
						ps = append(ps, types.FuncParam{Label: fp.Label, Type: requalify(fp.Type, own, path)})
					}
					key := conKey{qual: path, name: rc.Name}
					mname := d.MethodName
					if mname == "" {
						mname = core.ExportName(sym.Name)
					}
					l.methods[key] = append(l.methods[key], check.GomMethod{
						Name:    mname,
						Sig:     types.Fn(requalify(d.Sig.Ret, own, path), ps...),
						PtrRecv: ptrRecv,
					})
				}
			}
		case *check.LetDecl:
			for _, sym := range letSyms(d.Pat) {
				if sym == nil || sym.TestOnly {
					continue
				}
				sym.Package = path
				p.Members[sym.Name] = sym
				mt := requalify(patType(d.Pat, sym), own, path)
				if vars := memberVars(mt); len(vars) > 0 {
					sym.Kind = resolve.KindFunc
					if fnT, ok := types.Chase(mt).(*types.Func); ok {
						// `let apply : (a) -> a` — a
						// function-typed member. The
						// implementation can only
						// generalize it as a real fn
						// declaration (`func Apply[A](x A)
						// A`): a thunk wrapper `() -> (a ->
						// a)` cannot exist — types.Fn
						// flattens it away — so the member
						// keeps the function signature and
						// declared arity like an `fn` item.
						l.sigs[sym] = check.Signature{
							Type:       mt,
							TypeParams: vars,
							DeclArity:  len(fnT.Params),
						}
						l.arities[sym] = len(fnT.Params)
						continue
					}
					// `let empty : List a` — a generalized
					// value. The implementation emits the
					// thunk `func Empty[T]() T` for its
					// generalized let (checkLetDecl), so the
					// member carries that niladic signature
					// and the Thunk flag tells check's valOf
					// to type `p.empty` at T and lower's
					// funcRefOrFlatten to emit the call.
					sym.Thunk = true
					l.sigs[sym] = check.Signature{
						Type:       types.Fn(mt),
						TypeParams: vars,
					}
					continue
				}
				l.sigs[sym] = check.Signature{Type: mt}
			}
		case *check.TypeDecl:
			if d.Sym == nil {
				continue
			}
			d.Sym.Package = path
			l.requalifyDecl(d, own, path)
			l.tdecls = append(l.tdecls, d)
			l.byCon[conKey{qual: path, name: d.Sym.Name}] = d
			p.Members[d.Sym.Name] = d.Sym
			// The type's own signature is its constructor — what
			// `pkg.T` in annotation position resolves to (arity
			// included for generic declarations). An alias answers
			// the aliased type itself instead.
			if al, ok := d.Body.(*check.AliasType); ok {
				l.sigs[d.Sym] = check.Signature{Type: requalify(al.Type, own, path)}
			} else {
				args := make([]types.Type, len(d.TypeParams))
				for i, v := range d.TypeParams {
					args[i] = v
				}
				l.sigs[d.Sym] = check.Signature{
					Type: &types.Con{Qual: path, Name: d.Sym.Name, Args: args},
				}
			}
			if s, ok := d.Body.(*check.SumType); ok {
				parent := &types.Con{Qual: path, Name: d.Sym.Name, Args: conArgs(d.TypeParams)}
				// The per-type ctor table `pkg.Type.Ctor`
				// resolves against (resolve.CtorsOf).
				ctab := make(map[string]*resolve.Symbol, len(s.Variants))
				for i := range s.Variants {
					v := &s.Variants[i]
					if v.Sym == nil {
						continue
					}
					v.Sym.Package = path
					p.Members[v.Sym.Name] = v.Sym
					ctab[v.Sym.Name] = v.Sym
					params := make([]types.FuncParam, len(v.Fields))
					for j, f := range v.Fields {
						params[j] = types.FuncParam{Label: f.Label, Type: f.Type}
					}
					var sig types.Type = parent
					if len(params) > 0 {
						sig = &types.Func{Params: params, Ret: parent}
					}
					l.sigs[v.Sym] = check.Signature{Type: sig}
					l.ctorFields[v.Sym] = fieldTable(v.Fields)
				}
				l.typeCtors[d.Sym] = ctab
			}
			if rc, ok := d.Body.(*check.RecordType); ok {
				l.ctorFields[d.Sym] = fieldTable(rc.Fields)
			}
		case *check.TestDecl:
			// Interfaces cannot declare tests.
		default:
			panic("unreachable")
		}
	}
	// A member whose generated Go name another member claims
	// misbinds every imported reference — `p.foo` compiles to
	// `p.Foo`, the other member's name. The declaring package's
	// conformance check reports the collision as E0607; marking the
	// loser here lets an importer report E0607 at the reference
	// too, instead of silently emitting the wrong name. The mark
	// replays codegen's claiming over the member table: types take
	// `ExportName` first, then term members in (base, Gom name)
	// order — a constructor member emits type-qualified
	// (`T_Variant`) and never claims a bare name.
	var typeMembers, termMembers []*resolve.Symbol
	for _, m := range p.Members {
		switch m.Kind {
		case resolve.KindType:
			typeMembers = append(typeMembers, m)
		case resolve.KindVar, resolve.KindFunc:
			termMembers = append(termMembers, m)
		}
	}
	sort.Slice(typeMembers, func(i, j int) bool {
		return typeMembers[i].Name < typeMembers[j].Name
	})
	sort.Slice(termMembers, func(i, j int) bool {
		bi, bj := core.ExportName(termMembers[i].Name), core.ExportName(termMembers[j].Name)
		if bi != bj {
			return bi < bj
		}
		return termMembers[i].Name < termMembers[j].Name
	})
	claimed := map[string]*resolve.Symbol{}
	for _, m := range typeMembers {
		base := core.ExportName(m.Name)
		if claimed[base] == nil {
			claimed[base] = m
		} else {
			m.EmitClash = base
		}
	}
	for _, m := range termMembers {
		base := core.ExportName(m.Name)
		if claimed[base] == nil {
			claimed[base] = m
		} else {
			m.EmitClash = base
		}
	}
	return p, nil
}

// requalifyDecl rewrites a loaded type declaration's field and
// alias types so every reference to one of the package's own type
// names carries the import path — `Shape` becomes
// `example.com/m/shapes.Shape` — while builtin and other-package
// names stay untouched.
func (l *Loader) requalifyDecl(td *check.TypeDecl, own map[string]bool, path string) {
	switch body := td.Body.(type) {
	case *check.SumType:
		for i := range body.Variants {
			for j := range body.Variants[i].Fields {
				body.Variants[i].Fields[j].Type =
					requalify(body.Variants[i].Fields[j].Type, own, path)
			}
		}
	case *check.RecordType:
		for j := range body.Fields {
			body.Fields[j].Type = requalify(body.Fields[j].Type, own, path)
		}
	case *check.AliasType:
		body.Type = requalify(body.Type, own, path)
	case *check.AbstractType, nil:
	default:
		panic("unreachable")
	}
}

// --- the checker seam ---

// SignatureOf returns the signature of a member symbol — the
// check.SignatureSource entry. Symbols this loader minted answer
// from the loaded tables; anything else defers to Config.Sigs so a
// chain built on this loader still answers Go members and stdlib
// modules.
func (l *Loader) SignatureOf(sym *resolve.Symbol) (check.Signature, bool) {
	if sig, ok := l.sigs[sym]; ok {
		return sig, true
	}
	if l.cfg.Sigs != nil {
		return l.cfg.Sigs.SignatureOf(sym)
	}
	return check.Signature{}, false
}

// TypeDecls returns every loaded package's (requalified) type
// declarations — the check.TypeDeclSource entry.
func (l *Loader) TypeDecls() []*check.TypeDecl {
	return l.tdecls
}

// MethodOf defers the optional check.MethodSource protocol to
// Config.Sigs — this loader mints no Go method symbols.
func (l *Loader) MethodOf(recv types.Type, name string) (*resolve.Symbol, check.Signature, bool) {
	if ms, ok := l.cfg.Sigs.(check.MethodSource); ok {
		return ms.MethodOf(recv, name)
	}
	return nil, check.Signature{}, false
}

// InterfaceAssignable defers check.InterfaceSource to Config.Sigs.
func (l *Loader) InterfaceAssignable(from, to types.Type) bool {
	if is, ok := l.cfg.Sigs.(check.InterfaceSource); ok {
		return is.InterfaceAssignable(from, to)
	}
	return false
}

// FuncNOfNamed defers check.FuncNSource to Config.Sigs.
func (l *Loader) FuncNOfNamed(t types.Type) (types.Type, bool) {
	if fs, ok := l.cfg.Sigs.(check.FuncNSource); ok {
		return fs.FuncNOfNamed(t)
	}
	return nil, false
}

// FieldOf defers check.FieldSource to Config.Sigs.
func (l *Loader) FieldOf(recv types.Type, name string) (*resolve.Symbol, check.Signature, types.Type, bool) {
	if fs, ok := l.cfg.Sigs.(check.FieldSource); ok {
		return fs.FieldOf(recv, name)
	}
	return nil, check.Signature{}, nil, false
}

// GoComparable defers check.ComparableSource to Config.Sigs.
func (l *Loader) GoComparable(t types.Type) bool {
	if cs, ok := l.cfg.Sigs.(check.ComparableSource); ok {
		return cs.GoComparable(t)
	}
	return false
}

// GoSatisfies defers check.GoConstraintSource to Config.Sigs.
func (l *Loader) GoSatisfies(targs []types.Type, i int, con check.GoConstraint) check.GoVerdict {
	if cs, ok := l.cfg.Sigs.(check.GoConstraintSource); ok {
		return cs.GoSatisfies(targs, i, con)
	}
	return check.GoVerdictNo
}

// GoConvertible defers check.ConvertSource to Config.Sigs.
func (l *Loader) GoConvertible(from, to types.Type) (bool, bool) {
	if cs, ok := l.cfg.Sigs.(check.ConvertSource); ok {
		return cs.GoConvertible(from, to)
	}
	return false, false
}

// GoUnderlyingKind defers check.UnderlyingSource to Config.Sigs.
func (l *Loader) GoUnderlyingKind(t types.Type) (gotypes.BasicKind, bool) {
	if us, ok := l.cfg.Sigs.(check.UnderlyingSource); ok {
		return us.GoUnderlyingKind(t)
	}
	return gotypes.Invalid, false
}

// BindExtern defers check.ExternSource to Config.Sigs — the Go
// loader validates an `extern go` entry against the raw Go
// signature, reporting E0509 issues on a mismatch and registering
// the override's Go-view facts on the extern symbol on a clean
// bind. A missing or non-implementing Sigs leaves the entry
// unverified: the empty answer tells the checker (and a
// check.chain wrapping this loader) the entry was not bound, so
// the declared signature stands alone — the same fallback a
// source with no ExternSource produces.
func (l *Loader) BindExtern(sym *resolve.Symbol, decl check.Signature) (check.Signature, []check.ExternIssue) {
	if es, ok := l.cfg.Sigs.(check.ExternSource); ok {
		return es.BindExtern(sym, decl)
	}
	return check.Signature{}, nil
}

// --- the resolver seam ---

// CtorFieldsOf returns the named payload slots of a constructor (or
// record type) the loaded packages declare — the
// resolve.Config.CtorFields hook, keyed by field param name like
// the resolver's own tables.
func (l *Loader) CtorFieldsOf(ctor *resolve.Symbol) map[string]*resolve.Symbol {
	return l.ctorFields[ctor]
}

// CtorsOf returns the constructor table of a type the loaded
// packages declare — the resolve.Config.Ctors hook that unlocks
// `pkg.Type.Ctor`. Records, aliases and abstract types have no
// constructors and answer nil, like a symbol this loader never
// minted.
func (l *Loader) CtorsOf(typeSym *resolve.Symbol) map[string]*resolve.Symbol {
	return l.typeCtors[typeSym]
}

// --- the lowering seam ---

// ArityOf returns a loaded function member's declared arity — the
// lower.Env.FuncArity hook shape — or -1 when sym is not a loaded
// function.
func (l *Loader) ArityOf(sym *resolve.Symbol) int {
	if a, ok := l.arities[sym]; ok {
		return a
	}
	return -1
}

// FuncTypeOf returns a loaded function member's declared signature —
// the lower.Env.FuncType hook shape — with its own type parameters
// still rigid, or false when sym is not a loaded function.
func (l *Loader) FuncTypeOf(sym *resolve.Symbol) (*types.Func, bool) {
	s, ok := l.sigs[sym]
	if !ok {
		return nil, false
	}
	f, ok := s.Type.(*types.Func)
	return f, ok
}

// Variants returns the constructor set of a loaded sum type —
// lower.TypeOracle — with field types instantiated at the queried
// constructor's arguments.
func (l *Loader) Variants(c *types.Con) []lower.Variant {
	td := l.byCon[conKey{qual: c.Qual, name: c.Name}]
	if td == nil {
		return nil
	}
	s, ok := td.Body.(*check.SumType)
	if !ok {
		return nil
	}
	sub := instantiate(td.TypeParams, c.Args)
	out := make([]lower.Variant, len(s.Variants))
	for i, v := range s.Variants {
		fields := make([]check.TField, len(v.Fields))
		for j, f := range v.Fields {
			fields[j] = f
			fields[j].Type = sub.Apply(f.Type)
		}
		out[i] = lower.Variant{Sym: v.Sym, Fields: fields}
	}
	return out
}

// Fields returns a loaded record type's fields in declaration
// order — lower.TypeOracle — instantiated at the queried
// constructor's arguments.
func (l *Loader) Fields(c *types.Con) []check.TField {
	td := l.byCon[conKey{qual: c.Qual, name: c.Name}]
	if td == nil {
		return nil
	}
	rc, ok := td.Body.(*check.RecordType)
	if !ok {
		return nil
	}
	sub := instantiate(td.TypeParams, c.Args)
	out := make([]check.TField, len(rc.Fields))
	for i, f := range rc.Fields {
		out[i] = f
		out[i].Type = sub.Apply(f.Type)
	}
	return out
}

// DefinedField reports the declared field of a one-field record
// emitting as the Go defined type `type X F` — lower.TypeOracle.
func (l *Loader) DefinedField(c *types.Con) (check.TField, bool) {
	td := l.byCon[conKey{qual: c.Qual, name: c.Name}]
	if td == nil {
		return check.TField{}, false
	}
	return check.DefinedField(td)
}

// conArgs turns declared type parameters into the constructor's
// own argument list — `type Shape a` applied to itself.
func conArgs(params []*types.Var) []types.Type {
	args := make([]types.Type, len(params))
	for i, v := range params {
		args[i] = v
	}
	return args
}

// instantiate maps a declaration's type parameters to a queried
// constructor's arguments.
func instantiate(params []*types.Var, args []types.Type) types.Subst {
	sub := types.Subst{}
	for i, v := range params {
		if i < len(args) {
			sub[v.ID] = args[i]
		}
	}
	return sub
}

// fieldTable builds the param-name -> field-symbol map
// resolve.Config.CtorFields expects — labeled and named slots
// only; positional payload is addressed by index.
func fieldTable(fields []check.TField) map[string]*resolve.Symbol {
	var table map[string]*resolve.Symbol
	for _, f := range fields {
		name := f.Label
		if name == "" {
			name = f.Name
		}
		if name == "" || f.Sym == nil {
			continue
		}
		if table == nil {
			table = map[string]*resolve.Symbol{}
		}
		table[name] = f.Sym
	}
	return table
}

// letSyms collects the variable symbols a top-level let's pattern
// binds — the member names a `let` contributes.
func letSyms(p check.Pat) []*resolve.Symbol {
	var out []*resolve.Symbol
	var walk func(p check.Pat)
	walk = func(p check.Pat) {
		switch n := p.(type) {
		case nil:
		case *check.VarPat:
			out = append(out, n.Sym)
		case *check.TypePat:
			if n.Sym != nil {
				out = append(out, n.Sym)
			}
		case *check.LitPat, *check.WildPat:
			// Literal and wildcard patterns bind no symbols.
		case *check.CtorPat:
			for _, a := range n.Args {
				walk(a.Pat)
			}
			for i := range n.Fields {
				walk(n.Fields[i].Pat)
			}
		case *check.TuplePat:
			for _, e := range n.Elems {
				walk(e)
			}
		case *check.RecordPat:
			for i := range n.Fields {
				walk(n.Fields[i].Pat)
			}
		case *check.ListPat:
			for _, e := range n.Elems {
				walk(e)
			}
			walk(n.Rest)
		case *check.OrPat:
			if len(n.Alts) > 0 {
				walk(n.Alts[0])
			}
		case *check.AsPat:
			walk(n.Pat)
			out = append(out, n.Sym)
		default:
			panic("unreachable")
		}
	}
	walk(p)
	return out
}

// memberVars reports the rigid variables in t in first-occurrence
// order — the order check.instantiateSig and lower's CallDirect
// TypeArgs share with check's own rigidVars, and the order explicit
// type arguments (`p.empty @Int`) bind in.
func memberVars(t types.Type) []*types.Var {
	var out []*types.Var
	seen := map[int]bool{}
	var walk func(t types.Type)
	walk = func(t types.Type) {
		switch t := types.Chase(t).(type) {
		case *types.Var:
			if !seen[t.ID] {
				seen[t.ID] = true
				out = append(out, t)
			}
		case *types.Meta:
		case *types.Con:
			for _, a := range t.Args {
				walk(a)
			}
		case *types.Func:
			for _, prm := range t.Params {
				walk(prm.Type)
			}
			walk(t.Ret)
		case *types.Tuple:
			for _, e := range t.Elems {
				walk(e)
			}
		case *types.Go:
			for _, a := range t.Args {
				walk(a)
			}
		default:
			panic("unreachable")
		}
	}
	walk(t)
	return out
}

// patType reports the type one bound symbol of a let pattern
// carries — the let's declared or inferred member type.
func patType(p check.Pat, sym *resolve.Symbol) types.Type {
	var found types.Type
	var walk func(p check.Pat)
	walk = func(p check.Pat) {
		switch n := p.(type) {
		case nil:
		case *check.VarPat:
			if n.Sym == sym {
				found = n.Type()
			}
		case *check.TypePat:
			if n.Sym == sym {
				found = n.Type()
			}
		case *check.LitPat, *check.WildPat:
			// Literal and wildcard patterns bind no symbols.
		case *check.CtorPat:
			for _, a := range n.Args {
				walk(a.Pat)
			}
			for i := range n.Fields {
				walk(n.Fields[i].Pat)
			}
		case *check.TuplePat:
			for _, e := range n.Elems {
				walk(e)
			}
		case *check.RecordPat:
			for i := range n.Fields {
				walk(n.Fields[i].Pat)
			}
		case *check.ListPat:
			for _, e := range n.Elems {
				walk(e)
			}
			walk(n.Rest)
		case *check.OrPat:
			if len(n.Alts) > 0 {
				walk(n.Alts[0])
			}
		case *check.AsPat:
			walk(n.Pat)
			if n.Sym == sym {
				found = n.Type()
			}
		default:
			panic("unreachable")
		}
	}
	walk(p)
	return found
}

// requalify rewrites t so constructor references to the package's
// own type names carry the import path. Every other constructor —
// builtin-scope and other-package names — keeps its qualifier;
// Var/Meta are shared leaves.
func requalify(t types.Type, own map[string]bool, path string) types.Type {
	switch t := types.Chase(t).(type) {
	case *types.Var:
		return t
	case *types.Meta:
		return t
	case *types.Con:
		c := &types.Con{Qual: t.Qual, Name: t.Name}
		if c.Qual == "" && own[c.Name] {
			c.Qual = path
		}
		c.Args = requalifyAll(t.Args, own, path)
		return c
	case *types.Func:
		params := make([]types.FuncParam, len(t.Params))
		for i, fp := range t.Params {
			params[i] = types.FuncParam{Label: fp.Label,
				Type: requalify(fp.Type, own, path)}
		}
		return &types.Func{Params: params,
			Ret: requalify(t.Ret, own, path)}
	case *types.Tuple:
		return &types.Tuple{Elems: requalifyAll(t.Elems, own, path)}
	case *types.Go:
		return &types.Go{Ref: t.Ref, Args: requalifyAll(t.Args, own, path)}
	default:
		panic("unreachable")
	}
}

// requalifyAll maps requalify over a type slice.
func requalifyAll(ts []types.Type, own map[string]bool, path string) []types.Type {
	if len(ts) == 0 {
		return ts
	}
	out := make([]types.Type, len(ts))
	for i, t := range ts {
		out[i] = requalify(t, own, path)
	}
	return out
}

// --- the implementation-import cycle check (E0606) ---

// ensureImplScan runs the implementation-import cycle check once, on
// the first Gom load. The .gomi loading guard catches interface
// cycles, but a loaded package's .gom files may import back toward
// the package being compiled — a cycle the generated Go packages
// would only hit at `go build` ("import cycle not allowed"). The
// check seeds the graph at the entry directory's own .gom files and
// expands each reached package's .gom import clauses on demand; the
// first cycle found becomes the loader's verdict for every Gom load.
func (l *Loader) ensureImplScan() {
	if l.implScanned {
		return
	}
	l.implScanned = true
	entry, err := l.entryPath()
	if err != nil || entry == "" {
		// No module context — no Gom package can load anyway (dirOf
		// answers the same failure), so there is nothing to check.
		return
	}
	l.entry = entry
	visiting := map[string]bool{}
	var stack []string
	if chain := l.visitImpl(entry, visiting, &stack); chain != nil {
		l.implCycleErr = &resolve.ImportCycleError{Chain: chain}
		// The members of the loop are the distinct chain nodes —
		// only their imports close a cycle; loads outside the set
		// proceed normally even though a cycle exists elsewhere.
		l.implCycleSet = map[string]bool{}
		for _, u := range chain {
			l.implCycleSet[u] = true
		}
	}
}

// visitImpl is the DFS over .gom import clauses: reaching the entry
// package closes a mutual cycle, reaching an on-stack package closes
// a cycle internal to the dependency graph. Children are visited in
// sorted order so the reported chain is deterministic. A node that
// cannot reach the entry or its own ancestors is marked done — the
// graph is static (each package's .gom files are read once), so the
// verdict is final.
func (l *Loader) visitImpl(u string, visiting map[string]bool, stack *[]string) []string {
	if u == l.entry && len(*stack) > 0 {
		return append(append([]string{}, *stack...), u)
	}
	if visiting[u] {
		// The cycle is the stack suffix that starts at u.
		for i, s := range *stack {
			if s == u {
				return append(append([]string{}, (*stack)[i:]...), u)
			}
		}
		return append(append([]string{}, u), u)
	}
	if l.implDone[u] {
		return nil
	}
	visiting[u] = true
	*stack = append(*stack, u)
	edges := l.implImports(u)
	keys := make([]string, 0, len(edges))
	for q := range edges {
		keys = append(keys, q)
	}
	sort.Strings(keys)
	for _, q := range keys {
		if chain := l.visitImpl(q, visiting, stack); chain != nil {
			return chain
		}
	}
	*stack = (*stack)[:len(*stack)-1]
	delete(visiting, u)
	l.implDone[u] = true
	return nil
}

// implImports returns the Gom import paths declared by the .gom files
// of package u — the edges the generated Go package will carry. The
// entry package answers from Config.Dir; other packages resolve
// through dirOf, so a package outside the main module (or one with no
// .gom files) contributes no edges. Files that fail to parse still
// yield whatever import clauses the parser recovered.
func (l *Loader) implImports(u string) map[string]bool {
	if m, ok := l.implEdges[u]; ok {
		return m
	}
	m := map[string]bool{}
	l.implEdges[u] = m
	var dir string
	if u == l.entry {
		dir = l.cfg.Dir
	} else {
		d, err := l.dirOf(canonicalPath(u))
		if err != nil {
			return m
		}
		dir = d
	}
	ms, err := filepath.Glob(filepath.Join(dir, "*.gom"))
	if err != nil {
		return m
	}
	for _, f := range ms {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		ast, _ := syntax.Parse(src, f)
		if ast == nil {
			continue
		}
		for _, d := range ast.Decls {
			if imp, ok := d.(*syntax.ImportDecl); ok && !imp.Go {
				m[imp.Path] = true
			}
		}
	}
	return m
}

// EntryPath reports the Go import path the package compiled out of
// Config.Dir occupies — the value build.Options.SelfImportPath wants:
// references into it emit unqualified, so generated code can never
// self-import. It is "" when module resolution fails or Config.Dir
// sits outside the main module — the output then has no importable
// identity, and an import of a toolchain-known path (the embedded
// stdlib) is an ordinary import, not a self-reference.
func (l *Loader) EntryPath() string {
	p, err := l.entryPath()
	if err != nil {
		return ""
	}
	return p
}

// entryPath computes the entry package's import path: Config.Dir
// rendered module-relative — the package generated output for the
// current compile lands in. An empty result means Config.Dir sits
// outside the main module, where no Gom package can resolve.
func (l *Loader) entryPath() (string, error) {
	modPath, modDir, err := l.module()
	if err != nil {
		return "", err
	}
	dir, err := filepath.Abs(l.cfg.Dir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(modDir, dir)
	if err != nil {
		return "", err
	}
	switch {
	case rel == ".":
		return modPath, nil
	case rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)):
		return "", nil
	default:
		return modPath + "/" + filepath.ToSlash(rel), nil
	}
}

// NilableGoRef defers the optional check.NilableSource protocol to
// Config.Sigs — this loader mints no Go type handles.
func (l *Loader) NilableGoRef(ref types.NamedRef) bool {
	if ns, ok := l.cfg.Sigs.(check.NilableSource); ok {
		return ns.NilableGoRef(ref)
	}
	return false
}

// NilableUnderlying defers the same protocol — whether the named
// Go type's underlying can hold nil at all.
func (l *Loader) NilableUnderlying(ref types.NamedRef) bool {
	if ns, ok := l.cfg.Sigs.(check.NilableSource); ok {
		return ns.NilableUnderlying(ref)
	}
	return false
}

// GomMethodsOf reports the @method declarations an imported Gom
// record type carries — the check.GomMethodSource view of the
// method table recorded from the package's .gomi signatures.
func (l *Loader) GomMethodsOf(t *types.Con) ([]check.GomMethod, bool) {
	if t.Qual == "" {
		return nil, false
	}
	return l.methods[conKey{qual: t.Qual, name: t.Name}], true
}

// InterfaceMethods defers the same protocol to the Go oracle — the
// required method set of a Go interface for @method conformance.
func (l *Loader) InterfaceMethods(t types.Type) ([]check.InterfaceMethod, bool) {
	if ms, ok := l.cfg.Sigs.(check.InterfaceMethodSource); ok {
		return ms.InterfaceMethods(t)
	}
	return nil, false
}
