package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const scratchModulePath = "gom.local/scratch"

// toolchainRequirement renders the require/replace pair a generated
// module needs so generated imports of github.com/podhmo/gom/rt and
// stdlib resolve. Release builds use their own module version. A local
// source checkout is used for development binaries; GOM_TOOLCHAIN_ROOT
// is the explicit override for test and vendor setups.
func toolchainRequirement() (requirement, replace string, err error) {
	if root := os.Getenv("GOM_TOOLCHAIN_ROOT"); root != "" {
		abs, e := filepath.Abs(root)
		if e != nil {
			return "", "", e
		}
		if !isGomSourceRoot(abs) {
			return "", "", fmt.Errorf("GOM_TOOLCHAIN_ROOT %s is not a github.com/podhmo/gom checkout", abs)
		}
		return "github.com/podhmo/gom v0.0.0", "github.com/podhmo/gom => " + modPathLiteral(abs), nil
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		if strings.HasPrefix(bi.Main.Path, "github.com/podhmo/gom") {
			return "github.com/podhmo/gom " + bi.Main.Version, "", nil
		}
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		for d := filepath.Dir(file); ; d = filepath.Dir(d) {
			if isGomSourceRoot(d) {
				return "github.com/podhmo/gom v0.0.0", "github.com/podhmo/gom => " + modPathLiteral(d), nil
			}
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
		}
	}
	return "", "", fmt.Errorf("cannot determine the github.com/podhmo/gom module version used by this gom binary")
}

func modPathLiteral(path string) string {
	if strings.ContainsAny(path, " \t\n\r\v\f") {
		return strconv.Quote(path)
	}
	return path
}

var scratchFileTime = time.Unix(0, 0)

func pinFileTime(path string) {
	_ = os.Chtimes(path, scratchFileTime, scratchFileTime)
}

// downloadToolchain resolves the release-module requirement once so the
// scratch module carries the checksums the read-only Go build mode needs.
// Local replaces do not need a sum, but running the same command keeps the
// two paths on one contract and surfaces toolchain-resolution errors before
// compilation rather than at the final link step.
func downloadToolchain(dir string) error {
	cmd := exec.Command("go", "mod", "download", "github.com/podhmo/gom")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go mod download github.com/podhmo/gom: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

func isGomSourceRoot(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		line, _, _ = strings.Cut(line, "//")
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "module" {
			return f[1] == "github.com/podhmo/gom"
		}
	}
	return false
}

func moduleFile(modulePath string) ([]byte, error) {
	req, rep, err := toolchainRequirement()
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\ngo 1.27\n\nrequire %s\n", modulePath, req)
	if rep != "" {
		fmt.Fprintf(&b, "\nreplace %s\n", rep)
	}
	return []byte(b.String()), nil
}

// ensureScratchModule installs the invocation-scoped module file used by
// `gom run` outside a real module. The file is created exclusively, never
// replacing an existing go.mod, and the returned cleanup removes only the
// file this function created.
func ensureScratchModule(dir string) (func(), error) {
	if _, _, ok := moduleOf(dir); ok {
		return func() {}, nil
	}
	body, err := moduleFile(scratchModulePath)
	if err != nil {
		return nil, err
	}
	created := []string{}
	cleanup := func() {
		for i := len(created) - 1; i >= 0; i-- {
			_ = os.Remove(created[i])
		}
	}
	writeScratch := func(name string, body []byte) error {
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("create scratch %s: %w", path, err)
		}
		if _, err := f.Write(body); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(path)
			return err
		}
		pinFileTime(path)
		created = append(created, path)
		return nil
	}
	if err := writeScratch("go.mod", body); err != nil {
		return nil, err
	}
	// A parent go.work otherwise wins over the scratch go.mod and rejects the
	// unregistered temporary module. A nearest empty workspace keeps the
	// invocation self-contained without editing the user's workspace file.
	workPath := filepath.Join(dir, "go.work")
	if _, err := os.Stat(workPath); os.IsNotExist(err) {
		if err := writeScratch("go.work", []byte("go 1.27\n\nuse .\n")); err != nil {
			cleanup()
			return nil, err
		}
	}
	sumPath := filepath.Join(dir, "go.sum")
	_, sumErr := os.Stat(sumPath)
	sumExisted := sumErr == nil
	createdWork := len(created) == 2
	if err := downloadToolchain(dir); err != nil {
		cleanup()
		return nil, err
	}
	// In the scratch workspace Go records module checksums in go.work.sum;
	// without the workspace file they go in go.sum. Both are invocation-owned
	// only when this function created the workspace file or the sum did not
	// pre-exist.
	workSumPath := filepath.Join(dir, "go.work.sum")
	if createdWork {
		if _, err := os.Stat(workSumPath); err == nil {
			pinFileTime(workSumPath)
			created = append(created, workSumPath)
		}
	}
	if !sumExisted {
		if _, err := os.Stat(sumPath); err == nil {
			pinFileTime(sumPath)
			created = append(created, sumPath)
		}
	} else {
		pinFileTime(sumPath)
	}
	return cleanup, nil
}
