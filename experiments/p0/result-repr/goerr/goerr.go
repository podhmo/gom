// Package goerr explores what the type argument E should be at the Go
// boundary when Gom code says Result T GoError (spec 8.3 maps
// "(T, error) -> Result T GoError", and spec 3.1 lists GoError as a
// nominal builtin — "Go の error").
//
// Three options:
//
//	(a) instantiate E with the builtin: Result[T, error]
//	(b) declare an alias: type GoError = error — identical type, name only
//	(c) declare a named interface: type GoError interface{ error }
//
// (c) is what "nominal" most literally means, but it makes
// Result[T, GoError] a different type from Result[T, error] (generic
// instantiation is invariant), so generated packages would not
// interoperate with hand-written Go helpers that use error. The tests
// pin the assignability rules.
package goerr

import (
	"github.com/podhmo/gom/experiments/p0/result-repr/sealed"
)

// Option (b): an alias. Result[T, GoErrorA] IS Result[T, error].
type GoErrorA = error

// Option (c): a named interface with the same method set. Values
// convert implicitly both ways (the method sets are identical), but
// types instantiated with it are distinct.
type GoErrorI interface{ error }

// Compiled-assertion examples for the invariant-instantiation wrinkle
// of option (c):

// aliasInstantiated compiles because GoErrorA is error.
var aliasInstantiated sealed.Result[int, GoErrorA] = sealed.Result_Ok[int, error]{V0: 1}

// ifaceInstantiated is a DIFFERENT type from Result[int, error] —
// isResult() (int, GoErrorI) != isResult() (int, error), so
// sealed.Result[int, GoErrorI] does not satisfy sealed.Result[int,
// error]. A compile error, not a silent miscompile — the fixed marker
// turns the P0-1 phantom-param hazard into ordinary type checking.
var ifaceInstantiated sealed.Result[int, GoErrorI] = sealed.Result_Ok[int, GoErrorI]{V0: 1}
