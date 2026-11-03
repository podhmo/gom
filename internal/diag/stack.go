package diag

import (
	"strconv"
	"strings"
)

// StackFrame is one frame of a Go panic or goroutine dump — the pair
// of a call line and its "\tfile:line" position line:
//
//	main.pending(...)
//		/work/prog.gom:5 +0x20
//
// Generated code carries `//line` directives, so the file operand is
// already the .gom name for frames inside gom output; restoring a
// frame to a byte-offset Span is matching File against the compiled
// inputs and resolving Line — build's GoPosIndex performs that step
// (docs/design.md section 2.2).
type StackFrame struct {
	// Func is the trimmed call line — "main.pending(...)",
	// "m.TestX(0xc000)", or a "created by m.f in goroutine N" line
	// for the frames the runtime reports that way. It is empty when
	// the position line had no call line above it (a hand-edited or
	// truncated trace).
	Func string
	// File is the position's file operand — verbatim, including any
	// `//line`-adjusted .gom name.
	File string
	// Line is the position's line number.
	Line int
}

// ParseStack extracts the frames of a Go panic stack trace or
// goroutine dump. The input may contain anything around the frames —
// the "panic:" head, "goroutine N [state]:" headers, "exit status"
// trailers, several goroutine sections — non-frame lines are skipped.
// The frames come back in trace order, callers pair them with a
// position index to turn File/Line into .gom spans.
func ParseStack(trace string) []StackFrame {
	var frames []StackFrame
	lines := strings.Split(trace, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, "\t") {
			continue
		}
		file, line, ok := parseStackPos(strings.TrimLeft(l, "\t"))
		if !ok {
			continue
		}
		fn := ""
		if i > 0 && !strings.HasPrefix(lines[i-1], "\t") {
			fn = strings.TrimSpace(lines[i-1])
		}
		frames = append(frames, StackFrame{Func: fn, File: file, Line: line})
	}
	return frames
}

// parseStackPos parses the position half of a frame line:
// "file:line +0xoff" or "file:line". The file may itself contain
// ':' or spaces — the last " +0x" suffix is stripped first and the
// last ':' whose tail is all digits splits the line number.
func parseStackPos(s string) (file string, line int, ok bool) {
	if i := strings.LastIndex(s, " +0x"); i >= 0 {
		s = s[:i] // drop the PC-offset trailer
	}
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(s[i+1:])
	if err != nil || n < 0 {
		return "", 0, false
	}
	return s[:i], n, true
}
