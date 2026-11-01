package diag

import (
	"fmt"
	"os"
	"path/filepath"
)

// Resolver resolves the file names carried by spans to LineIndex
// values so that byte-offset positions can be rendered as line and
// column at output time (docs/spec.md section 12). The compiler
// anchors diagnostics to byte offsets; only the output layer needs
// the source text.
//
// Implementations may build LineIndex values lazily and cache them;
// they are not required to be safe for concurrent use.
type Resolver interface {
	// LineIndex returns the index of file, or an error when the
	// resolver cannot supply its source.
	LineIndex(file string) (*LineIndex, error)
}

// ResolverFunc adapts a function into a Resolver.
type ResolverFunc func(file string) (*LineIndex, error)

// LineIndex calls f(file).
func (f ResolverFunc) LineIndex(file string) (*LineIndex, error) { return f(file) }

// ResolveSpan resolves both ends of sp through r — shorthand for
// fetching the file's LineIndex and calling its Resolve.
func ResolveSpan(r Resolver, sp Span) (SpanPos, error) {
	li, err := resolveIndex(r, sp.File)
	if err != nil {
		return SpanPos{}, err
	}
	return li.Resolve(sp), nil
}

// resolveIndex is r.LineIndex plus the nil-resolver guard shared by
// the emitters.
func resolveIndex(r Resolver, file string) (*LineIndex, error) {
	if r == nil {
		return nil, fmt.Errorf("diag: no resolver for %q", file)
	}
	return r.LineIndex(file)
}

// NewMapResolver returns a Resolver over in-memory sources keyed by
// the file names used in spans. LineIndex values are built lazily and
// cached; the map is not copied, so its sources must stay alive and
// unmodified for the resolver's lifetime. A span naming a file absent
// from srcs resolves to an error.
func NewMapResolver(srcs map[string][]byte) Resolver {
	return &mapResolver{srcs: srcs, indexes: map[string]*LineIndex{}}
}

type mapResolver struct {
	srcs    map[string][]byte
	indexes map[string]*LineIndex
}

func (m *mapResolver) LineIndex(file string) (*LineIndex, error) {
	if li, ok := m.indexes[file]; ok {
		return li, nil
	}
	src, ok := m.srcs[file]
	if !ok {
		return nil, fmt.Errorf("diag: no source registered for %q", file)
	}
	li := NewLineIndex(src)
	m.indexes[file] = li
	return li, nil
}

// NewDirResolver returns a Resolver that reads sources lazily from
// the file system: LineIndex("a/b.gom") reads
// filepath.Join(dir, "a/b.gom"). Hits and failures are both memoized.
// It is intended for tools that know file names but do not hold the
// sources in memory; the compiler proper should prefer
// NewMapResolver over the sources it already read.
func NewDirResolver(dir string) Resolver {
	return &dirResolver{dir: dir, indexes: map[string]*LineIndex{}, failed: map[string]error{}}
}

type dirResolver struct {
	dir     string
	indexes map[string]*LineIndex
	failed  map[string]error
}

func (r *dirResolver) LineIndex(file string) (*LineIndex, error) {
	if li, ok := r.indexes[file]; ok {
		return li, nil
	}
	if err, ok := r.failed[file]; ok {
		return nil, err
	}
	src, err := os.ReadFile(filepath.Join(r.dir, filepath.FromSlash(file)))
	if err != nil {
		r.failed[file] = err
		return nil, err
	}
	li := NewLineIndex(src)
	r.indexes[file] = li
	return li, nil
}
