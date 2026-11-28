package main

import (
	"os"
	"sync"

	"github.com/podhmo/gom/internal/diag"
)

// gomiSourceCache holds the exact dependency interface bytes one
// compile invocation observed. Rendering happens after compilation,
// so reading the file again could see different contents — or no
// file at all. The cache is invocation-scoped rather than process-
// global: `gom run` must be able to release it before waiting for a
// long-lived child program.
type gomiSourceCache struct {
	sources sync.Map // path string -> []byte
}

func newGomiSourceCache() *gomiSourceCache {
	return &gomiSourceCache{}
}

func (c *gomiSourceCache) note(path string, src []byte) {
	if c == nil {
		return
	}
	c.sources.Store(path, append([]byte(nil), src...))
}

func (c *gomiSourceCache) load(path string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	cached, ok := c.sources.Load(path)
	if !ok {
		return nil, false
	}
	return cached.([]byte), true
}

func (c *gomiSourceCache) clear() {
	if c != nil {
		c.sources.Clear()
	}
}

// diskFallbackResolver resolves the compiler's in-memory inputs first
// and dependency files from disk after that. Imported .gomi files are
// not command inputs, but diagnostics may carry their declaration
// spans (for example E0407's candidate record labels).
type diskFallbackResolver struct {
	primary diag.Resolver
	gomi    *gomiSourceCache
	indexes map[string]*diag.LineIndex
	failed  map[string]error
}

func newDiskFallbackResolver(primary diag.Resolver, gomi *gomiSourceCache) *diskFallbackResolver {
	return &diskFallbackResolver{
		primary: primary,
		gomi:    gomi,
		indexes: map[string]*diag.LineIndex{},
		failed:  map[string]error{},
	}
}

func (r *diskFallbackResolver) LineIndex(file string) (*diag.LineIndex, error) {
	if li, err := r.primary.LineIndex(file); err == nil {
		return li, nil
	}
	if li, ok := r.indexes[file]; ok {
		return li, nil
	}
	if err, ok := r.failed[file]; ok {
		return nil, err
	}
	var src []byte
	if cached, ok := r.gomi.load(file); ok {
		src = cached
	} else {
		var err error
		src, err = os.ReadFile(file)
		if err != nil {
			r.failed[file] = err
			return nil, err
		}
	}
	li := diag.NewLineIndex(src)
	r.indexes[file] = li
	return li, nil
}
