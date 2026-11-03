package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// verticleSrc writes a source folder in the shape of the render verticle: one step that renders a
// source with a template (two file holes, one parameter), a template for the "rendered" claim, a
// query over renders, a control that says it, a viewer, and MCP tools bound to the ray and the query.
func verticleSrc(t *testing.T, edit map[string]string) string {
	t.Helper()
	src := t.TempDir()
	files := map[string]string{
		"templates/rendered.json": `{"name":"rendered","target":"either","predicate":"https://kton.dev/v/rendered",
			"fields":{"document":{"type":"string","required":true}}}`,
		"queries/renders.rq":     "SELECT ?run WHERE { ?run <https://kton.dev/v/source> $SOURCE }\n",
		"steps/render/render.sh": "pandoc \"$1\" --reference-doc \"$2\" -t \"$3\" -o out\n",
		"testdata/source.md":     "# Title\n",
		"testdata/template.docx": "not really a docx",
		"ray.json": `{"name":"render","steps":[{"id":"render","label":"Render",
			"protocol":{"command":{"args":"img@sha256:00\n<command-file>\n<format>"}},
			"params":{"format":"hole:format"},
			"inputs":[{"slot":"render.sh","path":"steps/render/render.sh","variable":"command-file"},
				{"slot":"source.md","from":"hole:source"},{"slot":"template.docx","from":"hole:template"}],
			"outputs":["out"]}],
			"holes":[{"name":"source","step":"render","slot":"source.md","type":"file","required":true,"default":"testdata/source.md"},
				{"name":"template","step":"render","slot":"template.docx","type":"file","required":true,"default":"testdata/template.docx"},
				{"name":"format","step":"render","param":"format","type":"param","required":true,"default":"docx"}]}`,
		"controls/note.json": `{"title":"Say a document was rendered","say":{"template":"rendered","templates":["rendered"]},
			"signer":{"from":"input","field":"lane"},"input":{"type":"object","required":["document"],
			"properties":{"document":{"type":"string"}}}}`,
		"mcp/render.json": `{"description":"Render a source with a template","ray":"render",
			"bind":{"source":"source","template":"template","format":"format"},
			"input":{"type":"object","required":["source","template"],"properties":{"source":{"type":"string"},
			"template":{"type":"string"},"format":{"type":"string","enum":["docx","pdf"]}}}}`,
		"mcp/renders.json": `{"description":"Runs that rendered a source","query":"renders","bind":{"SOURCE":"source"},
			"input":{"type":"object","properties":{"source":{"type":"string"}}}}`,
		"viewers/preview.json": `{"viewer":"kton-web/render","at":"0000000","tree":"sha256:` + strings.Repeat("a", 64) + `",
			"inputs":{"runs":"renders"},"intents":{"note":"note"}}`,
	}
	for k, v := range edit {
		if v == "" {
			delete(files, k)
		} else {
			files[k] = v
		}
	}
	for rel, body := range files {
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return src
}

func TestBuildVerticle_BuildsSealsReadsBackAndInstalls(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "render")
	p, r, err := BuildVerticle(dir, "render", "v1", verticleSrc(t, nil), newKey(t), Options{Clock: fixedClock()})
	if err != nil {
		t.Fatal(err)
	}
	if probs := p.Verify(); len(probs) > 0 {
		t.Fatalf("Verify: %v", probs)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _, err := again.Sealed()
	if err != nil || sealed.ID != r.ID {
		t.Fatalf("the built revision must be the sealed one: %v", err)
	}
	if prof, _ := sealed.Profile(); prof != ProfileVerticle {
		t.Fatalf("profile %q", prof)
	}
	ts, _ := sealed.Templates()
	qs, _ := sealed.Queries()
	if len(ts) != 1 || len(qs) != 1 {
		t.Fatalf("templates %v, queries %v", Names(ts), Names(qs))
	}
	vs, err := sealed.Viewers()
	if err != nil || vs["preview"].Viewer != "kton-web/render" || vs["preview"].Inputs["runs"] != "renders" {
		t.Fatalf("viewer: %+v (%v)", vs, err)
	}
	cs, err := sealed.Controls()
	if err != nil || cs["note"].Say.Template != "rendered" {
		t.Fatalf("control: %+v (%v)", cs, err)
	}
	ms, err := sealed.MCPTools()
	if err != nil || ms["render"].Ray != "render" || ms["renders"].Query != "renders" {
		t.Fatalf("mcp: %+v (%v)", ms, err)
	}
	ray, err := sealed.Ray()
	if err != nil || ray.Name != "render" || ray.Hole("format").Default != "docx" {
		t.Fatalf("ray: %+v (%v)", ray, err)
	}
	user := t.TempDir()
	in, err := again.Install("v1", filepath.Join(user, "plankton"), filepath.Join(user, "nekton"))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if in.Ray == "" || in.Fotons == 0 || in.Claims == 0 {
		t.Fatalf("installed nothing: %+v", in)
	}
}

// Each rule refuses the build with a message naming what is wrong.
func TestBuildVerticle_RefusesBrokenReferences(t *testing.T) {
	cases := map[string]struct {
		edit map[string]string
		want string
	}{
		"viewer input to a missing query": {map[string]string{
			"viewers/preview.json": `{"viewer":"kton-web/render","inputs":{"runs":"nope"}}`}, `"nope"`},
		"viewer intent to a missing control": {map[string]string{
			"viewers/preview.json": `{"viewer":"kton-web/render","intents":{"note":"nope"}}`}, `"nope"`},
		"viewer without a viewer": {map[string]string{
			"viewers/preview.json": `{"inputs":{"runs":"renders"}}`}, "viewer"},
		"viewer tree that is not a content hash": {map[string]string{
			"viewers/preview.json": `{"viewer":"kton-web/render","tree":"d80c07f"}`}, "tree"},
		"control saying a template from elsewhere": {map[string]string{
			"controls/note.json": `{"say":{"template":"requests"},"input":{"type":"object"}}`}, `"requests"`},
		"mcp bound to two things": {map[string]string{
			"mcp/renders.json": `{"query":"renders","control":"note","input":{"type":"object"}}`}, "exactly one"},
		"mcp bind to a variable the query lacks": {map[string]string{
			"mcp/renders.json": `{"query":"renders","bind":{"LANE":"lane"},"input":{"type":"object"}}`}, "$LANE"},
		"mcp bind to a hole the ray lacks": {map[string]string{
			"mcp/render.json": `{"ray":"render","bind":{"nope":"x"},"input":{"type":"object"}}`}, `"nope"`},
		"mcp input that is not an object schema": {map[string]string{
			"mcp/renders.json": `{"query":"renders","input":{"type":"string"}}`}, "input"},
		"unknown field (a typo)": {map[string]string{
			"controls/note.json": `{"say":{"template":"rendered"},"inputs":{"type":"object"}}`}, "inputs"},
		"mcp bound to a ray the package lacks": {map[string]string{"ray.json": ""}, `"render"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := BuildVerticle(filepath.Join(t.TempDir(), "render"), "render", "v1",
				verticleSrc(t, c.edit), newKey(t), Options{Clock: fixedClock()})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an error mentioning %s, got %v", c.want, err)
			}
		})
	}
}
