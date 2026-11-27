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
		"foo1": {Name: "foo1"},
		"foo2": {Name: "foo2"},
		"foo3": {Name: "foo3"},
		"foo4": {Name: "foo4"},
		"bar":  {Name: "bar"},
		"foo5": {Name: "foo5", Hidden: "unsupported basic type uint32"},
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
		if c.name == "foo5" {
			t.Errorf("hidden member foo5 was suggested: %+v", memberTypoCandidates(table, "foo"))
		}
	}
}
