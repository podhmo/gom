package cextern

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// Same observable contract as the (a)/(b) packages, verified against
// the compiler's real output (a.go) plus the hand-written impl.go.
func TestNewClientMatchesHost(t *testing.T) {
	c := NewClient(func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	})
	if c.Timeout != 30*time.Second {
		t.Fatalf("Timeout = %v, want 30s", c.Timeout)
	}
	req, err := http.NewRequest("GET", "http://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.CheckRedirect(req, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect = %v, want ErrUseLastResponse", err)
	}
	// The generated ClientTimeout drives the Gom-side caller:
	// Go.func2 adaptation + the extern body together.
	if ClientTimeout(struct{}{}) != 30*time.Second {
		t.Fatalf("ClientTimeout = %v, want 30s", ClientTimeout(struct{}{}))
	}
	// The generated Client's CheckRedirect is the Go.func2 adapter:
	// invoking it drives tmp13 -> RedirectPolicy -> error end to end.
	if err := Client(struct{}{}).CheckRedirect(req, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("generated CheckRedirect = %v, want ErrUseLastResponse", err)
	}
}
