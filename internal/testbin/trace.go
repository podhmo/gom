package testbin

import (
	"context"
	"os"
	"os/exec"
)

// CommandContext optionally routes a directly executed fixture through a
// transparent timing wrapper. Ordinary test runs do not use a wrapper.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	if wrapper := os.Getenv("GOM_TRACE_EXEC_WRAPPER"); wrapper != "" {
		return exec.CommandContext(ctx, wrapper, append([]string{name}, args...)...)
	}
	return exec.CommandContext(ctx, name, args...)
}

func Command(name string, args ...string) *exec.Cmd {
	return CommandContext(context.Background(), name, args...)
}
