// Package bfieldwrite is the hand-written Go that option (b) —
// field write through `Ptr T` plus `Ptr.new ()` zero value — would
// emit for the Gom mock in mock.gom.txt.
package bfieldwrite

import (
	"net/http"
	"time"
)

func redirectPolicy(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

// NewClient is what `Ptr.new ()` + two field writes + returning the
// pointer lowers to: `new(T)` then `p.F = v` assignments.
func NewClient() *http.Client {
	c := new(http.Client)
	c.Timeout = 30 * time.Second
	c.CheckRedirect = redirectPolicy
	return c
}
