package format

import (
	"bytes"
	"fmt"
	"strings"
)

// Diff renders old and new as a unified diff with three lines of
// context — the output of `gom fmt -d`. It returns "" when the two
// inputs are identical.
func Diff(file string, old, new []byte) string {
	if bytes.Equal(old, new) {
		return ""
	}
	ol := splitLines(string(old))
	nl := splitLines(string(new))
	ops := lcsOps(ol, nl)

	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s (formatted)\n", file, file)
	for _, h := range hunks(ops, 3) {
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", h.oldStart, h.oldLen, h.newStart, h.newLen)
		for _, op := range h.ops {
			switch op.kind {
			case opEq:
				b.WriteString(" " + op.text)
			case opDel:
				b.WriteString("-" + op.text)
			case opIns:
				b.WriteString("+" + op.text)
			}
		}
	}
	return b.String()
}

// opKind classifies one line of an edit script.
type opKind int

const (
	opEq opKind = iota
	opDel
	opIns
)

// op is one line of the edit script between two files.
type op struct {
	kind opKind
	text string
}

// splitLines splits s into lines, each keeping its trailing "\n"
// except possibly the last.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// lcsOps diffs two line slices by longest common subsequence and
// returns the edit script: equal, deleted and inserted lines in
// order. The DP table is O(len(a)*len(b)); inputs are single source
// files, so quadratic cost is fine.
func lcsOps(a, b []string) []op {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{opEq, a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, op{opDel, a[i]})
			i++
		default:
			ops = append(ops, op{opIns, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{opDel, a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{opIns, b[j]})
	}
	return ops
}

// hunk is one @@ range of a unified diff.
type hunk struct {
	oldStart, oldLen int
	newStart, newLen int
	ops              []op
}

// hunks groups the edit script into @@ hunks with ctx context lines;
// change runs closer than 2*ctx lines merge into one hunk.
func hunks(ops []op, ctx int) []hunk {
	var out []hunk
	i := 0
	for i < len(ops) {
		// Find the first changed op.
		for i < len(ops) && ops[i].kind == opEq {
			i++
		}
		if i == len(ops) {
			break
		}
		// Hunk starts ctx lines (or fewer) before the first change.
		start := i
		back := 0
		for back < ctx && start-1-back >= 0 && ops[start-1-back].kind == opEq {
			back++
		}
		start -= back
		// Extend while change runs interleave with ≤ 2*ctx eq lines.
		end := i
		lastChange := i
		for end < len(ops) {
			if ops[end].kind != opEq {
				lastChange = end
			} else if end-lastChange > 2*ctx {
				break
			}
			end++
		}
		end = min(lastChange+1+ctx, end)
		h := hunk{ops: ops[start:end]}
		// @@ ranges are 1-based line numbers.
		oi, ni := 1, 1
		for _, o := range ops[:start] {
			if o.kind != opIns {
				oi++
			}
			if o.kind != opDel {
				ni++
			}
		}
		h.oldStart, h.newStart = oi, ni
		for _, o := range h.ops {
			if o.kind != opIns {
				h.oldLen++
			}
			if o.kind != opDel {
				h.newLen++
			}
		}
		out = append(out, h)
		i = end
	}
	return out
}
