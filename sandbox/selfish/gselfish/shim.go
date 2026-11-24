package main

// Handwritten Go backing the @extern declarations in host.gom.
// fakeTransport is verbatim internal/fake_transport.go.

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"time"

	"github.com/google/go-github/v68/github"
	"github.com/pkg/errors"
)

type fakeTransport struct {
	W io.Writer
}

func (t *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	b, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		return nil, err
	}

	io.WriteString(t.W, "\nrequest:\n")
	io.WriteString(t.W, "----------------------------------------\n")
	t.W.Write(b)
	io.WriteString(t.W, "\n")

	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
	}, nil
}

func fakeHttpClient(_ struct{}) *http.Client {
	return &http.Client{
		Timeout:   time.Duration(1) * time.Second,
		Transport: &fakeTransport{W: os.Stderr},
	}
}

// errors.Wrap(err, msg) — gom can't emit %w (see host.gom).
func wrap(e error, msg string) error { return fmt.Errorf("%s: %w", msg, e) }

// errors.Wrapf(err, format, arg) — one format arg is all this port needs.
func wrapf(e error, format string, arg any) error { return fmt.Errorf(format+": %w", arg, e) }

// pkg/errors.Errorf — the oracle's errors carry a stack trace, which
// log.Fatalf("!!%+v") then prints. Paths/line numbers follow the build.
func stackErrorf(format string, arg any) error { return errors.Errorf(format, arg) }

// test helper for the nil-gist guard (gom can't spell nil).
func nilGist(_ struct{}) *github.Gist { return nil }

// github.GistFile.Size is *int; gom's Ptr Int is *int64, so the
// literal is built here.
func hostNewGistFile(filename string, size int64, content string) *github.GistFile {
	n := int(size)
	return &github.GistFile{Size: &n, Filename: &filename, Content: &content}
}

// internal.ClientType's flag behavior: flagstruct picks the
// TextUnmarshaler path, so usage shows `--client` with no `string`
// placeholder, the default renders unquoted, and Parse itself
// validates the value. Gom can't declare methods, so they live here
// on the generated `type ClientType string`.
func (v ClientType) validate() error {
	switch v {
	case "github", "fake":
		return nil
	default:
		return fmt.Errorf("%v is an invalid value for internal.ClientType", v)
	}
}

func (v *ClientType) UnmarshalText(data []byte) error {
	*v = ClientType(data)
	return v.validate()
}

func (v ClientType) MarshalText() ([]byte, error) {
	return []byte(v), nil
}
