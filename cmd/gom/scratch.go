package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
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

// pinFileTime gives scratch files a deterministic content-derived mtime.
// The export cache keys on mtimes rather than file contents; a constant
// time would keep stale entries alive across toolchain replacement changes,
// while the creation time would needlessly miss every invocation.
func pinFileTime(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	sum := sha256.Sum256(b)
	sec := int64(binary.BigEndian.Uint64(sum[:8])%(10*365*24*60*60)) + 1600000000
	_ = os.Chtimes(path, timeUnix(sec), timeUnix(sec))
}

func timeUnix(sec int64) time.Time { return time.Unix(sec, 0) }

// downloadToolchain resolves the release-module requirement once so the
// scratch module carries the checksums the read-only Go build mode needs.
// Local replaces do not need a sum, but running the same command keeps the
// two paths on one contract and surfaces toolchain-resolution errors before
// compilation rather than at the final link step.
func downloadToolchain(dir string) error {
	out, err := runDownloadToolchain(dir, os.Environ())
	if err == nil {
		return nil
	}
	// A module under a parent workspace can use that workspace's replace
	// directives, so try the caller's resolution first. A directory not yet
	// listed in the workspace cannot be resolved there; retry outside the
	// workspace before reporting the original failure shape.
	if _, ierr := runDownloadToolchain(dir, replaceEnv(os.Environ(), "GOWORK", "off")); ierr == nil {
		return nil
	}
	return fmt.Errorf("go mod download github.com/podhmo/gom: %w: %s", err, bytes.TrimSpace(out))
}

// downloadToolchainStandalone resolves the toolchain exactly as the new
// module will outside any caller workspace. `gom init` must not silently
// rely on a parent go.work replace or checksum: a project that only works
// while initialized inside that workspace is not yet standalone.
func downloadToolchainStandalone(dir string) error {
	out, err := runDownloadToolchain(dir, replaceEnv(os.Environ(), "GOWORK", "off"))
	if err != nil {
		return fmt.Errorf("go mod download github.com/podhmo/gom with GOWORK=off: %w: %s (gom init requires the project to resolve outside the caller's workspace; use a released gom version or add an explicit replace in the project go.mod)", err, bytes.TrimSpace(out))
	}
	return nil
}

func runDownloadToolchain(dir string, env []string) ([]byte, error) {
	cmd := exec.Command("go", "mod", "download", "github.com/podhmo/gom")
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return out, err
}

func setEnv(key, value string) func() {
	old, existed := os.LookupEnv(key)
	_ = os.Setenv(key, value)
	return func() {
		if existed {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	}
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	seen := false
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			if !seen {
				out = append(out, prefix+value)
				seen = true
			}
			continue
		}
		out = append(out, e)
	}
	if !seen {
		out = append(out, prefix+value)
	}
	return out
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
	// A go.work already in this directory would capture every Go subprocess
	// before the scratch go.mod. It is user-owned and cannot be shadowed
	// non-destructively, so refuse rather than editing or deleting it.
	if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
		cleanup()
		return nil, fmt.Errorf("module-external run is not supported in a directory already containing go.work")
	}
	sumPath := filepath.Join(dir, "go.sum")
	_, sumErr := os.Stat(sumPath)
	sumExisted := sumErr == nil
	if err := downloadToolchain(dir); err != nil {
		cleanup()
		return nil, err
	}
	if !sumExisted {
		if _, err := os.Stat(sumPath); err == nil {
			pinFileTime(sumPath)
			created = append(created, sumPath)
		}
	}
	return cleanup, nil
}
