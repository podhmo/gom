// Package resolve performs name resolution (phase 3 of the pipeline). It
// binds every identifier to a symbol: its scope, whether it is defined in
// Gom or Go, and whether it lives in a test-only scope.
//
// It also loads dependency information: .gomi interface files of Gom
// packages (implementations are never read) and Go package types via
// internal/goimport.
package resolve
