package diag_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// loadSchema parses MessageSchema into a generic document.
func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	raw := diag.MessageSchema()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatal("schema document should end with a newline")
	}
	return doc
}

func defs(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	d, ok := doc["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no $defs: %v", doc)
	}
	return d
}

// TestSchemaMeta pins the envelope: draft, identity, and the version
// the spec requires bumping on incompatible changes (spec section
// 12 item 7).
func TestSchemaMeta(t *testing.T) {
	doc := loadSchema(t)
	if doc["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("$schema = %v", doc["$schema"])
	}
	if doc["$id"] != "gom:schema:message:1" {
		t.Errorf("$id = %v", doc["$id"])
	}
	if doc["version"] != float64(1) {
		t.Errorf("version = %v", doc["version"])
	}
}

// TestSchemaCoversAllKinds: the oneOf must reference a definition
// for every Kind the format defines.
func TestSchemaCoversAllKinds(t *testing.T) {
	doc := loadSchema(t)
	d := defs(t, doc)
	kinds := map[diag.Kind]bool{
		diag.KindDiagnostic:  false,
		diag.KindSummary:     false,
		diag.KindTest:        false,
		diag.KindUnformatted: false,
		diag.KindApplied:     false,
		diag.KindExplanation: false,
	}
	oneOf, ok := doc["oneOf"].([]any)
	if !ok {
		t.Fatalf("schema has no oneOf: %v", doc)
	}
	if len(oneOf) != len(kinds) {
		t.Fatalf("oneOf has %d entries, want %d", len(oneOf), len(kinds))
	}
	for _, alt := range oneOf {
		ref := alt.(map[string]any)["$ref"].(string)
		name := strings.TrimPrefix(ref, "#/$defs/")
		if _, ok := kinds[diag.Kind(name)]; !ok {
			t.Errorf("oneOf references unknown kind %q", ref)
			continue
		}
		kinds[diag.Kind(name)] = true
		def, ok := d[name].(map[string]any)
		if !ok {
			t.Errorf("$defs has no %q", name)
			continue
		}
		// Each message def must pin its kind constant.
		props, _ := def["properties"].(map[string]any)
		kind, _ := props["kind"].(map[string]any)
		if kind["const"] != name {
			t.Errorf("%s: kind const = %v", name, kind["const"])
		}
	}
	for k, seen := range kinds {
		if !seen {
			t.Errorf("kind %q is not in oneOf", k)
		}
	}
}

// TestSchemaRequiredFields: the required sets of the stable message
// shapes stay aligned with the wire encoders.
func TestSchemaRequiredFields(t *testing.T) {
	doc := loadSchema(t)
	d := defs(t, doc)
	required := func(name string) map[string]bool {
		def, ok := d[name].(map[string]any)
		if !ok {
			t.Fatalf("no $defs.%s", name)
		}
		out := map[string]bool{}
		for _, r := range def["required"].([]any) {
			out[r.(string)] = true
		}
		return out
	}
	for _, tt := range []struct {
		def  string
		want []string
	}{
		{"diagnostic", []string{"kind", "code", "severity", "message"}},
		{"summary", []string{"kind", "ok", "errors", "warnings", "files", "outputs"}},
		{"applied", []string{"kind", "code", "title", "edits"}},
		{"unformatted", []string{"kind", "file", "diff"}},
		{"explanation", []string{"kind", "code", "severity", "title", "phase", "explanation"}},
		{"pos", []string{"line", "col", "offset"}},
		{"span", []string{"file", "start", "end"}},
		{"edit", []string{"file", "start", "end", "text"}},
		{"fix", []string{"title", "applicability", "edits"}},
	} {
		got := required(tt.def)
		if len(got) != len(tt.want) {
			t.Errorf("%s.required = %v, want %v", tt.def, got, tt.want)
			continue
		}
		for _, w := range tt.want {
			if !got[w] {
				t.Errorf("%s.required missing %q", tt.def, w)
			}
		}
	}
}

// TestSchemaCodePattern: the code pattern in the schema must agree
// with diag.WellFormed — every registered code must match, and the
// classic malformed shapes must not.
func TestSchemaCodePattern(t *testing.T) {
	doc := loadSchema(t)
	d := defs(t, doc)
	pat := d["diagnostic"].(map[string]any)["properties"].(map[string]any)["code"].(map[string]any)["pattern"].(string)
	re, err := regexp.Compile("^(?:" + pat + ")$")
	if err != nil {
		t.Fatalf("code pattern does not compile: %v", err)
	}
	for _, info := range diag.CodeTable() {
		if !re.MatchString(string(info.Code)) {
			t.Errorf("registered code %s does not match schema pattern %q", info.Code, pat)
		}
		if !diag.WellFormed(info.Code) {
			t.Errorf("registered code %s fails WellFormed", info.Code)
		}
	}
	for _, bad := range []string{"e0301", "X0301", "E301", "E030", "E30101"} {
		if re.MatchString(bad) {
			t.Errorf("schema pattern accepts malformed code %q", bad)
		}
	}
}

// TestSchemaRefsResolve: every $ref target inside the schema must
// name a $defs entry.
func TestSchemaRefsResolve(t *testing.T) {
	doc := loadSchema(t)
	d := defs(t, doc)
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				name := strings.TrimPrefix(ref, "#/$defs/")
				if name == ref {
					t.Errorf("non-local $ref %q", ref)
				} else if _, ok := d[name]; !ok {
					t.Errorf("$ref to missing def %q", ref)
				}
			}
			for _, x := range v {
				walk(x)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(doc)
}
