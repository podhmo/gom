package main

import (
	"os"

	"github.com/podhmo/gom/internal/diag"
)

// diskFallbackResolver resolves the compiler's in-memory inputs first
// and dependency files from disk after that. Imported .gomi files are
// not command inputs, but diagnostics may carry their declaration
// spans (for example E0407's candidate record labels).
type diskFallbackResolver struct {
	primary diag.Resolver
	indexes map[string]*diag.LineIndex
	failed  map[string]error
}

func newDiskFallbackResolver(primary diag.Resolver) *diskFallbackResolver {
	return &diskFallbackResolver{
		primary: primary,
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
	src, err := os.ReadFile(file)
	if err != nil {
		r.failed[file] = err
		return nil, err
	}
	li := diag.NewLineIndex(src)
	r.indexes[file] = li
	return li, nil
}
