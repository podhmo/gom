package goimport

// Loader is the one-session Go package oracle behind `import go`
// (docs/decisions/2026-11-02-import-go-loader-seam.md and
// 2026-11-03-import-go-loading-mechanics.md). It answers every
// consumer of imported-Go metadata from a single go/types session —
// shared because *types.Named identity only survives inside one
// importer:
//
//   - resolve.Loader: Load returns the exported member table.
//   - check.SignatureSource: SignatureOf returns the Gom-view
//     signature of a member (check.Signature.Type stays the Gom
//     view, e.g. strconv.Atoi is `String -> Result Int GoError`).
//   - check.MethodSource: MethodOf resolves `x.M` on types.Go (and
//     Ptr-of-types.Go) receivers through go/types method sets.
//   - lower's Env.GoSig hook: GoSigOf returns the Go-view signature
//     facts — arity, variadicness, result shape, and the raw
//     parameter/result types (Params/ResTypes) where the Go spelling
//     differs from the Gom one (`int` vs `int64`).
//
// Symbols whose signatures cannot be represented in Gom still enter
// the member table with Symbol.Hidden set; the checker turns each
// use site into E0501 carrying the reason.
//
// Load(path, false) refuses: Gom packages arrive through the .gomi
// reader (P4-1), and a plain `import` must produce E0203 rather than
// silently falling back to Go loading.

import (
	"fmt"
	"go/constant"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	gt "github.com/podhmo/gom/internal/types"
)

// Config configures a Loader. The zero value loads packages by
// running `go list -export` in the process working directory with the
// `go` binary on PATH. Dir and GoBin are the documented escape from
// ambient toolchain and workspace skew (a GOWORK file, a different
// toolchain): point them at the module the .gom file belongs to.
type Config struct {
	// Dir is the module directory `go list` runs in ("" = cwd).
	Dir string
	// GoBin is the go binary used for `go list -export` ("" = "go").
	GoBin string
	// OnLoad, when set, is called for every package whose export
	// data the importer resolves — cache hit or fresh `go list` —
	// with the package's own source dir and the dirs of the
	// dependencies `go list -export` reported. The build cache
	// records them: a dep dir's file stats stand in for the
	// package's API surface.
	OnLoad func(srcDir string, depDirs []string)
}

// Loader is the Go import oracle; see the package comment.
type Loader struct {
	// mu serializes every public method — a Loader shared across
	// concurrent dir compiles (design.md section 5 item 6) hands
	// out symbols and mutates pkgs/syms/msyms/named/nextVar (and
	// the importer's fset) only under it.
	mu sync.Mutex

	fset *token.FileSet
	imp  types.Importer

	// gobin/dir/cache back the prefetch path: Prefetch warms every
	// upcoming `import go` miss with one `go list -export -deps`
	// subprocess instead of paying one per import clause.
	gobin string
	dir   string
	cache *exportCache

	pkgs map[string]*resolve.Package
	// syms indexes every member and method symbol the loader handed
	// out — the shared table SignatureOf, MethodOf and GoSigOf read.
	syms map[*resolve.Symbol]*member
	// msyms interns method symbols by "path.Recv.Method" so every
	// `x.M` site shares the callee.
	msyms map[string]*resolve.Symbol
	// named indexes the go/types.Named behind every types.Go ref the
	// mapping produced — MethodOf needs it to build method sets.
	named map[gt.NamedRef]*types.Named
	// mapping marks the named types whose underlying signature
	// namedType is currently translating to a FuncN — a recursive
	// `type F func() F` re-enters through its own result and must
	// answer the named handle instead of descending forever.
	mapping map[gt.NamedRef]bool
	// listNamed is the lazily built `list.List` named type
	// boundaryType spells inside identity positions.
	listNamed *types.Named

	// tps scopes a generic signature's type parameters while
	// mapSignature maps it: the go/types.TypeParam objects map to
	// the rigid variables minted for it. Nil outside a generic
	// signature — TypeParam occurrences then reject.
	tps map[types.Object]*gt.Var
	// nextVar numbers the rigid variables minted for Go type
	// parameters, offset by goTypeParamVarBase so they never
	// collide with a checker's declared or builtin variables (Var
	// identity is its ID — a collision would equate distinct
	// variables under types.Subst).
	nextVar int
}

// goTypeParamVarBase starts the ID range of the rigid variables
// standing for Go type parameters — kept clear of the checker's
// declared-variable range (0+) and its builtinVarBase.
const goTypeParamVarBase = 1 << 21

// member is the per-symbol Go metadata: the object itself plus the
// two signature views the checker and lowering consume.
type member struct {
	obj types.Object     // the loaded *types.Func/TypeName/Var/Const
	sig *types.Signature // for funcs and methods
	ck  check.Signature  // Gom view
	gs  lower.GoSig      // Go view
}

// New returns a Loader per cfg; a nil cfg means the zero-value
// configuration.
func New(cfg *Config) *Loader {
	c := Config{}
	if cfg != nil {
		c = *cfg
	}
	l := &Loader{
		fset:  token.NewFileSet(),
		pkgs:  map[string]*resolve.Package{},
		syms:  map[*resolve.Symbol]*member{},
		msyms: map[string]*resolve.Symbol{},
		named: map[gt.NamedRef]*types.Named{},
	}
	gobin := c.GoBin
	if gobin == "" {
		gobin = "go"
	}
	cache := newExportCache(gobin, c.Dir)
	l.gobin, l.dir, l.cache = gobin, c.Dir, cache
	l.imp = importer.ForCompiler(l.fset, "gc", func(path string) (io.ReadCloser, error) {
		if cache != nil {
			if e, ok := cache.get(path); ok {
				if c.OnLoad != nil {
					deps := make([]string, len(e.Deps))
					for i, d := range e.Deps {
						deps[i] = d.Dir
					}
					c.OnLoad(e.Dir, deps)
				}
				return os.Open(e.Export)
			}
		}
		export, dir, deps, err := goExport(gobin, c.Dir, path)
		if err != nil {
			return nil, err
		}
		if cache != nil {
			cache.put(path, export, dir, deps)
		}
		if c.OnLoad != nil {
			c.OnLoad(dir, deps)
		}
		return os.Open(export)
	})
	return l
}

// goExport resolves a package's compiled export data by asking the
// toolchain — `go list -export` compiles it into the build cache and
// reports the .a path, working uniformly for the standard library,
// module dependencies and the module's own packages. `-deps` makes
// the same subprocess also print each dependency's source directory,
// which the file cache records: a dependency's exported API is
// embedded in the importer's archive, so its edits must invalidate
// the entry even while the package's own source is unchanged
// (exportcache.go). Printing deps costs nothing — `-export` already
// builds them.

// Prefetch warms the export cache for the `import go` paths a
// package is about to load. Cache misses today resolve one `go
// list -export -deps` subprocess per import clause (~80ms each);
// `go list` accepts multiple package arguments, so the misses of
// a whole compile settle with one call. Entries written here are
// keyed by the canonical ImportPath the toolchain reports —
// relative arguments (`./foo`) canonicalize differently, so
// callers pass only absolute paths. A missing or unbuildable
// package aborts the batch and the lazy per-path path reports the
// real error during resolve.
func (l *Loader) Prefetch(paths []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cache == nil || len(paths) == 0 {
		return
	}
	var miss []string
	seen := map[string]bool{}
	for _, p := range paths {
		// Relative paths canonicalize differently in `go list`, and
		// a leading dash would be read as a flag of it — both stay
		// on the per-path lazy path. A package this loader already
		// imported needs no disk read at all.
		if seen[p] || strings.HasPrefix(p, ".") || strings.HasPrefix(p, "-") || l.pkgs[p] != nil {
			continue
		}
		seen[p] = true
		if _, ok := l.cache.get(p); !ok {
			miss = append(miss, p)
		}
	}
	if len(miss) == 0 {
		return
	}
	args := append([]string{"list", "-export", "-deps", "-f", "{{.ImportPath}}|{{.Export}}|{{.Dir}}"}, miss...)
	cmd := exec.Command(l.gobin, args...)
	if l.dir != "" {
		cmd.Dir = l.dir
	}
	out, err := cmd.Output()
	if err != nil {
		return // the lazy path surfaces the diagnostic
	}
	// The flat dep list cannot be attributed per package, so every
	// entry records the union: over-invalidation only — an edit
	// anywhere in the batch's dep set refetches all of them.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	byPath := map[string][2]string{}
	var depDirs []string
	seenDir := map[string]bool{}
	for _, line := range lines {
		ip, rest, _ := strings.Cut(line, "|")
		exp, d, _ := strings.Cut(rest, "|")
		if exp != "" {
			byPath[ip] = [2]string{exp, d}
		}
		if d != "" && !seenDir[d] {
			seenDir[d] = true
			depDirs = append(depDirs, d)
		}
	}
	for _, p := range miss {
		if e, ok := byPath[p]; ok {
			l.cache.put(p, e[0], e[1], depDirs)
		}
	}
}
func goExport(gobin, dir, path string) (export, srcDir string, depDirs []string, err error) {
	// A leading dash would make `go list` read the path as a flag —
	// no valid Go import path starts with one.
	if strings.HasPrefix(path, "-") {
		return "", "", nil, fmt.Errorf("go list -export %s: invalid import path", path)
	}
	cmd := exec.Command(gobin, "list", "-export", "-deps", "-f", "{{.ImportPath}}|{{.Export}}|{{.Dir}}", path)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", "", nil, fmt.Errorf("go list -export %s: %s", path, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", "", nil, fmt.Errorf("go list -export %s: %w", path, err)
	}
	// `-deps` lists dependencies before their importers, so the
	// named package is the last line — matching `.ImportPath ==
	// path` would miss a relative path (`go list` canonicalizes
	// "./foo" to the module path).
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	last := lines[len(lines)-1]
	if last != "" {
		_, rest, _ := strings.Cut(last, "|")
		export, srcDir, _ = strings.Cut(rest, "|")
	}
	seen := map[string]bool{srcDir: true}
	for _, line := range lines[:len(lines)-1] {
		_, rest, _ := strings.Cut(line, "|")
		_, d, _ := strings.Cut(rest, "|")
		if d != "" && !seen[d] {
			seen[d] = true
			depDirs = append(depDirs, d)
		}
	}
	if export == "" {
		return "", "", nil, fmt.Errorf("go list -export %s: no export data (is this a main package?)", path)
	}
	return export, srcDir, depDirs, nil
}

// Load returns the exported member table of the Go package at path —
// the resolve.Loader entry. goPkg distinguishes `import go "path"`
// from `import "path"`; a plain import is refused so the resolver
// reports E0203 rather than loading Go members under a Gom name.
func (l *Loader) Load(path string, goPkg bool) (*resolve.Package, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.load(path, goPkg)
}

func (l *Loader) load(path string, goPkg bool) (*resolve.Package, error) {
	if !goPkg {
		return nil, fmt.Errorf("package %q is not a Go package (use `import go`)", path)
	}
	if p, ok := l.pkgs[path]; ok {
		return p, nil
	}
	gp, err := l.imp.Import(path)
	if err != nil {
		return nil, err
	}
	p := &resolve.Package{Path: path, Members: map[string]*resolve.Symbol{}}
	scope := gp.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		sym, m := l.member(path, obj)
		p.Members[name] = sym
		l.syms[sym] = m
	}
	l.pkgs[path] = p
	return p, nil
}

// member builds the resolver symbol and the checker/lowering
// metadata for one exported package member. Members the type mapping
// cannot express carry sym.Hidden — they still bind so references
// produce E0501 with the reason instead of a bare "no such member".
func (l *Loader) member(path string, obj types.Object) (*resolve.Symbol, *member) {
	sym := &resolve.Symbol{Name: obj.Name(), IsGo: true, Package: path}
	m := &member{obj: obj}
	switch obj := obj.(type) {
	case *types.Func:
		sym.Kind = resolve.KindFunc
		sig, _ := obj.Type().(*types.Signature)
		m.sig = sig
		l.sigOf(sym, m, sig)
		if b, ok := stdBindOf(path, sym.Name); ok && sym.Hidden == "" {
			// A standard binding re-voices a package function
			// through the same admission an extern entry passes
			// — a drifted entry hides the member like any
			// unrepresentable one (methods share sigOf but are
			// never bound here).
			if reason := l.applyStdBind(sym, b, sig, &m.ck, &m.gs); reason != "" {
				sym.Hidden = reason
			}
		}
	case *types.TypeName:
		sym.Kind = resolve.KindType
		// A type name spells the named type itself — inside a
		// `List pkg.F` annotation or anywhere else the name means
		// `pkg.F`, and the boundary's FuncN reading is only a
		// signature-position view (mapType assignable=false).
		mt, reason := l.mapType(obj.Type(), false, false)
		if reason != "" {
			sym.Hidden = reason
			break
		}
		m.ck = check.Signature{Type: mt}
	case *types.Var, *types.Const:
		sym.Kind = resolve.KindVar
		mt, reason := l.gomType(obj.Type())
		if reason != "" {
			sym.Hidden = reason
			break
		}
		if reason := constFits(obj, mt); reason != "" {
			sym.Hidden = reason
			break
		}
		m.ck = check.Signature{Type: mt}
		// The Go view of the member's type — identical to the Gom
		// view except where the spellings diverge (`int` reports
		// Go.Int here, Int there). Lowering reads it through
		// GoSigOf to place the conversion a reference needs:
		// `os.O_RDONLY` (a typed `int` const) emits
		// `int64(os.O_RDONLY)` where Gom sees Int. A var/const
		// reference is a zero-arity, single-result "call", so the
		// signature's single ResTypes slot carries the raw type.
		rt, rreason := l.mapType(obj.Type(), true, true)
		if rreason != "" {
			rt = mt // the views diverge only where both map — defensive
		}
		if rreason == "" && !viewsCompatible(mt, rt) {
			sym.Hidden = divergentReason(obj.Type())
			break
		}
		m.gs = lower.GoSig{Results: core.GoResultsOne, ResTypes: []gt.Type{rt}}
	default:
		sym.Hidden = fmt.Sprintf("unsupported member kind %T", obj)
	}
	return sym, m
}

// constFits reports the Hidden reason for a Go constant whose value
// the Gom view cannot hold: an int-kind constant mapped to `Int`
// (`int64`) emits `int64(pkg.C)` at every reference, which fails to
// compile when the constant overflows int64 — `math.MaxUint` is
// `1<<64 - 1`. Constants of a named Go type emit bare references and
// are not checked here: their own type — which Go already validated
// — is what holds the value.
func constFits(obj types.Object, mt gt.Type) string {
	c, ok := obj.(*types.Const)
	if !ok || !gt.Equal(mt, gt.Int) {
		return ""
	}
	v := c.Val()
	if v.Kind() != constant.Int {
		return ""
	}
	if _, ok := constant.Int64Val(v); ok {
		return ""
	}
	return "value not representable as Int"
}

// sigOf fills m with the Gom-view and Go-view signatures of a Go
// function; failure marks the symbol Hidden.
func (l *Loader) sigOf(sym *resolve.Symbol, m *member, sig *types.Signature) {
	if sig == nil {
		sym.Hidden = "member has no Go signature"
		return
	}
	ck, gs, h := l.mapSignature(sig)
	if h.reason != "" {
		sym.Hidden = h.reason
		sym.HiddenCode = h.code
		return
	}
	m.ck = ck
	m.gs = gs
}

// SignatureOf returns the Gom-view signature of a member symbol —
// the check.SignatureSource entry.
func (l *Loader) SignatureOf(sym *resolve.Symbol) (check.Signature, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.signatureOf(sym)
}

func (l *Loader) signatureOf(sym *resolve.Symbol) (check.Signature, bool) {
	m, ok := l.syms[sym]
	if !ok || sym.Hidden != "" {
		return check.Signature{}, false
	}
	return m.ck, true
}

// GoSigOf returns the Go-view signature facts — the Env.GoSig hook
// lowering consults for arity, result shape and the raw parameter and
// result types. A package var or const answers too: it is a
// zero-arity, single-result "call" whose lone ResTypes entry is the
// member's raw Go type (lowering needs it to place the `int`/`Int`
// conversion — see member).
func (l *Loader) GoSigOf(sym *resolve.Symbol) (lower.GoSig, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.goSigOf(sym)
}

func (l *Loader) goSigOf(sym *resolve.Symbol) (lower.GoSig, bool) {
	m, ok := l.syms[sym]
	if !ok || sym.Hidden != "" {
		return lower.GoSig{}, false
	}
	if m.sig == nil {
		// A non-function member (a type name carries no signature
		// either) answers only when it recorded a raw result type.
		return m.gs, len(m.gs.ResTypes) == 1
	}
	return m.gs, true
}
