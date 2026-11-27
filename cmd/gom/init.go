package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const initMainTemplate = `package main

import go "fmt"

fn main () -> Unit = {
  let _ = fmt.Println("hello")
  ()
}
`

var unsafeModuleChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// runInit implements `gom init [-dir DIR] [MODULE-PATH]`: create a
// go.mod/main.gom pair for a new project without overwriting either file.
func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", ".", "directory to initialize")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "gom init: usage: gom init [-dir DIR] [MODULE-PATH]")
		return 2
	}
	modulePath := ""
	if fs.NArg() == 1 {
		modulePath = fs.Arg(0)
	} else {
		baseDir := *dir
		if abs, err := filepath.Abs(*dir); err == nil {
			baseDir = abs
		}
		base := filepath.Base(filepath.Clean(baseDir))
		base = strings.Trim(unsafeModuleChars.ReplaceAllString(base, "-"), "-.")
		if base == "" {
			base = "project"
		}
		modulePath = "example.com/" + base
	}
	if modulePath == "" || strings.ContainsAny(modulePath, " \t\n\r") {
		fmt.Fprintf(stderr, "gom init: invalid module path %q\n", modulePath)
		return 2
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "gom init: %v\n", err)
		return 2
	}

	gomodPath := filepath.Join(*dir, "go.mod")
	mainPath := filepath.Join(*dir, "main.gom")
	gomodExists := fileExists(gomodPath)
	mainExists := fileExists(mainPath)
	if gomodExists && mainExists {
		fmt.Fprintf(stderr, "gom init: %s already has go.mod and main.gom\n", *dir)
		return 2
	}
	if !gomodExists {
		body, err := moduleFile(modulePath)
		if err != nil {
			fmt.Fprintf(stderr, "gom init: %v\n", err)
			return 2
		}
		if err := writeNewFile(gomodPath, body); err != nil {
			fmt.Fprintf(stderr, "gom init: %v\n", err)
			return 2
		}
		if err := downloadToolchain(*dir); err != nil {
			fmt.Fprintf(stderr, "gom init: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "created %s\n", gomodPath)
	} else {
		if err := ensureGomDependency(*dir, gomodPath); err != nil {
			fmt.Fprintf(stderr, "gom init: %v\n", err)
			return 2
		}
		if err := downloadToolchain(*dir); err != nil {
			fmt.Fprintf(stderr, "gom init: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "kept existing %s\n", gomodPath)
	}
	if !mainExists {
		if err := writeNewFile(mainPath, []byte(initMainTemplate)); err != nil {
			fmt.Fprintf(stderr, "gom init: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "created %s\n", mainPath)
	} else {
		fmt.Fprintf(stdout, "kept existing %s\n", mainPath)
	}
	return 0
}

// ensureGomDependency makes an existing Go module able to compile generated
// Gom code. The edit is additive: it never rewrites the module line or an
// existing requirement. A replace-only mention is not a dependency, and an
// existing replace is preserved rather than duplicated.
func ensureGomDependency(dir, gomodPath string) error {
	b, err := os.ReadFile(gomodPath)
	if err != nil {
		return err
	}
	hasRequire, isGomModule := false, false
	replacements := map[string]bool{}
	inRequireBlock, inReplaceBlock := false, false
	modToken := func(s string) string {
		if unq, err := strconv.Unquote(s); err == nil {
			return unq
		}
		return s
	}
	markReplace := func(f []string, offset int) {
		if len(f) > offset && modToken(f[offset]) == "github.com/podhmo/gom" {
			version := ""
			if len(f) > offset+1 && f[offset+1] != "=>" {
				version = modToken(f[offset+1])
			}
			replacements[version] = true
		}
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		line, _, _ = strings.Cut(line, "//")
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if f[0] == ")" {
			inRequireBlock, inReplaceBlock = false, false
			continue
		}
		if inRequireBlock {
			if modToken(f[0]) == "github.com/podhmo/gom" {
				hasRequire = true
			}
			continue
		}
		if inReplaceBlock {
			markReplace(f, 0)
			continue
		}
		if f[0] == "require" && len(f) >= 2 && f[1] == "(" {
			inRequireBlock = true
			continue
		}
		if f[0] == "replace" && len(f) >= 2 && f[1] == "(" {
			inReplaceBlock = true
			continue
		}
		if len(f) >= 2 && f[0] == "module" {
			isGomModule = modToken(f[1]) == "github.com/podhmo/gom"
		}
		if len(f) >= 2 && f[0] == "require" && modToken(f[1]) == "github.com/podhmo/gom" {
			hasRequire = true
		}
		if len(f) >= 2 && f[0] == "replace" {
			markReplace(f, 1)
		}
	}
	if hasRequire || isGomModule {
		return nil
	}
	req, rep, err := toolchainRequirement()
	if err != nil {
		return err
	}
	var out strings.Builder
	out.Write(b)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		out.WriteByte('\n')
	}
	fmt.Fprintf(&out, "\nrequire %s\n", req)
	reqVersion := ""
	if parts := strings.Fields(req); len(parts) == 2 {
		reqVersion = parts[1]
	}
	if rep != "" && !replacements[""] && !replacements[reqVersion] {
		fmt.Fprintf(&out, "\nreplace %s\n", rep)
	}
	return os.WriteFile(gomodPath, []byte(out.String()), 0o644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeNewFile(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
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
	return nil
}
