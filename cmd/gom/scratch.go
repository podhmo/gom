package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
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
		return "github.com/podhmo/gom v0.0.0", "github.com/podhmo/gom => " + abs, nil
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		if strings.HasPrefix(bi.Main.Path, "github.com/podhmo/gom") {
			return "github.com/podhmo/gom " + bi.Main.Version, "", nil
		}
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		for d := filepath.Dir(file); ; d = filepath.Dir(d) {
			if isGomSourceRoot(d) {
				return "github.com/podhmo/gom v0.0.0", "github.com/podhmo/gom => " + d, nil
			}
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
		}
	}
	return "", "", fmt.Errorf("cannot determine the github.com/podhmo/gom module version used by this gom binary")
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
	path := filepath.Join(dir, "go.mod")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create scratch module %s: %w", path, err)
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}
