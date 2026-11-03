package diag

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// TextOptions controls Report.WriteText, the human-readable
// counterpart of the JSON Lines format (docs/spec.md section 12).
type TextOptions struct {
	// Color enables ANSI styling: severity words and underline carets
	// take the severity's color, structural marks ("-->", "|", "=")
	// are blue, and headers are bold. The zero value renders plain
	// text. The writer is never inspected — the caller decides (TTY
	// detection, NO_COLOR, a --color flag).
	Color bool
}

// ANSI styling used when TextOptions.Color is set.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiBlue   = "\x1b[34m"
)

// markStyle colors the structural marks "-->", "|" and "=".
const markStyle = ansiBlue

// sevStyle is the header/caret style of a severity.
func sevStyle(s Severity) string {
	switch s {
	case SeverityError:
		return ansiBold + ansiRed
	case SeverityWarning:
		return ansiBold + ansiYellow
	default:
		return ansiBold + ansiBlue
	}
}

// WriteText renders the report in the human-readable format
// (docs/spec.md section 12, "--format text"; the spec's E0301 example
// in section 4 shows the shape):
//
//	error[E0301]: non-exhaustive match on `Expr`
//	  --> eval.gom:2:3
//	   |
//	 2 |   match s {
//	   |   ^^^^^^^
//	   = note: arms with guards do not count toward exhaustiveness
//
// The layout follows the spec example's offsets, which are rustc's
// with one extra column: the structural marks "|" and "=" sit in the
// gutter one column wide of the largest line number. Like rustc,
// diagnostics are separated by a blank line and the output ends with
// a one-line trailer ("aborting due to previous error", "N warnings
// emitted", or "success").
//
// Text is a lossy rendering of the same internal diagnostics: it shows
// the header, the primary span with its source snippet, notes, and fix
// titles — structured Data and fix edits are machine-facing detail and
// are not rendered.
//
// Diagnostics are emitted in List.Sort order on a copy of the list.
// Each diagnostic must pass Validate and located spans must resolve
// through res; the first failure stops the write and is returned —
// the driver reports such failures as internal errors (exit code 2).
func (r Report) WriteText(w io.Writer, res Resolver, opts TextOptions) error {
	tw := &textWriter{w: w, res: res, color: opts.Color}
	diags := r.orderedDiagnostics()
	for i := range diags {
		if err := diags[i].Validate(); err != nil {
			return fmt.Errorf("diag: cannot emit diagnostics[%d]: %w", i, err)
		}
		if i > 0 {
			tw.printf("\n")
		}
		if err := tw.diagnostic(diags[i]); err != nil {
			return fmt.Errorf("diag: cannot emit diagnostics[%d]: %w", i, err)
		}
	}
	if len(diags) > 0 {
		tw.printf("\n")
	}
	for i := range r.Tests {
		if i > 0 {
			tw.printf("\n")
		}
		tw.test(r.Tests[i])
	}
	if len(r.Tests) > 0 {
		tw.printf("\n")
	}
	tw.trailer(r)
	return tw.err
}

// test renders one test result:
//
//	test "rect area": ok (0.00s)
//	test "fails": FAIL (0.00s)
//	  --> area.gom:33:7
//	    area.gom:33: gom: assertion failed:
//	          actual:   6
//	          expected: 7
//
// The output lines (assertion values, panic traces) keep go test's
// own layout — they already carry the .gom positions the generated
// //line directives set.
func (tw *textWriter) test(t TestResult) {
	tag, style := "ok", ansiBold+ansiGreen
	switch {
	case t.Skipped:
		tag, style = "SKIP", ansiBold+ansiYellow
	case !t.OK:
		tag, style = "FAIL", ansiBold+ansiRed
	}
	tw.printf("%s %s (%.2fs)\n",
		tw.paint(ansiBold, "test "+strconv.Quote(t.Name)), tw.paint(style, tag), t.Elapsed)
	if !t.OK && !t.Span.IsZero() {
		// The declaration position matters only on failure — a
		// passing test's span is noise next to its verdict.
		if li, err := resolveIndex(tw.res, t.Span.File); err == nil {
			start := li.Pos(t.Span.Start)
			tw.printf("  %s %s:%d:%d\n", tw.paint(markStyle, "-->"),
				t.Span.File, start.Line, start.Col)
		}
	}
	for _, line := range strings.Split(strings.TrimRight(t.Output, "\n"), "\n") {
		if line != "" {
			tw.printf("    %s\n", line)
		}
	}
}

// textWriter accumulates one text stream, keeping the first write
// error so rendering helpers need no error plumbing.
type textWriter struct {
	w     io.Writer
	res   Resolver
	color bool
	err   error
}

// paint wraps s in an ANSI style when color is enabled.
func (tw *textWriter) paint(style, s string) string {
	if !tw.color || s == "" {
		return s
	}
	return style + s + ansiReset
}

// printf writes a formatted line; after a write error it is a no-op.
func (tw *textWriter) printf(format string, args ...any) {
	if tw.err != nil {
		return
	}
	_, tw.err = fmt.Fprintf(tw.w, format, args...)
}

// diagnostic renders one diagnostic block.
func (tw *textWriter) diagnostic(d Diagnostic) error {
	tag := d.Severity.String() + "[" + string(d.Code) + "]"
	tw.printf("%s: %s\n", tw.paint(sevStyle(d.Severity), tag), tw.paint(ansiBold, d.Message))

	// The gutter width is the digit count of the largest displayed
	// line number; positionless diagnostics render with width 1.
	gutter := 1
	shown := false
	if !d.Span.IsZero() {
		li, err := resolveIndex(tw.res, d.Span.File)
		if err != nil {
			return fmt.Errorf("diag: diagnostic %s: cannot resolve %q: %w",
				d.Code, d.Span.File, err)
		}
		start := li.Pos(d.Span.Start)
		first := start.Line
		// The span end is exclusive, so the last covered position is
		// the rune containing End-1: a span ending at a line break
		// underlines through it instead of spilling onto the next
		// line, and one ending mid-rune still covers the whole rune.
		last, endCol := first, start.Col+1
		if d.Span.End > d.Span.Start {
			lastByte := d.Span.End - 1
			last = li.Pos(lastByte).Line
			ls, _ := li.LineBounds(last)
			rel := lastByte - ls
			lastText := li.LineText(last)
			for rel > 0 && rel < len(lastText) && !utf8.RuneStart(lastText[rel]) {
				rel--
			}
			endCol = li.Pos(ls+rel).Col + 1
		}
		gutter = digitWidth(last)
		pad := strings.Repeat(" ", gutter+1)
		tw.printf("%s %s:%d:%d\n", tw.paint(markStyle, pad+"-->"),
			d.Span.File, start.Line, start.Col)
		tw.printf("%s\n", tw.paint(markStyle, pad+" |"))
		for line := first; line <= last; line++ {
			raw := string(li.LineText(line))
			text := expandTabs(raw)
			ucol, uend := 1, endCol
			if line == first {
				ucol = start.Col
			}
			if line < last {
				uend = utf8.RuneCountInString(raw) + 1
			}
			indent, carets := underlineCells(raw, ucol, uend)
			tw.printf("%*d  | %s\n", gutter, line, text)
			tw.printf("%s%s%s\n", tw.paint(markStyle, pad+" | "),
				strings.Repeat(" ", indent), tw.paint(sevStyle(d.Severity), strings.Repeat("^", carets)))
		}
		shown = true
	}

	pad := strings.Repeat(" ", gutter+1)
	if shown && (len(d.Notes) > 0 || len(d.Fixes) > 0) {
		tw.printf("%s\n", tw.paint(markStyle, pad+" |"))
	}
	for _, n := range d.Notes {
		tw.labeled(pad, "note", n)
	}
	for _, f := range d.Fixes {
		suffix := ""
		switch f.Applicability {
		case ApplicabilityPlaceholder:
			suffix = " (placeholder)"
		case ApplicabilityUnsafe:
			suffix = " (unsafe)"
		}
		tw.labeled(pad, "help", f.Title+suffix)
	}
	return tw.err
}

// labeled prints " = <label>: <text>" in the gutter; continuation
// lines of a multi-line text align under the first line's text.
func (tw *textWriter) labeled(pad, label, text string) {
	cont := strings.Repeat(" ", len(pad)+len(" = "+label+": "))
	for i, line := range strings.Split(text, "\n") {
		if i == 0 {
			tw.printf("%s%s%s\n", pad, tw.paint(markStyle, " = "+label+": "), line)
		} else {
			tw.printf("%s%s\n", cont, line)
		}
	}
}

// trailer prints the closing one-line summary rustc-style. Test
// failures rank between errors and warnings: a clean compile whose
// tests failed closes "error: M of N tests failed" rather than
// "success".
func (tw *textWriter) trailer(r Report) {
	errors, warnings := List(r.Diagnostics).Counts()
	run, failed, _ := r.testStats()
	switch {
	case errors > 0:
		var b strings.Builder
		if errors == 1 {
			b.WriteString("aborting due to previous error")
		} else {
			fmt.Fprintf(&b, "aborting due to %d previous errors", errors)
		}
		if warnings > 0 {
			fmt.Fprintf(&b, "; %d warning%s emitted", warnings, plural(warnings))
		}
		tw.printf("%s: %s\n", tw.paint(sevStyle(SeverityError), "error"), b.String())
	case failed > 0:
		var b strings.Builder
		fmt.Fprintf(&b, "%d of %d test%s failed", failed, run, plural(run))
		if warnings > 0 {
			fmt.Fprintf(&b, "; %d warning%s emitted", warnings, plural(warnings))
		}
		tw.printf("%s: %s\n", tw.paint(sevStyle(SeverityError), "error"), b.String())
	case warnings > 0:
		tw.printf("%s: %d warning%s emitted\n",
			tw.paint(sevStyle(SeverityWarning), "warning"), warnings, plural(warnings))
	default:
		tw.printf("%s\n", tw.paint(ansiBold+ansiGreen, "success"))
	}
}

// digitWidth returns the number of decimal digits of n (n >= 1).
func digitWidth(n int) int {
	w := 1
	for n >= 10 {
		n /= 10
		w++
	}
	return w
}

// plural returns "s" when n is not 1, for "N previous errors"-style
// phrasing.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// expandTabs renders a source line for display: '\t' becomes four
// spaces, matching the cell width underlineCells assumes.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	return strings.ReplaceAll(s, "\t", "    ")
}

// underlineCells computes the underline of the code-point column range
// [ucol, uend) on one line: the indent (display cells before the
// underline) and the caret count (display cells it covers). Every rune
// counts as one cell except '\t', which counts four to match
// expandTabs. Columns beyond the line's content — a span ending on
// the line's '\n', or past the end of an empty line — count one cell
// each; a zero-width range still emits one caret.
func underlineCells(text string, ucol, uend int) (indent, carets int) {
	col := 1
	for _, r := range text {
		if col >= uend {
			break
		}
		if col >= ucol {
			carets += runeCellWidth(r)
		} else {
			indent += runeCellWidth(r)
		}
		col++
	}
	for col < uend {
		carets++
		col++
	}
	if carets == 0 {
		carets = 1
	}
	return indent, carets
}

// runeCellWidth is the display width of one rune under the text
// format's column accounting: one cell per code point (the spec's
// column unit), four for '\t' to match the expanded display.
func runeCellWidth(r rune) int {
	if r == '\t' {
		return 4
	}
	return 1
}
