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

// A template may say which template it answers (a job: the request and its answers, MH 21.11.).
func TestTemplateAnswers(t *testing.T) {
	requested := `{"name":"capture-requested","target":"foton","predicate":"https://kton.dev/v/capture-requested","fields":{"what":{"type":"string"}}}`
	result := `{"name":"capture-result","answers":"capture-requested","target":"claim","predicate":"https://kton.dev/v/capture-result","fields":{"outcome":{"type":"string"}}}`
	r, err := buildUI(t, map[string]string{"templates/capture-requested.json": requested, "templates/capture-result.json": result})
	if err != nil {
		t.Fatalf("an answer template must build: %v", err)
	}
	ts, _ := r.Templates()
	if a, _ := TemplateAnswers(ts["capture-result"]); a != "capture-requested" {
		t.Fatalf("answers read back as %q", a)
	}
	if a, err := TemplateAnswers(ts["capture-requested"]); a != "" || err != nil {
		t.Fatalf("a template without answers: %q %v", a, err)
	}
	for _, c := range []struct {
		name string
		edit map[string]string
		want string
	}{
		{"not in the package", map[string]string{"templates/capture-result.json": result}, `answers "capture-requested", which is not a template of the package`},
		{"itself", map[string]string{"templates/capture-result.json": strings.Replace(result, `"answers":"capture-requested"`, `"answers":"capture-result"`, 1)}, "answers itself"},
		{"not a name", map[string]string{"templates/capture-result.json": strings.Replace(result, `"answers":"capture-requested"`, `"answers":["capture-requested"]`, 1)}, "answers is"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildUI(t, c.edit)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want a refusal with %q, got %v", c.want, err)
			}
		})
	}
	// Written past the builder: Verify reports it.
	h, err := r.P.PutBlob([]byte(`{"name":"late","answers":"nothing"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Claim(h, PartOf, member(RoleTemplate, map[string]any{"name": "late"})); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.P.Verify(), "\n"); !strings.Contains(got, `template "late" answers "nothing"`) {
		t.Fatalf("Verify must report it, got %q", got)
	}
}

// The job monitor's block needs no format change: a block with typed options, placed by a use.
func TestBuildVerticle_AJobsBlock(t *testing.T) {
	src := uiSrc(t, nil)
	main := mainJSON(src, t)
	intro := `{"use":"text","file":"content/text/intro.md"}`
	_, err := buildUI(t, map[string]string{
		"ui/blocks/jobs.json": `{"viewer":{"viewer":"workbench/jobs","at":"` + strings.Repeat("a", 40) + `","tree":"sha256:` + strings.Repeat("d", 64) + `"},
			"options":{"root":"literal","depth":"integer","kinds":{"set":"literal"}}}`,
		"ui/compositions/main.json": strings.Replace(main, intro, intro+`,{"use":"jobs","options":{"root":"dryrun1.triage/decision","depth":2,"kinds":["ray-run","request"]}}`, 1),
	})
	if err != nil {
		t.Fatalf("a jobs block must build: %v", err)
	}
}
