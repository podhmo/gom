package stdlib

import (
	"io/fs"
	"sort"
	"strings"
	"testing"

	gomstdlib "github.com/podhmo/gom/stdlib"
)

// TestEmbeddedInterfacesMatchPackages pins the synchronization
// between the two places that enumerate the Gom-written standard
// library: the //go:embed set compiled into gomstdlib.Gomis
// (stdlib/gomis.go) and this package's `packages` table — every
// embedded .gomi must back exactly one table row under the
// <pkg>/<pkg>.gomi convention, and every row must have its
// interface embedded. Drift means an Apply that skips a package or
// dead weight shipped in the binary.
func TestEmbeddedInterfacesMatchPackages(t *testing.T) {
	var embedded []string
	err := fs.WalkDir(gomstdlib.Gomis, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".gomi") {
			embedded = append(embedded, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk gomstdlib.Gomis: %v", err)
	}
	sort.Strings(embedded)

	var want []string
	for _, p := range packages {
		want = append(want, p.pkg+"/"+p.pkg+".gomi")
	}
	sort.Strings(want)

	if strings.Join(embedded, ",") != strings.Join(want, ",") {
		t.Fatalf("embedded .gomi files %v do not match packages table %v — "+
			"update stdlib/gomis.go's //go:embed list or internal/stdlib's packages table",
			embedded, want)
	}

	// The impPath convention is what generated code imports; a row
	// that breaks it makes codegen emit a path nothing provides.
	for _, p := range packages {
		if want := "github.com/podhmo/gom/stdlib/" + p.pkg; p.impPath != want {
			t.Errorf("packages[%q].impPath = %q, want %q", p.pkg, p.impPath, want)
		}
	}
}
