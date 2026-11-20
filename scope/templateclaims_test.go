package scope

import (
	"strings"
	"testing"
)

const rowsTemplate = `{"name":"reads","claims":"rows","target":"claim","predicate":"https://kton.dev/v/reads",
	"fields":{"document":{"type":"string","required":true}}}`

func TestTemplateClaims(t *testing.T) {
	for body, want := range map[string]string{
		`{"name":"a"}`:                     ClaimsVersions,
		`{"name":"a","claims":"versions"}`: ClaimsVersions,
		`{"name":"a","claims":"rows"}`:     ClaimsRows,
	} {
		if got, err := TemplateClaims([]byte(body)); err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", body, got, err, want)
		}
	}
	for _, body := range []string{`{"claims":true}`, `{"claims":"row"}`, `{"claims":["rows"]}`, `[]`, `not json`} {
		if _, err := TemplateClaims([]byte(body)); err == nil {
			t.Errorf("%s must be refused", body)
		}
	}
}

func TestBuildVerticle_TemplateClaimsRows(t *testing.T) {
	r, err := buildUI(t, map[string]string{"templates/reads.json": rowsTemplate})
	if err != nil {
		t.Fatalf("a rows template must build: %v", err)
	}
	ts, _ := r.Templates()
	if got, _ := TemplateClaims(ts["reads"]); got != ClaimsRows {
		t.Fatalf("claims read back as %q", got)
	}
	_, err = buildUI(t, map[string]string{"templates/reads.json": strings.Replace(rowsTemplate, `"rows"`, `true`, 1)})
	if err == nil || !strings.Contains(err.Error(), `claims is true; it is "versions" or "rows"`) {
		t.Fatalf("a bad claims value must be refused at build, got %v", err)
	}

	// A template member written past AddTemplate (a raw claim with the package key): Verify reports it.
	h, err := r.P.PutBlob([]byte(`{"name":"bad","claims":"row"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Claim(h, PartOf, member(RoleTemplate, map[string]any{"name": "bad"})); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.P.Verify(), "\n"); !strings.Contains(got, `template "bad": claims is "row"`) {
		t.Fatalf("Verify must report the bad claims value, got %q", got)
	}
}
