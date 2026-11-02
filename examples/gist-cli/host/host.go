// Package host provides the Go I/O primitives still missing from Gom's
// interop surface. It knows nothing about Gists or the CLI's application policy.
package host

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

func ArgumentCount() int        { return len(os.Args) - 1 }
func Argument(index int) string { return os.Args[index+1] }
func Stderr() io.Writer         { return os.Stderr }

func ReadText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("%s: file is not UTF-8 text", path)
	}
	return string(data), nil
}

func Quote(value string) string {
	data, _ := json.Marshal(value) // strings always have a JSON representation.
	return string(data)
}

func StringField(document, key string) (string, error) {
	var object map[string]json.RawMessage
	dec := json.NewDecoder(strings.NewReader(document))
	if err := dec.Decode(&object); err != nil {
		return "", err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return "", fmt.Errorf("response has trailing JSON data")
	}
	raw, ok := object[key]
	if !ok {
		return "", fmt.Errorf("response has no %s field", key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("response %s must be a string: %w", key, err)
	}
	if value == "" {
		return "", fmt.Errorf("response %s is empty", key)
	}
	return value, nil
}

func Reader(value string) io.Reader                   { return strings.NewReader(value) }
func RequestHeader(request *http.Request) http.Header { return request.Header }
func StatusCode(response *http.Response) int          { return response.StatusCode }

func NewClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func ReadResponse(response *http.Response) (string, error) {
	defer response.Body.Close()
	const limit = 2 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return "", err
	}
	if len(data) > limit {
		return "", fmt.Errorf("response exceeds %d bytes", limit)
	}
	return string(data), nil
}
