package main

import (
	"github.com/podhmo/gom/experiments/defer/scope"
	"github.com/podhmo/gom/stdlib/result"
)

func withScope[A any](body func(*scope.Scope) A) A { return scope.Run(body) }

func withScopeResult[A any](body func(*scope.Scope) result.Result[A, error]) result.Result[A, error] {
	return scope.RunResult(body)
}
