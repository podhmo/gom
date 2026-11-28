package resolve

import "testing"

func TestOSADistance(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want int
	}{
		{"map", "map", 0},
		{"mapp", "map", 1},
		{"lenght", "length", 1},
		{"lengt", "length", 1},
		{"zzzz", "length", 6},
		{"ab", "ba", 1},
		{"ca", "abc", 3},
	} {
		if got := osaDistance(tt.a, tt.b); got != tt.want {
			t.Errorf("osaDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestMemberTypoCandidates(t *testing.T) {
	table := map[string]*Symbol{
		"foo1": {Name: "foo1", Kind: KindFunc},
		"foo2": {Name: "foo2", Kind: KindFunc},
		"foo3": {Name: "foo3", Kind: KindFunc},
		"foo4": {Name: "foo4", Kind: KindFunc},
		"bar":  {Name: "bar", Kind: KindFunc},
		"foo0": {Name: "foo0", Kind: KindFunc, Hidden: "unsupported basic type uint32"},
	}
	got := memberTypoCandidates(table, "foo")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(got), got)
	}
	for i, want := range []string{"foo1", "foo2", "foo3"} {
		if got[i].name != want || got[i].distance != 1 {
			t.Errorf("candidate[%d] = %+v, want %s at distance 1", i, got[i], want)
		}
	}
	if got := memberTypoCandidates(table, "zzzz"); len(got) != 0 {
		t.Errorf("zzzz candidates = %+v, want none", got)
	}
	for _, c := range memberTypoCandidates(table, "foo") {
		if c.name == "foo0" {
			t.Errorf("hidden member foo0 was suggested: %+v", memberTypoCandidates(table, "foo"))
		}
	}
}

func TestMemberTypoCandidatesHiddenZeroKind(t *testing.T) {
	table := map[string]*Symbol{
		"Sizeof": {Name: "Sizeof", Hidden: "unsupported unsafe type"},
	}
	if got := memberTypoCandidates(table, "Sizof"); len(got) != 0 {
		t.Errorf("hidden zero-kind candidates = %+v, want none", got)
	}
}

func TestMemberTypoCandidateKindPolicy(t *testing.T) {
	table := map[string]*Symbol{
		"abcdx": {Name: "abcdx", Kind: KindFunc},
		"abcdy": {Name: "abcdy", Kind: KindType},
		"abcdz": {Name: "abcdz", Kind: KindCtor},
	}
	if got := memberTypoCandidatesForUse(table, "abcde", memberUseType, false); len(got) != 1 || got[0].name != "abcdy" {
		t.Errorf("type candidates = %+v, want only abcdy", got)
	}
	if got := memberTypoCandidatesForUse(table, "abcde", memberUseConstructor, false); len(got) != 1 || got[0].name != "abcdz" {
		t.Errorf("constructor candidates = %+v, want only abcdz", got)
	}
	if got := memberTypoCandidatesForUse(table, "abcde", memberUseValue, false); len(got) != 2 || got[0].name != "abcdx" || got[1].name != "abcdz" {
		t.Errorf("value candidates = %+v, want abcdx and abcdz", got)
	}
	got := memberTypoCandidatesForUse(table, "abcde", memberUseValue, true)
	if len(got) != 3 || got[0].name != "abcdx" || got[1].name != "abcdy" || got[2].name != "abcdz" {
		t.Errorf("namespace-hop value candidates = %+v, want abcdx, abcdy, and abcdz", got)
	}
}

func TestValueTypoCandidates(t *testing.T) {
	outer := &scope{terms: map[string]*Symbol{
		"total": {Name: "total", Kind: KindFunc},
		"todo":  {Name: "todo", Kind: KindBuiltin},
		"List":  {Name: "List", Kind: KindModule},
	}}
	inner := &scope{parent: outer, terms: map[string]*Symbol{
		"count":  {Name: "count", Kind: KindVar},
		"hidden": {Name: "hidden", Kind: KindFunc, Hidden: "unsupported"},
		"testfn": {Name: "testfn", Kind: KindFunc, TestOnly: true},
		"assert": {Name: "assert", Kind: KindBuiltin},
	}}

	if got := valueTypoCandidates(inner, "tota", false); len(got) != 1 || got[0].name != "total" {
		t.Fatalf("tota candidates = %+v, want only total", got)
	}
	if got := valueTypoCandidates(inner, "tota", true); len(got) != 1 || got[0].name != "total" {
		t.Fatalf("tota candidates in test = %+v, want only total", got)
	}
	shadowAssert := &scope{parent: outer, terms: map[string]*Symbol{
		"assert": {Name: "assert", Kind: KindVar},
	}}
	if got := valueTypoCandidates(shadowAssert, "asser", false); len(got) != 1 || got[0].name != "assert" {
		t.Errorf("shadowing user assert candidates = %+v, want assert", got)
	}

	for name, want := range map[string]string{
		"hidde":  "hidden",
		"testf":  "testfn",
		"asser":  "assert",
		"lis":    "",
		"to":     "",
		"zzzzzz": "",
	} {
		got := valueTypoCandidates(inner, name, false)
		if want == "" && len(got) != 0 {
			t.Errorf("%s candidates = %+v, want none", name, got)
		}
		if want != "" {
			for _, c := range got {
				if c.name == want {
					t.Errorf("%s candidates include filtered %s: %+v", name, want, got)
				}
			}
		}
	}
	if got := valueTypoCandidates(inner, "testf", true); len(got) != 1 || got[0].name != "testfn" {
		t.Errorf("testf candidates in test = %+v, want testfn", got)
	}

	shadowed := &scope{parent: outer, terms: map[string]*Symbol{
		"total": {Name: "total", Kind: KindModule},
	}}
	if got := valueTypoCandidates(shadowed, "tota", false); len(got) != 0 {
		t.Errorf("shadowed total candidates = %+v, want none", got)
	}
}
