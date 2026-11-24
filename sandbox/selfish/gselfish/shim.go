package main

// Handwritten Go backing the @extern declarations in host.gom.
// fakeTransport is verbatim internal/fake_transport.go.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"time"

	"github.com/google/go-github/v68/github"
	"github.com/podhmo/gom/stdlib/result"
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

// github.GistsService.{Create,Edit} return 3 values, which gom can't
// express (E0501); these wrappers drop the *Response like the oracle
// does. Gom `Result T GoError` emits as result.Result[T, error].
func gistsCreate(c *github.Client, ctx context.Context, g *github.Gist) result.Result[*github.Gist, error] {
	gist, _, err := c.Gists.Create(ctx, g)
	if err != nil {
		return result.Err[*github.Gist, error](err)
	}
	return result.Ok[*github.Gist, error](gist)
}

func gistsEdit(c *github.Client, ctx context.Context, gistID string, g *github.Gist) result.Result[*github.Gist, error] {
	gist, _, err := c.Gists.Edit(ctx, gistID, g)
	if err != nil {
		return result.Err[*github.Gist, error](err)
	}
	return result.Ok[*github.Gist, error](gist)
}

// errors.Wrap(err, msg) — gom can't emit %w (see host.gom).
func wrap(e error, msg string) error { return fmt.Errorf("%s: %w", msg, e) }

// errors.Wrapf(err, format, arg) — one format arg is all this port needs.
func wrapf(e error, format string, arg any) error { return fmt.Errorf(format+": %w", arg, e) }

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
