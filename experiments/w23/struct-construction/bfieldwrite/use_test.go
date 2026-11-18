package bfieldwrite

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// Same observable contract as the (a) package: 30s timeout and the
// caller-supplied redirect policy.
func TestNewClientMatchesHost(t *testing.T) {
	c := NewClient()
	if c.Timeout != 30*time.Second {
		t.Fatalf("Timeout = %v, want 30s", c.Timeout)
	}
	if c.CheckRedirect == nil {
		t.Fatal("CheckRedirect is nil")
	}
	req, err := http.NewRequest("GET", "http://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.CheckRedirect(req, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect = %v, want ErrUseLastResponse", err)
	}
}
