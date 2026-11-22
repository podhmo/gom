package diag

import "strings"

// AllowDirective scans .gom source text for `//gom:allow <code>...`
// directives (spec section 12): each line names the diagnostic codes
// the file permits to drop from error to warning. It returns the set
// of relaxable codes the file allows plus one W0003 diagnostic per
// unusable entry — an unknown code, a registered code that is not
// relaxable, or a directive without codes. Unusable lines report
// instead of silently doing nothing, matching the table's rule that
// directives never turn a diagnostic off entirely.
//
// The scan is textual, like the `//gom:noescape` reader: a stale or
// misplaced comment must not break the parse, so lines are matched on
// the trimmed "//gom:allow " prefix regardless of position.
func ScanAllow(src []byte, path string) (allow map[Code]bool, diags List) {
	off := 0
	for _, line := range strings.Split(string(src), "\n") {
		start := off
		off += len(line) + 1
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//gom:allow") {
			continue
		}
		rest := trimmed[len("//gom:allow"):]
		if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			// `//gom:allowE0301` is not a directive but its intent is
			// obvious — warn rather than demote anything on a line
			// that only looks like the directive.
			diags.Add(Warning("W0003", "malformed `//gom:allow` directive: expected whitespace after `allow`").
				At(Span{File: path, Start: start, End: start + len(line)}))
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest == "" {
			diags.Add(Warning("W0003", "`//gom:allow` needs at least one diagnostic code").
				At(Span{File: path, Start: start, End: start + len(line)}))
			continue
		}
		for _, f := range strings.Fields(rest) {
			c := Code(f)
			info, ok := LookupCode(c)
			switch {
			case !ok:
				diags.Add(Warning("W0003", "unknown diagnostic code in `//gom:allow`: "+f).
					At(Span{File: path, Start: start, End: start + len(line)}).
					Set("code", f))
			case !info.Relaxable:
				diags.Add(Warning("W0003", "`//gom:allow` cannot demote "+f+": the code is not relaxable").
					At(Span{File: path, Start: start, End: start + len(line)}).
					Set("code", f))
			default:
				if allow == nil {
					allow = map[Code]bool{}
				}
				allow[c] = true
			}
		}
	}
	return allow, diags
}

// DemoteAllowed lowers the severity of every error diagnostic in ds
// whose span's file allows the code — the directive's effect. allows
// maps a file path to its `//gom:allow` code set. The diagnostic keeps
// its code and span: a demoted E0301 still reports as E0301, at
// warning severity, so it is never silent.
func DemoteAllowed(ds List, allows map[string]map[Code]bool) {
	for i, d := range ds {
		if d.Severity == SeverityError && allows[d.Span.File][d.Code] {
			ds[i].Severity = SeverityWarning
		}
	}
}
