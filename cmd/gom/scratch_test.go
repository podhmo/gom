package main

import (
	"archive/zip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestModPathLiteralQuotesWhitespace(t *testing.T) {
	t.Parallel()

	got := modPathLiteral("/tmp/gom source")
	if got != "\"/tmp/gom source\"" {
		t.Fatalf("modPathLiteral = %q", got)
	}
}

func TestScratchModulePinsAndRemovesFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go mod download")
	}
	haveGo(t)
	dir := t.TempDir()
	cleanup, err := ensureScratchModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	firstTime := fi.ModTime()
	cleanup()
	for _, name := range []string{"go.mod", "go.sum"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s remained: %v", name, err)
		}
	}

	cleanup, err = ensureScratchModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	fi, err = os.Stat(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Equal(firstTime) {
		t.Fatalf("same scratch content changed mtime: first %v, second %v", firstTime, fi.ModTime())
	}
	if _, err := os.Stat(filepath.Join(dir, "go.work")); !os.IsNotExist(err) {
		t.Fatalf("scratch go.work exists: %v", err)
	}
}

func TestModuleFileQuotesReplacePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "gom source")
	if err := os.Symlink(mustRepoRoot(t), root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOM_TOOLCHAIN_ROOT", root)
	body, err := moduleFile(scratchModulePath)
	if err != nil {
		t.Fatal(err)
	}
	want := "=> " + strconv.Quote(root)
	if !strings.Contains(string(body), want) {
		t.Fatalf("replace path is not quoted as %q: %q", want, body)
	}
}

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDownloadToolchainStandaloneIgnoresWorkspaceReplace(t *testing.T) {
	haveGo(t)
	root := mustRepoRoot(t)
	ws := t.TempDir()
	project := filepath.Join(ws, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	gomod := "module workspace.example/project\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n"
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	gowork := "go 1.27\n\nuse ./project\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(ws, "go.work"), []byte(gowork), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", filepath.Join(ws, "go.work"))
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")

	if out, err := runDownloadToolchain(project, os.Environ()); err != nil {
		t.Fatalf("workspace resolution did not use the local replace: %v: %s", err, out)
	}
	err := downloadToolchainStandalone(project)
	if err == nil {
		t.Fatal("standalone resolution succeeded through the caller workspace")
	}
	if !strings.Contains(err.Error(), "GOWORK=off") || !strings.Contains(err.Error(), "explicit replace") {
		t.Fatalf("error is not actionable: %v", err)
	}
}

func TestDownloadToolchainStandaloneReleaseModuleWritesSum(t *testing.T) {
	haveGo(t)
	proxy := writeGomModuleProxy(t, "v0.0.1")
	t.Setenv("GOPROXY", (&url.URL{Scheme: "file", Path: proxy}).String())
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOWORK", "off")

	for _, tc := range []struct {
		name        string
		existingSum bool
	}{
		{name: "no existing sum"},
		{name: "existing sum", existingSum: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gomod := "module release.example/project\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.1\n"
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
				t.Fatal(err)
			}
			sumPath := filepath.Join(dir, "go.sum")
			if tc.existingSum {
				if err := os.WriteFile(sumPath, nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := downloadToolchainStandalone(dir); err != nil {
				t.Fatalf("standalone release resolution failed: %v", err)
			}
			sum, err := os.ReadFile(sumPath)
			if err != nil {
				t.Fatalf("go.sum was not written: %v", err)
			}
			if !strings.Contains(string(sum), "github.com/podhmo/gom v0.0.1") {
				t.Fatalf("go.sum lacks the release checksum: %q", sum)
			}
		})
	}
}

func writeGomModuleProxy(t *testing.T, version string) string {
	t.Helper()
	proxy := t.TempDir()
	vdir := filepath.Join(proxy, "github.com", "podhmo", "gom", "@v")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := []byte("module github.com/podhmo/gom\n\ngo 1.27\n")
	if err := os.WriteFile(filepath.Join(vdir, version+".mod"), mod, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vdir, version+".info"), []byte(`{"Version":"`+version+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vdir, "list"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	zf, err := os.Create(filepath.Join(vdir, version+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	entry, err := zw.Create("github.com/podhmo/gom@" + version + "/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(mod); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zf.Close(); err != nil {
		t.Fatal(err)
	}
	return proxy
}
