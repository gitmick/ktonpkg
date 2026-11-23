package scope

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateState(t *testing.T) {
	c, err := decodeComposition([]byte(`{"kind":"view",
		"vars":{"picked":{"type":"iri"},"roles":{"type":{"set":"literal"},"default":[]},"n":{"type":"integer","default":3},"node":{"type":"hash"}},
		"layout":{"type":"tabs","active":"a"},
		"parts":[{"id":"a","heading":"A"},{"id":"b","heading":"B","layout":{"type":"split","ratio":0.5},"parts":[{"id":"l"},{"id":"r"}]},
		         {"heading":"nested","composition":"detail"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	hash := `"sha256:` + strings.Repeat("a", 64) + `"`
	in := State{
		Vars: map[string]json.RawMessage{
			"picked": json.RawMessage(`"https://kton.dev/x"`),
			"roles":  json.RawMessage(`["Q",1]`),     // not a set of literals: default
			"n":      json.RawMessage(`"three"`),     // not an integer: default 3
			"node":   json.RawMessage(`"sha256:zz"`), // not a hash, no default: unset
			"ghost":  json.RawMessage(`1`),           // not declared: dropped
		},
		Layout: map[string]LayoutState{
			"":     {Active: "b"},
			"b":    {Ratio: 1.5},
			"a":    {Active: "x"},
			"nope": {Ratio: 0.3},
		},
		Nested: map[string]State{"#3": {Vars: map[string]json.RawMessage{"x": json.RawMessage(hash)}}, "a": {}},
	}
	out, errs := ValidateState(c, in)
	want := map[string]string{"picked": `"https://kton.dev/x"`, "roles": `[]`, "n": `3`}
	if len(out.Vars) != len(want) {
		t.Fatalf("vars: %v", out.Vars)
	}
	for k, v := range want {
		if string(out.Vars[k]) != v {
			t.Errorf("var %s = %s, want %s", k, out.Vars[k], v)
		}
	}
	if out.Layout[""].Active != "b" || len(out.Layout) != 1 {
		t.Errorf("layout: %+v", out.Layout)
	}
	if _, ok := out.Nested["#3"]; !ok || len(out.Nested) != 1 {
		t.Errorf("nested: %+v", out.Nested)
	}
	got := map[string]bool{}
	for _, e := range errs {
		got[e.Key] = true
	}
	for _, k := range []string{"var.roles", "var.n", "var.node", "var.ghost", "layout.b.ratio", "layout.a", "layout.nope", "nested.a"} {
		if !got[k] {
			t.Errorf("no error for %s; got %v", k, errs)
		}
	}
	if len(errs) != 8 {
		t.Errorf("want 8 errors, got %d: %v", len(errs), errs)
	}

	// A report takes no state: every var holds its default.
	r, _ := decodeComposition([]byte(`{"kind":"report","vars":{"n":{"type":"integer","default":3}},"parts":[]}`))
	out, errs = ValidateState(r, State{Vars: map[string]json.RawMessage{"n": json.RawMessage(`5`)}})
	if string(out.Vars["n"]) != "3" || len(errs) != 1 || !strings.Contains(errs[0].Reason, "a report does not take state") {
		t.Fatalf("report: %v %v", out.Vars, errs)
	}
	// No state at all: the defaults.
	out, errs = ValidateState(c, State{})
	if string(out.Vars["n"]) != "3" || len(errs) != 0 {
		t.Fatalf("empty state: %v %v", out.Vars, errs)
	}
}
