package blankemb_test

// The explicit `import go _ "embed"` path: the generated file must
// carry exactly one blank embed import and the var must be filled.

import (
	"testing"

	"example.com/gocall/blankemb"
)

func TestBlankEmbedImport(t *testing.T) {
	if got := blankemb.NoteText(struct{}{}); got != "secret note via blank import\n" {
		t.Errorf("NoteText() = %q, want the embedded note.txt contents", got)
	}
}
