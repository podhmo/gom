package fuzzutil

import "testing"

// A knownBugs link is satisfied only by an open task line: `- [ ]`
// or `- [-]`. A `- [x]` line is completed work — the bug is supposed
// to be fixed — so a TODO string matching only there must report
// stale (CheckRegistry would flag the entry as an orphan).
func TestOpenItemsContain(t *testing.T) {
	const todoSrc = `# TODO

Some prose mentioning "fixed crash" — not a task line.

- [x] **[bug] fixed crash**: parser dies on empty input
- [ ] **[bug] live hang**: select without arms never returns
- [-] **[epic] parent task**: split into children
  - [x] **[bug] child done**: nested and checked off
  - [ ] **[bug] child open**: nested and still live
`
	for _, tc := range []struct {
		name   string
		needle string
		want   bool
	}{
		{"open item matches", "live hang", true},
		{"partially-done parent matches", "parent task", true},
		{"nested open child matches", "child open", true},
		{"checked item is stale", "fixed crash", false},
		{"nested checked item is stale", "child done", false},
		{"absent text is stale", "no such bug", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := openItemsContain(todoSrc, tc.needle); got != tc.want {
				t.Errorf("openItemsContain(%q) = %v, want %v", tc.needle, got, tc.want)
			}
		})
	}
}
