// Package goside stands in for Go standard-library functions that a
// Gom program calls through `import go`. Every function has a
// (T, error) signature, the shape spec 8.3 maps to Result T GoError.
package goside

import (
	"errors"
	"strconv"

	"github.com/podhmo/gom/experiments/p0/result-repr/payload"
)

// ParseInt mirrors strconv.ParseInt.
func ParseInt(s string, base, bitSize int) (int64, error) {
	return strconv.ParseInt(s, base, bitSize)
}

// LoadBig fabricates a large successful result, like a file loader or
// decoder that returns a big record.
func LoadBig(fail bool) (payload.Big, error) {
	var b payload.Big
	if fail {
		return b, errors.New("load failed")
	}
	b.N = 42
	return b, nil
}
