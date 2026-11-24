package methodres

import (
	"encoding"
	"io"
	"io/fs"
	"testing"
)

// The emitted Go methods satisfy the interfaces the Go side calls
// them through: `Result T GoError` unwraps to (T, error) at the
// method's boundary spelling (`[]byte`, `int`, `[]fs.DirEntry`,
// nil for a None Ok payload).
func TestMethodResultUnwrap(t *testing.T) {
	var w io.Writer = WriterOf(Sink(3))
	n, err := w.Write([]byte("xy"))
	if n != 3 || err != nil {
		t.Fatalf("Write = (%d, %v), want (3, nil)", n, err)
	}

	var u encoding.TextUnmarshaler = UnmarshalerOf(Bare(0))
	if err := u.UnmarshalText([]byte("s")); err != nil {
		t.Fatalf("UnmarshalText = %v, want nil", err)
	}

	var r fs.ReadDirFS = FsOf(Dir(true))
	f, err := r.Open("x")
	if f != nil || err != nil {
		t.Fatalf("Open = (%v, %v), want (nil, nil)", f, err)
	}
	if _, err := r.ReadDir("x"); err == nil {
		t.Fatal("ReadDir = nil error, want non-nil")
	}
}
