// Package eval is a tree-walking evaluator over the typed AST
// (internal/check). It exists for the semantics-comparison testing of
// docs/design.md section 7: small programs are evaluated directly from
// the typed AST and their results compared against running the
// generated Go code, so the two semantics must agree on evaluation
// order, pattern dispatch, partial application and error behavior.
//
// # Scope (P1)
//
// The evaluator is monomorphic and pure. Covered: literals (Int,
// Int32/Int64, Float, Bool, String, Char, Unit), tuples, immutable
// lists, records (literal and `{ base with ... }` update), ADT
// variants (construction, field access, matching), `if`, `match`
// (wildcard/variable/literal/constructor/tuple/record/list/or/as
// patterns and `if` guards), blocks, `let`, `ignore`, `try` (escape
// to the innermost function boundary), the sugared operators `|>`,
// `<|`, `|?>`, `>>`, `<<` (still Binary nodes at this stage),
// curried application via check.CallInfo (labeled arguments are
// already resolved to parameter positions — the evaluator trusts
// CallInfo.ArgToParam), partial application, over-application, and
// `for x in <list>` iteration over List values.
//
// Skipped (reported as an "unsupported" diagnostic, never a Go
// panic): `go` expressions, `select`, `for` over non-List iterables
// (channels), `import go` calls, `_` placeholders, spreads outside
// list literals, `Go.conv` ascriptions, Ref/Array/Ptr/Chan/Task
// values, and member selection of unbound module members. Go
// functions CAN be supplied by the harness as host functions bound in
// the environment (see NewHostFunc), which makes even CallGo nodes
// evaluable — including the cute `_` placeholder.
//
// # Errors
//
// Runtime failures are reported as diag.Diagnostic, never as Go
// panics, so comparison tests can assert on error behavior. The
// evaluator reserves the E90xx range (see errors.go): E9001
// unsupported construct, E9002 match failure, E9003 Gom panic (todo,
// assertion, invalid variant, division by zero), E9004 evaluator or
// compiler invariant violation, E9005 test failure. The codes are
// registered in internal/diag/codes.go under the E90xx "Evaluation"
// category with phase eval.
//
// # Contracts assumed of the typed AST
//
//   - check.Lit.Text is the source lexeme (e.g. `"a\n"`, `'c'`,
//     `0x1F`); the evaluator decodes Gom escapes. For LitStr/LitChar a
//     bare unquoted Text is also accepted, and escapes are decoded in
//     it all the same — so a bare Text must hold raw source text, not
//     decoded content (a `\n` in it becomes a newline).
//     check.StrText.Text is likewise treated as raw source text
//     (escapes are decoded by the evaluator). If internal/check later
//     normalizes Text to decoded content this assumption must be
//     revisited.
//   - Constructor identity is *resolve.Symbol pointer identity. As a
//     concession to hand-built ASTs, a variant VALUE whose Ctor is nil
//     (built without going through a TypeDecl or a ctor Var) falls
//     back to comparing its cached Name against the pattern's
//     Ctor.Name. Pattern and value sites should share the same
//     *Symbol whenever possible.
//   - Constructor and record-field metadata normally comes from
//     TypeDecls in the evaluated File. Constructors whose Var is
//     evaluated without a TypeDecl are derived lazily from the Var's
//     type (fields take Func parameter order/labels). Labeled
//     constructor pattern arguments require the constructor to be
//     known — either via a TypeDecl or a prior Var evaluation.
//   - CallInfo.ArgToParam indexes the callee's declared parameters
//     for CallDirect/CallPartial (nothing bound yet) and the
//     remaining-parameter list of the function type for CallUnknown;
//     evaluation keeps a "remaining parameters" view on each function
//     value so a single rule covers both.
//   - Function type equality allows labeled-parameter runs to reorder
//     (types.Equal), so the remaining-parameter list of a partially
//     applied function is a subset of the declared indices in
//     declaration order — matching what check emits.
//
// # Recursion
//
// Top-level FnDecls are bound into the global environment before any
// body can run, so self-recursion and mutual recursion between
// top-level functions work. `let` bindings (top-level and local) are
// sequential and non-recursive — the right-hand side is evaluated
// before the pattern is bound.
package eval
