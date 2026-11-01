package syntax

import "github.com/podhmo/gom/internal/diag"

// Node is any node of the surface AST. Every node produced by the
// parser carries the source range it was written at (docs/design.md
// section 2: "every node carries diag.Span").
type Node interface {
	Span() diag.Span
}

// File is a parsed .gom source file — the root of the surface AST.
//
// Comments and blank lines are kept as file-level trivia so that
// gom fmt can share the same parser (docs/design.md section 3 and
// section 6). Trivia is not part of the tree: it is reattached by
// position when printing.
type File struct {
	Sp diag.Span // the whole file

	Package  *Ident      // name of the `package` clause; nil if absent
	Decls    []Decl      // top-level declarations in source order
	Comments []*Comment  // every comment in the file, in source order
	Blanks   []diag.Span // one span per blank line, in source order
}

// Span returns the whole-file span.
func (f *File) Span() diag.Span { return f.Sp }

// PackageName returns the declared package name, or "" when the
// package clause is missing.
func (f *File) PackageName() string {
	if f.Package == nil {
		return ""
	}
	return f.Package.Name
}

// CommentKind classifies the three comment markers of the surface
// syntax (docs/spec.md section 1.2).
type CommentKind int

const (
	// CommentLine is a `//` line comment.
	CommentLine CommentKind = iota + 1
	// CommentBlock is a `/* */` block comment.
	CommentBlock
	// CommentDoc is a `///` documentation comment.
	CommentDoc
)

// String returns a short name for the kind ("line", "block", "doc").
func (k CommentKind) String() string {
	switch k {
	case CommentLine:
		return "line"
	case CommentBlock:
		return "block"
	case CommentDoc:
		return "doc"
	default:
		return "CommentKind(?)"
	}
}

// Comment is a single comment, kept as trivia for gom fmt and for the
// doc comments that feed .gomi generation.
type Comment struct {
	Sp   diag.Span   // covers the marker through the end of the comment
	Kind CommentKind //
	Text string      // comment text following the marker (uninterpreted)
}

// Span returns the comment's source span.
func (c *Comment) Span() diag.Span { return c.Sp }

// Doc reports whether this is a `///` documentation comment.
func (c *Comment) Doc() bool { return c.Kind == CommentDoc }
