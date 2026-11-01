// Package rt contains helpers imported by Go code generated from Gom
// sources: tuples, Task, assertion functions for tests, InvalidVariant
// panics for contract-violating ADT values, and similar runtime support.
//
// It lives outside internal/ because generated code must be able to
// import it (internal packages are not importable by other modules).
// The name "rt" avoids colliding with Go's standard runtime package.
package rt
