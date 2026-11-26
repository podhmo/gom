package check

import "testing"

// Expected-type labels attach only to the conformance owned by the
// annotation: the let/return annotation is labelled on a direct mismatch,
// while unrelated nested mismatches do not inherit it.
func TestExpectedTypeLabels(t *testing.T) {
	ress := resolveFiles(t, map[string]string{
		"a.gom": "package m\n\nlet x : Int = \"s\"\n\nfn f () -> Int = \"s\"\n",
	})
	_, ds := Files(ress, nil)
	if len(ds) != 2 {
		t.Fatalf("diagnostics = %v, want two E0401", ds)
	}
	for i, d := range ds {
		if d.Code != "E0401" {
			t.Fatalf("diagnostics[%d].Code = %s, want E0401", i, d.Code)
		}
		if len(d.Labels) != 1 {
			t.Fatalf("diagnostics[%d].Labels = %+v, want one label", i, d.Labels)
		}
		if got := d.Labels[0].Message; got != "expected type declared here" {
			t.Errorf("diagnostics[%d].Labels[0].Message = %q", i, got)
		}
	}
	if got := ds[0].Labels[0].Span; got.Start != 19 || got.End != 22 {
		t.Errorf("let label span = %+v, want 19-22", got)
	}
	if got := ds[1].Labels[0].Span; got.Start != 41 || got.End != 44 {
		t.Errorf("return label span = %+v, want 41-44", got)
	}
}

func TestExpectedTypeLabelDoesNotLeakIntoNestedDiagnostics(t *testing.T) {
	ress := resolveFiles(t, map[string]string{
		"a.gom": "package m\n\nlet x : Int = {\n  \"a\" + \"b\"\n  1\n}\n",
	})
	_, ds := Files(ress, nil)
	if len(ds) == 0 {
		t.Fatal("expected nested type diagnostics")
	}
	for i, d := range ds {
		if len(d.Labels) != 0 {
			t.Errorf("diagnostics[%d] unexpectedly carries labels: %+v", i, d.Labels)
		}
	}
}
