package main

import (
	"archive/zip"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func isolatedModCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				_ = os.Chmod(path, 0o700)
			} else {
				_ = os.Chmod(path, 0o600)
			}
			return nil
		})
	})
	return dir
}

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
	t.Setenv("GOMODCACHE", isolatedModCache(t))

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
	t.Setenv("GOMODCACHE", isolatedModCache(t))

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
	return writeGomModuleProxyVersions(t, map[string]bool{version: true})
}

func writeGomModuleProxyVersions(t *testing.T, versions map[string]bool) string {
	t.Helper()
	proxy := t.TempDir()
	vdir := filepath.Join(proxy, "github.com", "podhmo", "gom", "@v")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := []byte("module github.com/podhmo/gom\n\ngo 1.27\n")
	var list strings.Builder
	for version, withResult := range versions {
		list.WriteString(version + "\n")
		if err := os.WriteFile(filepath.Join(vdir, version+".mod"), mod, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vdir, version+".info"), []byte(`{"Version":"`+version+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		zf, err := os.Create(filepath.Join(vdir, version+".zip"))
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(zf)
		writeProxyZipEntry(t, zw, "github.com/podhmo/gom@"+version+"/go.mod", mod)
		writeProxyZipEntry(t, zw, "github.com/podhmo/gom@"+version+"/rt/doc.go", []byte("package rt\n"))
		writeProxyZipEntry(t, zw, "github.com/podhmo/gom@"+version+"/stdlib/doc.go", []byte("package stdlib\n"))
		if withResult {
			writeProxyZipEntry(t, zw, "github.com/podhmo/gom@"+version+"/stdlib/result/result.go", []byte(`package result

type Result[A, E any] struct {
	Value A
	Error E
}

func FromGo[A, E any](v A, err E) Result[A, E] {
	return Result[A, E]{Value: v, Error: err}
}
`))
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := zf.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(vdir, "list"), []byte(list.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return proxy
}

func TestDownloadToolchainStandaloneDoesNotUpgradePinnedRelease(t *testing.T) {
	haveGo(t)
	proxy := writeGomModuleProxyVersions(t, map[string]bool{
		"v0.0.1": false,
		"v0.0.2": true,
	})
	dir := t.TempDir()
	gomod := []byte("module pinned.example/project\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.1\n")
	gomodPath := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(gomodPath, gomod, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPROXY", (&url.URL{Scheme: "file", Path: proxy}).String())
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOMODCACHE", isolatedModCache(t))

	err := downloadToolchainStandalone(dir)
	if err == nil {
		t.Fatal("standalone resolution accepted a pinned release without stdlib/result")
	}
	if !strings.Contains(err.Error(), "stdlib/result") {
		t.Fatalf("failure did not come from the missing stdlib/result package: %v", err)
	}
	after, readErr := os.ReadFile(gomodPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(gomod) {
		t.Fatalf("go.mod was rewritten during failed validation:\nbefore:\n%s\nafter:\n%s", gomod, after)
	}
}

func writeProxyZipEntry(t *testing.T, zw *zip.Writer, name string, body []byte) {
	t.Helper()
	entry, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(body); err != nil {
		t.Fatal(err)
	}
}

func TestInitNewModuleValidationFailureRollsBackCreatedFiles(t *testing.T) {
	haveGo(t)
	proxy := writeGomModuleProxyVersions(t, map[string]bool{"v0.0.1": false})
	t.Setenv("GOPROXY", (&url.URL{Scheme: "file", Path: proxy}).String())
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOMODCACHE", isolatedModCache(t))
	requirement := func() (string, string, error) {
		return "github.com/podhmo/gom v0.0.1", "", nil
	}
	run := func(t *testing.T, dir string) (string, string, int) {
		t.Helper()
		var out, errout strings.Builder
		code := runInitWithRequirement([]string{"-dir", dir, "example.com/hello"}, &out, &errout, requirement)
		return out.String(), errout.String(), code
	}

	t.Run("without existing files", func(t *testing.T) {
		dir := t.TempDir()
		out, errout, code := run(t, dir)
		if code != 2 {
			t.Fatalf("exit = %d, want 2; stdout=%q stderr=%q", code, out, errout)
		}
		if !strings.Contains(errout, "stdlib/result") {
			t.Fatalf("init did not fail during toolchain validation: %q", errout)
		}
		for _, name := range []string{"go.mod", "go.sum", "main.gom"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Fatalf("%s exists after failed init: %v", name, err)
			}
		}
	})

	t.Run("keeps an existing main.gom", func(t *testing.T) {
		dir := t.TempDir()
		sentinel := []byte("package main\n\nfn main () -> Unit = ()\n")
		mainPath := filepath.Join(dir, "main.gom")
		if err := os.WriteFile(mainPath, sentinel, 0o644); err != nil {
			t.Fatal(err)
		}
		out, errout, code := run(t, dir)
		if code != 2 {
			t.Fatalf("exit = %d, want 2; stdout=%q stderr=%q", code, out, errout)
		}
		after, err := os.ReadFile(mainPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(sentinel) {
			t.Fatalf("existing main.gom changed:\nbefore:\n%s\nafter:\n%s", sentinel, after)
		}
		for _, name := range []string{"go.mod", "go.sum"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Fatalf("%s exists after failed init: %v", name, err)
			}
		}
	})
}

func TestInitMainWriteFailureRollsBackAllInitChanges(t *testing.T) {
	haveGo(t)
	t.Setenv("GOWORK", "off")
	root := mustRepoRoot(t)
	requirement := func() (string, string, error) {
		return "github.com/podhmo/gom v0.0.0", "github.com/podhmo/gom => " + strconv.Quote(root), nil
	}
	writeMainFailure := func(path string, body []byte) error {
		if filepath.Base(path) == "main.gom" {
			return errors.New("injected main.gom write failure")
		}
		return writeNewFile(path, body)
	}

	t.Run("new module", func(t *testing.T) {
		dir := t.TempDir()
		var out, errout strings.Builder
		code := runInitWithWriter([]string{"-dir", dir, "example.com/hello"}, &out, &errout, requirement, writeMainFailure)
		if code != 2 {
			t.Fatalf("exit = %d, want 2; stdout=%q stderr=%q", code, out.String(), errout.String())
		}
		if !strings.Contains(errout.String(), "injected main.gom write failure") {
			t.Fatalf("init did not report the write failure: %q", errout.String())
		}
		for _, name := range []string{"go.mod", "go.sum", "main.gom"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Fatalf("%s exists after failed init: %v", name, err)
			}
		}
	})

	t.Run("existing module", func(t *testing.T) {
		proxy := writeGomModuleProxy(t, "v0.0.1")
		t.Setenv("GOPROXY", (&url.URL{Scheme: "file", Path: proxy}).String())
		t.Setenv("GOSUMDB", "off")
		t.Setenv("GOMODCACHE", isolatedModCache(t))
		releaseRequirement := func() (string, string, error) {
			return "github.com/podhmo/gom v0.0.1", "", nil
		}
		dir := t.TempDir()
		original := []byte("module existing.example/project\n\ngo 1.27\n")
		gomodPath := filepath.Join(dir, "go.mod")
		if err := os.WriteFile(gomodPath, original, 0o644); err != nil {
			t.Fatal(err)
		}
		sumPath := filepath.Join(dir, "go.sum")
		if err := os.WriteFile(sumPath, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		var out, errout strings.Builder
		code := runInitWithWriter([]string{"-dir", dir}, &out, &errout, releaseRequirement, writeMainFailure)
		if code != 2 {
			t.Fatalf("exit = %d, want 2; stdout=%q stderr=%q", code, out.String(), errout.String())
		}
		if !strings.Contains(errout.String(), "injected main.gom write failure") {
			t.Fatalf("init did not reach the injected write failure: %q", errout.String())
		}
		after, err := os.ReadFile(gomodPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(original) {
			t.Fatalf("failed init left go.mod modified:\nbefore:\n%s\nafter:\n%s", original, after)
		}
		sum, err := os.ReadFile(sumPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(sum) != 0 {
			t.Fatalf("failed init left go.sum modified: %q", sum)
		}
		if _, err := os.Stat(filepath.Join(dir, "main.gom")); !os.IsNotExist(err) {
			t.Fatalf("main.gom exists after failed init: %v", err)
		}
	})

	t.Run("externally created main.gom is not removed", func(t *testing.T) {
		dir := t.TempDir()
		sentinel := []byte("package main\n\nfn main () -> Unit = ()\n")
		writeExternalMain := func(path string, body []byte) error {
			if filepath.Base(path) == "main.gom" {
				if err := os.WriteFile(path, sentinel, 0o644); err != nil {
					return err
				}
				return errors.New("main.gom appeared before init could create it")
			}
			return writeNewFile(path, body)
		}
		var out, errout strings.Builder
		code := runInitWithWriter([]string{"-dir", dir, "example.com/hello"}, &out, &errout, requirement, writeExternalMain)
		if code != 2 {
			t.Fatalf("exit = %d, want 2; stdout=%q stderr=%q", code, out.String(), errout.String())
		}
		after, err := os.ReadFile(filepath.Join(dir, "main.gom"))
		if err != nil {
			t.Fatalf("rollback removed an externally created main.gom: %v", err)
		}
		if string(after) != string(sentinel) {
			t.Fatalf("externally created main.gom changed:\nbefore:\n%s\nafter:\n%s", sentinel, after)
		}
	})
}

func TestInitFailureKeepsBrokenGoModSymlink(t *testing.T) {
	dir := t.TempDir()
	gomodPath := filepath.Join(dir, "go.mod")
	if err := os.Symlink("missing-go-mod-target", gomodPath); err != nil {
		t.Fatal(err)
	}

	code, _, errout := invoke("init", "-dir", dir)
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%q", code, errout)
	}
	if _, err := os.Lstat(gomodPath); err != nil {
		t.Fatalf("failed init removed the pre-existing go.mod symlink: %v", err)
	}
}

func TestInitFailureKeepsBrokenMainSymlink(t *testing.T) {
	haveGo(t)
	t.Setenv("GOWORK", "off")
	root := mustRepoRoot(t)
	requirement := func() (string, string, error) {
		return "github.com/podhmo/gom v0.0.0", "github.com/podhmo/gom => " + strconv.Quote(root), nil
	}
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.gom")
	if err := os.Symlink("missing-main-gom-target", mainPath); err != nil {
		t.Fatal(err)
	}

	var out, errout strings.Builder
	code := runInitWithRequirement([]string{"-dir", dir, "example.com/hello"}, &out, &errout, requirement)
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stdout=%q stderr=%q", code, out.String(), errout.String())
	}
	if !strings.Contains(errout.String(), "main.gom") {
		t.Fatalf("init did not reach the main.gom write failure: %q", errout.String())
	}
	if _, err := os.Lstat(mainPath); err != nil {
		t.Fatalf("failed init removed the pre-existing main.gom symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); !os.IsNotExist(err) {
		t.Fatalf("go.mod exists after failed init: %v", err)
	}
}

func TestInitExistingOlderGoModuleRollsBackDependency(t *testing.T) {
	haveGo(t)
	dir := t.TempDir()
	original := []byte("module existing.example/project\n\ngo 1.26\n")
	gomodPath := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(gomodPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOM_TOOLCHAIN_ROOT", mustRepoRoot(t))

	code, _, errout := invoke("init", "-dir", dir)
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%q", code, errout)
	}
	if !strings.Contains(errout, "go list") || !strings.Contains(errout, "stdlib/result") {
		t.Fatalf("init failed before standalone toolchain validation: %q", errout)
	}
	after, err := os.ReadFile(gomodPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("failed init left go.mod modified:\nbefore:\n%s\nafter:\n%s", original, after)
	}
	for _, name := range []string{"main.gom", "go.sum"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s exists after rolled-back init: %v", name, err)
		}
	}
}

func TestDownloadToolchainStandaloneRejectsMissingLocalReplace(t *testing.T) {
	haveGo(t)
	dir := t.TempDir()
	gomod := "module replace.example/project\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => ./missing\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOMODCACHE", isolatedModCache(t))

	err := downloadToolchainStandalone(dir)
	if err == nil {
		t.Fatal("standalone resolution accepted a missing local replace")
	}
	if !strings.Contains(err.Error(), "replacement directory") || !strings.Contains(err.Error(), "explicit replace") {
		t.Fatalf("error does not explain the invalid replace: %v", err)
	}
}

func TestInitReleaseProjectBuildsOutsideWorkspaceReadonly(t *testing.T) {
	if testing.Short() {
		t.Skip("runs gom init, gom run, and go build")
	}
	haveGo(t)
	root := mustRepoRoot(t)
	proxy := writeGomModuleProxy(t, "v0.0.1")
	ws := t.TempDir()
	project := filepath.Join(ws, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	gowork := "go 1.27\n\nuse ./project\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(ws, "go.work"), []byte(gowork), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", filepath.Join(ws, "go.work"))
	t.Setenv("GOPROXY", (&url.URL{Scheme: "file", Path: proxy}).String())
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOMODCACHE", isolatedModCache(t))
	t.Setenv("GOFLAGS", "-mod=mod")

	requirement := func() (string, string, error) {
		return "github.com/podhmo/gom v0.0.1", "", nil
	}
	var initOut, initErr strings.Builder
	code := runInitWithRequirement([]string{"-dir", project, "example.com/hello"}, &initOut, &initErr, requirement)
	if code != 0 {
		t.Fatalf("gom init exit = %d; stdout=%q stderr=%q", code, initOut.String(), initErr.String())
	}
	sum, err := os.ReadFile(filepath.Join(project, "go.sum"))
	if err != nil {
		t.Fatalf("go.sum was not written: %v", err)
	}
	if !strings.Contains(string(sum), "github.com/podhmo/gom v0.0.1") {
		t.Fatalf("go.sum lacks the release checksum: %q", sum)
	}

	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	code, out, errout := invoke("run", filepath.Join(project, "main.gom"))
	if code != 0 {
		t.Fatalf("gom run outside the workspace exit = %d; stdout=%q stderr=%q", code, out, errout)
	}
	if out != "hello\n" {
		t.Fatalf("stdout = %q", out)
	}
}
