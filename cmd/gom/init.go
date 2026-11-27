package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
	if strings.ContainsAny(modulePath, " \t\n\r") {
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
		fmt.Fprintf(stdout, "created %s\n", gomodPath)
	} else {
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
