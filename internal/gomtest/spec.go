package gomtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// RunSpecTests runs the expected-diagnostics suite in dir against
// compile, one subtest per case ("" selects the repo's testdata/spec).
// A case is a single <name>.gom file, or a directory <name>/ marked
// by a bare side file (expected.json, golden.go or case.json) whose
// member .gom files compile as one package unit — a .gomi member
// compiles as an interface file in its own scope; the compile
// function receives the whole member list either way.
//
// Matching semantics — every field present in an expected diagnostic
// object constrains the actual one; absent fields are unchecked:
//
//   - "kind" must be "diagnostic" if present.
//   - "severity", "code", "message", "source": exact string match.
//   - "span": null requires a positionless diagnostic. Otherwise the
//     actual span is resolved through the named file's source under the
//     suite dir, and "file" plus each present "line"/"col"/"offset" key
//     of "start" and "end" must match. Expected files may check only
//     byte offsets or only line/columns.
//   - "data": subset match — each expected key must exist in the
//     actual data with a JSON-equal value. Extra actual keys pass.
//   - "notes": each expected note must appear among the actual notes,
//     in order (a subsequence). Extra actual notes pass.
//   - "fixes": each expected fix must match some actual fix in order.
//     A fix matches when its "title"/"applicability" equal the present
//     expected values and, if "edits" is present, the edits are equal
//     in number and pairwise matching (same fields as "span" plus
//     "text").
//
// The actual diagnostics are sorted with diag.List.Sort before
// comparison, so the expected list is authored in output order and its
// length must equal the actual count exactly.
//
// With -update the expected file is rewritten from the actual
// diagnostics rendered to the wire shape of spec section 12; the file
// is created if missing.
func RunSpecTests(t *testing.T, dir string, compile DiagnosticsFunc) {
	t.Helper()
	if compile == nil {
		t.Fatal("gomtest: nil DiagnosticsFunc")
	}
	suiteDir := resolveSuiteDir(t, dir, "spec")
	runCases(t, suiteDir, "spec", func(t *testing.T, c testCase) {
		t.Helper()
		got, err := compile(c.files)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		res := diag.NewDirResolver(suiteDir)
		expectedPath := c.sidePath(expectedExt)

		if *update {
			writeExpected(t, expectedPath, got, res)
			return
		}

		exp, err := loadExpected(expectedPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, msg := range diffDiagnostics(exp, got, res) {
			t.Error(msg)
		}
	})
}

// loadExpected parses a <name>.expected.json file into the list of
// expected diagnostic objects.
func loadExpected(path string) ([]map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("gomtest: missing expected file %s "+
			"(add {\"diagnostics\": []} or run `go test -update`)", path)
	}
	if err != nil {
		return nil, fmt.Errorf("gomtest: cannot read %s: %w", path, err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("gomtest: invalid JSON in %s: %w", path, err)
	}
	v, ok := root["diagnostics"]
	if !ok {
		return nil, fmt.Errorf("gomtest: %s: missing \"diagnostics\" key", path)
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("gomtest: %s: \"diagnostics\" must be an array", path)
	}
	exp := make([]map[string]any, 0, len(arr))
	for i, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("gomtest: %s: diagnostics[%d] must be an object", path, i)
		}
		exp = append(exp, m)
	}
	return exp, nil
}

// writeExpected renders the actual diagnostics — sorted into output
// order — to the spec section 12 wire shape and rewrites the expected
// file (-update). The diagnostics are encoded by
// diag.MarshalDiagnostic, the same encoder the compiler emits, and
// indented with json.Indent so the file keeps their bytes verbatim.
func writeExpected(t *testing.T, path string, got []diag.Diagnostic, res diag.Resolver) {
	t.Helper()
	got = sortedDiagnostics(got)
	var doc bytes.Buffer
	doc.WriteString(`{"diagnostics":[`)
	for i, d := range got {
		raw, err := diag.MarshalDiagnostic(d, res)
		if err != nil {
			t.Fatalf("gomtest: cannot render diagnostics[%d] for %s: %v", i, path, err)
		}
		if i > 0 {
			doc.WriteByte(',')
		}
		doc.Write(raw)
	}
	doc.WriteString("]}")
	var out bytes.Buffer
	if err := json.Indent(&out, doc.Bytes(), "", "  "); err != nil {
		t.Fatalf("gomtest: cannot encode %s: %v", path, err)
	}
	out.WriteByte('\n')
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatalf("gomtest: cannot write %s: %v", path, err)
	}
	t.Logf("updated %s", path)
}

// sortedDiagnostics returns a copy of got ordered into the output
// order the compiler emits (diag.List.Sort).
func sortedDiagnostics(got []diag.Diagnostic) []diag.Diagnostic {
	sorted := slices.Clone(got)
	diag.List(sorted).Sort()
	return sorted
}

// diffDiagnostics compares the expected diagnostic objects against the
// actual diagnostics, sorting a copy of the actual list into output
// order first. It returns one message per discrepancy.
func diffDiagnostics(exp []map[string]any, got []diag.Diagnostic, res diag.Resolver) []string {
	var msgs []string
	got = sortedDiagnostics(got)
	if len(exp) != len(got) {
		msgs = append(msgs, fmt.Sprintf("diagnostic count: expected %d, got %d\nactual: %s",
			len(exp), len(got), dumpDiagnostics(got, res)))
	}
	for i := 0; i < len(exp) && i < len(got); i++ {
		msgs = append(msgs, diffDiagnostic(exp[i], got[i], res,
			fmt.Sprintf("diagnostics[%d]", i))...)
	}
	return msgs
}

// dumpDiagnostics renders actual diagnostics for a failure message on
// a best-effort basis: spans that cannot be resolved degrade to byte
// offsets.
func dumpDiagnostics(got []diag.Diagnostic, res diag.Resolver) string {
	var buf bytes.Buffer
	for i, d := range got {
		if i > 0 {
			buf.WriteString(", ")
		}
		if raw, err := diag.MarshalDiagnostic(d, res); err == nil {
			buf.Write(raw)
			continue
		}
		b, _ := json.Marshal(map[string]any{
			"severity": d.Severity.String(),
			"code":     string(d.Code),
			"message":  d.Message,
			"span":     fmt.Sprintf("%s:%d-%d", d.Span.File, d.Span.Start, d.Span.End),
		})
		buf.Write(b)
	}
	return buf.String()
}

// diffDiagnostic checks one actual diagnostic against one expected
// object and returns the mismatches found, prefixed by path.
func diffDiagnostic(exp map[string]any, got diag.Diagnostic, res diag.Resolver, path string) (msgs []string) {
	if v, ok := exp["kind"]; ok {
		if s, ok := v.(string); !ok || s != "diagnostic" {
			msgs = append(msgs, fmt.Sprintf("%s.kind: expected \"diagnostic\", got %s", path, jsonRepr(v)))
		}
	}
	msgs = append(msgs, checkString(exp, "severity", got.Severity.String(), path)...)
	msgs = append(msgs, checkString(exp, "code", string(got.Code), path)...)
	msgs = append(msgs, checkString(exp, "message", got.Message, path)...)
	msgs = append(msgs, checkString(exp, "source", string(got.Source), path)...)
	msgs = append(msgs, checkSpan(exp, got.Span, res, path)...)
	msgs = append(msgs, checkData(exp, got.Data, path)...)
	msgs = append(msgs, checkNotes(exp, got.Notes, path)...)
	msgs = append(msgs, checkFixes(exp, got.Fixes, res, path)...)
	return msgs
}

// checkString compares the expected field key (if present) with got.
func checkString(exp map[string]any, key, got, path string) []string {
	v, ok := exp[key]
	if !ok {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return []string{fmt.Sprintf("%s.%s: expected a string, got %s", path, key, jsonRepr(v))}
	}
	if s != got {
		return []string{fmt.Sprintf("%s.%s: expected %q, got %q", path, key, s, got)}
	}
	return nil
}

// checkSpan verifies the "span" field: null expects a positionless
// diagnostic; an object checks "file" plus per-endpoint position keys.
func checkSpan(exp map[string]any, sp diag.Span, res diag.Resolver, path string) (msgs []string) {
	v, ok := exp["span"]
	if !ok {
		return nil
	}
	if v == nil {
		if !sp.IsZero() {
			return []string{fmt.Sprintf("%s.span: expected no span, got %s:%d-%d",
				path, sp.File, sp.Start, sp.End)}
		}
		return nil
	}
	em, ok := v.(map[string]any)
	if !ok {
		return []string{fmt.Sprintf("%s.span: expected an object or null, got %s", path, jsonRepr(v))}
	}
	if sp.IsZero() {
		return []string{fmt.Sprintf("%s.span: expected %s, got no span", path, jsonRepr(v))}
	}
	return checkSpanFields(em, sp, res, path+".span")
}

// checkSpanFields compares the file/start/end keys of an expected span
// object (also used for fix edits, which add "text") against sp.
func checkSpanFields(em map[string]any, sp diag.Span, res diag.Resolver, path string) (msgs []string) {
	if v, ok := em["file"]; ok {
		s, ok := v.(string)
		if !ok {
			msgs = append(msgs, fmt.Sprintf("%s.file: expected a string, got %s", path, jsonRepr(v)))
		} else if s != sp.File {
			msgs = append(msgs, fmt.Sprintf("%s.file: expected %q, got %q", path, s, sp.File))
		}
	}

	needPos := false
	for _, end := range []string{"start", "end"} {
		if em2, ok := em[end].(map[string]any); ok {
			if _, ok := em2["line"]; ok {
				needPos = true
			}
			if _, ok := em2["col"]; ok {
				needPos = true
			}
		}
	}
	var rp diag.SpanPos
	resolved := false
	if needPos {
		var err error
		rp, err = diag.ResolveSpan(res, sp)
		if err != nil {
			msgs = append(msgs, fmt.Sprintf("%s: cannot resolve span: %v "+
				"(the expected file checks line/column but the source for %q is unreadable)",
				path, err, sp.File))
		} else {
			resolved = true
		}
	}
	for _, end := range []string{"start", "end"} {
		v, ok := em[end]
		if !ok {
			continue
		}
		em2, ok := v.(map[string]any)
		if !ok {
			msgs = append(msgs, fmt.Sprintf("%s.%s: expected an object, got %s", path, end, jsonRepr(v)))
			continue
		}
		pos, off := rp.Start, sp.Start
		if end == "end" {
			pos, off = rp.End, sp.End
		}
		msgs = append(msgs, checkEndpoint(em2, pos, off, resolved, path+"."+end)...)
	}
	return msgs
}

// checkEndpoint compares each present "line"/"col"/"offset" key of an
// expected position object against the actual resolved/raw position.
func checkEndpoint(em map[string]any, pos diag.Pos, offset int, resolved bool, path string) (msgs []string) {
	for _, k := range []string{"line", "col", "offset"} {
		v, ok := em[k]
		if !ok {
			continue
		}
		n, ok := v.(float64)
		if !ok {
			msgs = append(msgs, fmt.Sprintf("%s.%s: expected a number, got %s", path, k, jsonRepr(v)))
			continue
		}
		var got int
		switch k {
		case "offset":
			got = offset
		case "line":
			got = pos.Line
		case "col":
			got = pos.Col
		}
		if k != "offset" && !resolved {
			msgs = append(msgs, fmt.Sprintf("%s.%s: cannot compare (span not resolved)", path, k))
			continue
		}
		if n != float64(got) {
			msgs = append(msgs, fmt.Sprintf("%s.%s: expected %v, got %d", path, k, n, got))
		}
	}
	return msgs
}

// checkData verifies subset inclusion: every expected "data" key must
// exist in the actual data with a JSON-equal value.
func checkData(exp map[string]any, data map[string]any, path string) (msgs []string) {
	v, ok := exp["data"]
	if !ok {
		return nil
	}
	em, ok := v.(map[string]any)
	if !ok {
		return []string{fmt.Sprintf("%s.data: expected an object, got %s", path, jsonRepr(v))}
	}
	var norm map[string]any
	if len(data) > 0 {
		n, err := normalizeJSON(data)
		if err != nil {
			return []string{fmt.Sprintf("%s.data: actual data is not JSON-representable: %v", path, err)}
		}
		norm, _ = n.(map[string]any)
	}
	for k, ev := range em {
		gv, ok := norm[k]
		if !ok {
			msgs = append(msgs, fmt.Sprintf("%s.data[%q]: key missing from actual data", path, k))
			continue
		}
		if !jsonEqual(ev, gv) {
			msgs = append(msgs, fmt.Sprintf("%s.data[%q]: expected %s, got %s",
				path, k, jsonRepr(ev), jsonRepr(gv)))
		}
	}
	return msgs
}

// checkNotes verifies that each expected note appears among the actual
// notes in order (subsequence semantics).
func checkNotes(exp map[string]any, notes []string, path string) (msgs []string) {
	v, ok := exp["notes"]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return []string{fmt.Sprintf("%s.notes: expected an array, got %s", path, jsonRepr(v))}
	}
	from := 0
	for i, e := range arr {
		s, ok := e.(string)
		if !ok {
			msgs = append(msgs, fmt.Sprintf("%s.notes[%d]: expected a string, got %s", path, i, jsonRepr(e)))
			continue
		}
		j := from
		for j < len(notes) && notes[j] != s {
			j++
		}
		if j == len(notes) {
			msgs = append(msgs, fmt.Sprintf("%s.notes[%d]: expected note not found: %q "+
				"(actual notes: %s)", path, i, s, jsonRepr(notes)))
		} else {
			from = j + 1
		}
	}
	return msgs
}

// checkFixes verifies that each expected fix matches some actual fix
// in order (subsequence semantics).
func checkFixes(exp map[string]any, fixes []diag.Fix, res diag.Resolver, path string) (msgs []string) {
	v, ok := exp["fixes"]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return []string{fmt.Sprintf("%s.fixes: expected an array, got %s", path, jsonRepr(v))}
	}
	from := 0
	for i, e := range arr {
		em, ok := e.(map[string]any)
		if !ok {
			msgs = append(msgs, fmt.Sprintf("%s.fixes[%d]: expected an object, got %s", path, i, jsonRepr(e)))
			continue
		}
		found := -1
		for j := from; j < len(fixes); j++ {
			if fixMatches(em, fixes[j], res) {
				found = j
				break
			}
		}
		if found < 0 {
			msgs = append(msgs, fmt.Sprintf("%s.fixes[%d]: no matching actual fix: %s "+
				"(actual fix titles: %s)", path, i, jsonRepr(em), jsonRepr(fixTitles(fixes))))
		} else {
			from = found + 1
		}
	}
	return msgs
}

// fixMatches reports whether the actual fix satisfies every field
// present in the expected fix object.
func fixMatches(em map[string]any, f diag.Fix, res diag.Resolver) bool {
	if v, ok := em["title"]; ok {
		s, ok := v.(string)
		if !ok || s != f.Title {
			return false
		}
	}
	if v, ok := em["applicability"]; ok {
		s, ok := v.(string)
		if !ok || s != f.Applicability.String() {
			return false
		}
	}
	if v, ok := em["edits"]; ok {
		arr, ok := v.([]any)
		if !ok || len(arr) != len(f.Edits) {
			return false
		}
		for i, e := range arr {
			em2, ok := e.(map[string]any)
			if !ok || !editMatches(em2, f.Edits[i], res) {
				return false
			}
		}
	}
	return true
}

// editMatches reports whether the actual edit satisfies the expected
// edit object: "file" and "text" exact, "start"/"end" per-key.
func editMatches(em map[string]any, e diag.Edit, res diag.Resolver) bool {
	if v, ok := em["text"]; ok {
		s, ok := v.(string)
		if !ok || s != e.Text {
			return false
		}
	}
	return spanFieldsMatch(em, e.Span, res)
}

// spanFieldsMatch is the boolean counterpart of checkSpanFields, for
// subsequence matching inside fixes.
func spanFieldsMatch(em map[string]any, sp diag.Span, res diag.Resolver) bool {
	if v, ok := em["file"]; ok {
		s, ok := v.(string)
		if !ok || s != sp.File {
			return false
		}
	}
	var rp diag.SpanPos
	resolved := false
	for _, end := range []string{"start", "end"} {
		v, ok := em[end]
		if !ok {
			continue
		}
		em2, ok := v.(map[string]any)
		if !ok {
			return false
		}
		if !resolved && (has(em2, "line") || has(em2, "col")) {
			var err error
			rp, err = diag.ResolveSpan(res, sp)
			if err != nil {
				return false
			}
			resolved = true
		}
		pos, off := rp.Start, sp.Start
		if end == "end" {
			pos, off = rp.End, sp.End
		}
		if !endpointMatches(em2, pos, off) {
			return false
		}
	}
	return true
}

// endpointMatches reports whether each present key of an expected
// position object equals the actual one. pos is consulted only when
// the expected object has been resolved (spanFieldsMatch resolves
// lazily).
func endpointMatches(em map[string]any, pos diag.Pos, offset int) bool {
	for _, k := range []string{"line", "col", "offset"} {
		v, ok := em[k]
		if !ok {
			continue
		}
		n, ok := v.(float64)
		if !ok {
			return false
		}
		var got int
		switch k {
		case "offset":
			got = offset
		case "line":
			got = pos.Line
		case "col":
			got = pos.Col
		}
		if n != float64(got) {
			return false
		}
	}
	return true
}

func fixTitles(fixes []diag.Fix) []string {
	titles := make([]string, len(fixes))
	for i, f := range fixes {
		titles[i] = f.Title
	}
	return titles
}

func has(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

// normalizeJSON round-trips v through JSON so that Go values held in
// diag.Diagnostic.Data compare equal to values decoded from expected
// JSON (e.g. ints become float64s, slices become []any).
func normalizeJSON(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// jsonEqual compares two JSON-decoded values for structural equality.
func jsonEqual(a, b any) bool {
	ba, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ba, bb)
}

// jsonRepr renders a JSON-decoded value compactly for messages.
func jsonRepr(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return string(b)
}
