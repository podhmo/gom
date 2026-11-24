package methodres

import (
	"encoding"
	"io"
	"io/fs"
	"net/http"
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
	// Err path of an Option-payload method: the emitted unwrap must
	// return the error, not panic on the nil Option (the zero value
	// of gom's sealed Option interface).
	var r2 fs.ReadDirFS = FsOf(Dir(false))
	if _, err := r2.Open("x"); err == nil {
		t.Fatal("Open (err path) = nil error, want non-nil")
	}

	// http.Hijacker's (net.Conn, *bufio.ReadWriter, error) maps to
	// `Result (Option net.Conn, Ptr bufio.ReadWriter) GoError` — the
	// tuple spreads and Option elements unwrap to nil-able results.
	var hj http.Hijacker = HijackerOf(Hij(true))
	c, rw, err := hj.Hijack()
	if c != nil || rw != nil || err != nil {
		t.Fatalf("Hijack = (%v, %v, %v), want (nil, nil, nil)", c, rw, err)
	}
	var hj2 http.Hijacker = HijackerOf(Hij(false))
	if _, _, err := hj2.Hijack(); err == nil {
		t.Fatal("Hijack (err path) = nil error, want non-nil")
	}
}
