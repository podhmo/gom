package format

import (
	"os"
	"testing"
)

func TestNormDump(t *testing.T) {
	path := os.Getenv("NORMDUMP")
	if path == "" {
		t.Skip("NORMDUMP=<file>")
	}
	src, _ := os.ReadFile(path)
	out, _ := Source(src, path, false)
	os.WriteFile("/tmp/norm_in.txt", []byte(parseNorm(t, src, path, false)), 0o644)
	os.WriteFile("/tmp/norm_out.txt", []byte(parseNorm(t, out, path, false)), 0o644)
	os.WriteFile("/tmp/fmt_out.gom", out, 0o644)
}
