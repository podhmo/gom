package main

// Incremental-build-cache tests (design.md section 5): a package
// regenerates only when its own source or a dependency's .gomi
// changes — an implementation-only dependency edit must skip the
// consumer entirely. The multifile fixture (shapes library +
// gomapp consumer) gives the two-package shape; the "sentinel"
// trick below proves a skip: the consumer's generated file is
// hand-corrupted after the first build, and only a real regen
// would overwrite it.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupCacheFixture copies testdata/multifile into a temp module
// and points GOM_BUILD_CACHE_DIR at a fresh dir.
func setupCacheFixture(t *testing.T) (dir, shapesDir, gomappDir string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}
	dir = t.TempDir()
	copyFixture(t, filepath.Join(root, "testdata", "multifile"), dir)
	gomodPath := filepath.Join(dir, "go.mod")
	gomod, err := os.ReadFile(gomodPath)
	if err != nil {
		t.Fatal(err)
	}
	gomod = []byte(strings.ReplaceAll(string(gomod), "=> ../..", "=> "+root))
	if err := os.WriteFile(gomodPath, gomod, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(cacheDirEnv, filepath.Join(dir, ".gomcache"))
	t.Setenv(cacheOffEnv, "")
	return dir, filepath.Join(dir, "shapes"), filepath.Join(dir, "gomapp")
}

// sentinel corrupts the consumer's generated file: a subsequent
// cache hit leaves it, a regen restores generated code.
func sentinel(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("// sentinel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func isSentinel(t *testing.T, path string) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) == "// sentinel\n"
}

func TestBuildCache_SkipsOnIdenticalAndImplOnlyChange(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes the go tool")
	}
	_, shapesDir, gomappDir := setupCacheFixture(t)
	appGo := filepath.Join(gomappDir, "main.go")

	if code, out, errout := invoke("build", shapesDir, gomappDir); code != 0 {
		t.Fatalf("first build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}

	// Same-input rebuild: hit — main.go must not be touched even
	// when hand-corrupted (the sentinel is the observable proof a
	// compile+write did not run).
	sentinel(t, appGo)
	if code, out, errout := invoke("build", shapesDir, gomappDir); code != 0 {
		t.Fatalf("second build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	if !isSentinel(t, appGo) {
		t.Fatal("identical rebuild regenerated the consumer — cache missed")
	}

	// Implementation-only dependency edit: ops.gom's pi literal
	// changes generated code but leaves shapes.gomi untouched, so
	// gomapp's key still holds — sentinel stands.
	ops := filepath.Join(shapesDir, "ops.gom")
	b, err := os.ReadFile(ops)
	if err != nil {
		t.Fatal(err)
	}
	b = []byte(strings.ReplaceAll(string(b), "3.14159", "3.14"))
	if err := os.WriteFile(ops, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errout := invoke("build", shapesDir, gomappDir); code != 0 {
		t.Fatalf("impl-edit build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	if !isSentinel(t, appGo) {
		t.Fatal("implementation-only dep edit regenerated the consumer")
	}
}

func TestBuildCache_GomiChangeRegenerates(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes the go tool")
	}
	_, shapesDir, gomappDir := setupCacheFixture(t)
	appGo := filepath.Join(gomappDir, "main.go")

	if code, out, errout := invoke("build", shapesDir, gomappDir); code != 0 {
		t.Fatalf("first build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	sentinel(t, appGo)

	// A semantic .gomi change regenerates the consumer: reordering
	// member decls keeps conformance intact but changes the
	// interface hash, so the sentinel is overwritten by fresh
	// output.
	gomi := filepath.Join(shapesDir, "shapes.gomi")
	b, err := os.ReadFile(gomi)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	const tau = "\nlet tau : Float\n"
	i := strings.Index(src, tau)
	if i < 0 {
		t.Fatalf("fixture .gomi changed shape: %q", src)
	}
	moved := src[:i] + src[i+len(tau):] + tau
	if err := os.WriteFile(gomi, []byte(moved), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errout := invoke("build", shapesDir, gomappDir); code != 0 {
		t.Fatalf("gomi-change build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	if isSentinel(t, appGo) {
		t.Fatal(".gomi change did not regenerate the consumer")
	}
}

// manifestFor reads the committed manifest of one package dir.
func manifestFor(t *testing.T, cacheBase, dir string) bcManifest {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(abs))
	b, err := os.ReadFile(filepath.Join(cacheBase, hex.EncodeToString(sum[:])+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m bcManifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBuildCache_RecordsGoDepsAndModuleFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes the go tool")
	}
	dir, _, gomappDir := setupCacheFixture(t)

	if code, out, errout := invoke("build", gomappDir); code != 0 {
		t.Fatalf("build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	m := manifestFor(t, filepath.Join(dir, ".gomcache"), gomappDir)

	// gomapp imports go "fmt" — its source dir (and the dep dirs
	// `go list -export` reported) must be tracked so a Go API
	// change regenerates.
	if len(m.GoDirs) == 0 {
		t.Fatal("no Go dep dirs recorded for an `import go` consumer")
	}
	foundFmt := false
	for _, g := range m.GoDirs {
		if strings.HasSuffix(g.Path, string(filepath.Separator)+"fmt") {
			foundFmt = true
		}
	}
	if !foundFmt {
		t.Fatalf("fmt's dir missing from goDirs: %v", m.GoDirs)
	}
	// The fixture's go.mod steers resolution — its stat is the
	// tripwire for replace/require edits.
	foundMod := false
	for _, mod := range m.Mods {
		if mod.Path == filepath.Join(dir, "go.mod") {
			foundMod = true
		}
	}
	if !foundMod {
		t.Fatalf("go.mod missing from mods: %v", m.Mods)
	}
}

func TestBuildCache_GoModChangeRegenerates(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes the go tool")
	}
	dir, _, gomappDir := setupCacheFixture(t)
	appGo := filepath.Join(gomappDir, "main.go")

	if code, out, errout := invoke("build", gomappDir); code != 0 {
		t.Fatalf("first build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	sentinel(t, appGo)

	// A go.mod edit that changes nothing semantically still must
	// miss — the file's stat is the only signal a replace or
	// require changed.
	f, err := os.OpenFile(filepath.Join(dir, "go.mod"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n// resolution bump\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if code, out, errout := invoke("build", gomappDir); code != 0 {
		t.Fatalf("rebuild: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	if isSentinel(t, appGo) {
		t.Fatal("go.mod edit did not regenerate the package")
	}
}

func TestBuildCache_NocacheDisables(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes the go tool")
	}
	_, shapesDir, gomappDir := setupCacheFixture(t)
	t.Setenv(cacheOffEnv, "1")
	appGo := filepath.Join(gomappDir, "main.go")

	if code, out, errout := invoke("build", shapesDir, gomappDir); code != 0 {
		t.Fatalf("first build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	sentinel(t, appGo)
	if code, out, errout := invoke("build", shapesDir, gomappDir); code != 0 {
		t.Fatalf("second build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	if isSentinel(t, appGo) {
		t.Fatal("identical rebuild stayed cached under GOM_BUILD_NOCACHE")
	}
}

func TestBuildCache_MtimeUnchangedOnIdenticalOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes the go tool")
	}
	_, shapesDir, _ := setupCacheFixture(t)
	shapesGo := filepath.Join(shapesDir, "shapes.go")

	if code, out, errout := invoke("build", shapesDir); code != 0 {
		t.Fatalf("first build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	fi1, err := os.Stat(shapesGo)
	if err != nil {
		t.Fatal(err)
	}
	if code, out, errout := invoke("build", shapesDir); code != 0 {
		t.Fatalf("second build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	fi2, err := os.Stat(shapesGo)
	if err != nil {
		t.Fatal(err)
	}
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Fatalf("identical output rewrote the file: %v -> %v", fi1.ModTime(), fi2.ModTime())
	}
}
