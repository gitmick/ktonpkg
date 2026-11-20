package scope

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg"
)

// uiSrc is the render-shaped verticle of verticleSrc plus a ui part: an alias file, a decision and a
// record control, a slot entry, two blocks, content, and two compositions, one nesting the other
// with a param, a var wired across, an act and a refine. edit replaces or (with "") removes files.
func uiSrc(t *testing.T, edit map[string]string) string {
	t.Helper()
	files := map[string]string{
		"templates/aliases.json": `{"schema":"https://kton.dev/aliases/v0","prefixes":{"nk":"https://kton.dev/nk#"}}`,
		"templates/reads.json": `{"name":"reads","target":"claim","predicate":"https://kton.dev/v/reads",
			"fields":{"document":{"type":"string","required":true}}}`,
		"controls/confirm.json": `{"kind":"decision","say":{"template":"reads"}}`,
		"controls/log.json":     `{"kind":"record","say":{"template":"reads"}}`,
		"queries/items.rq":      "SELECT ?item ?label WHERE { ?item <https://kton.dev/v/in> $doc ; <https://kton.dev/v/status> $status }\n",
		"queries/card.rq":       "SELECT ?p ?o WHERE { $item ?p ?o }\n",
		"queries/count.rq":      "SELECT (COUNT(*) AS ?n) WHERE { ?s ?p ?o }\n",
		"content/text/intro.md": "Intro.\n",
		"ui/review.json": `{"slot":"review-view","label":"Review","when":{"kind":"claim","type":"nk:document"},
			"viewer":"preview","control":"confirm","query":"card","inputs":{"item":"subject.iri"}}`,
		"ui/tile.json": `{"slot":"home-tile","label":"Items","query":"count","order":1}`,
		"ui/area.json": `{"slot":"activity","label":"Area","composition":"main"}`,
		"ui/blocks/table.json": `{"block":"table","viewer":{"viewer":"table","at":"kton-web@1","tree":"sha256:` + strings.Repeat("b", 64) + `"},
			"requires":["query@1"],"optional":["open@1","sign@1"],"accepts":[{"kind":"claim"}],
			"options":{"columns":{"set":"literal"},"mask":{"set":"literal"},"focus":"iri"},"emits":{"select":"iri","focus":"hash"}}`,
		"ui/blocks/text.json": `{"viewer":"preview"}`,
		"ui/compositions/main.json": `{"kind":"view","subject":{"kind":"claim","type":"nk:document"},
			"vars":{"picked":{"type":"iri"},"roles":{"type":{"set":"literal"},"default":[]},"node":{"type":"hash"}},
			"parts":[
			 {"id":"intro","heading":"Intro","blocks":[{"use":"text","file":"content/text/intro.md"}]},
			 {"id":"list","heading":"List","blocks":[{"id":"items","use":"table","query":"items",
			   "params":{"doc":"subject.iri"},"refine":{"status":{"values":["open","done"]}},
			   "options":{"columns":["Item","Label"],"mask":"var.roles","focus":"var.node"},"emits":{"select":"picked","focus":"node"},
			   "acts":[{"intent":"sign","label":"Confirm","control":"confirm","fields":{"document":"subject.iri"}},
			           {"intent":"record","control":"log","fields":{"document":"subject.iri"}}]}]},
			 {"parts":[{"id":"deep","heading":"Deeper","blocks":[{"use":"table","query":"card","params":{"item":"var.picked"},"empty":"show"}]}]},
			 {"id":"detail","heading":"Detail","composition":"detail","params":{"subject":"var.picked","item":"var.picked"},"emits":{"chose":"picked"}}],
			"eachPart":[{"use":"table","query":"count","empty":"omit"}]}`,
		"ui/compositions/detail.json": `{"kind":"view","params":["item"],"vars":{"chosen":{"type":"iri"}},"emits":{"chose":"chosen"},
			"parts":[{"id":"card","heading":"Card","blocks":[{"use":"table","query":"card","params":{"item":"item"},"emits":{"select":"chosen"}}]}]}`,
	}
	for k, v := range edit {
		files[k] = v
	}
	return verticleSrc(t, files)
}

func buildUI(t *testing.T, edit map[string]string) (*Revision, error) {
	t.Helper()
	_, r, err := BuildVerticle(filepath.Join(t.TempDir(), "render"), "render", "v1", uiSrc(t, edit), newKey(t), Options{Clock: fixedClock()})
	return r, err
}

func TestBuildVerticle_CarriesTheUIPart(t *testing.T) {
	r, err := buildUI(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	ui, err := r.UIEntries()
	if err != nil || len(ui) != 3 || ui["review"].Control != "confirm" {
		t.Fatalf("ui entries: %v %v", ui, err)
	}
	bs, _ := r.Blocks()
	cs, _ := r.Compositions()
	content, _ := r.Content()
	if len(bs) != 2 || len(cs) != 2 || string(content["content/text/intro.md"]) != "Intro.\n" {
		t.Fatalf("blocks %d, compositions %d, content %v", len(bs), len(cs), content)
	}
	ctl, _ := r.Controls()
	if ctl["confirm"].EffectiveKind() != ControlDecision || ctl["log"].EffectiveKind() != ControlRecord {
		t.Fatalf("control kinds: %+v", ctl)
	}
	if probs := r.P.Verify(); len(probs) > 0 {
		t.Fatalf("Verify: %v", probs)
	}
}

func TestControl_WithoutKindIsADecision(t *testing.T) {
	if (Control{}).EffectiveKind() != ControlDecision {
		t.Fatal("a control that says no kind must be a decision: the safe side")
	}
	_, err := buildUI(t, map[string]string{"controls/confirm.json": `{"kind":"vote","say":{"template":"reads"}}`})
	if err == nil || !strings.Contains(err.Error(), `kind "vote" is not decision or record`) {
		t.Fatalf("want the kind refused, got %v", err)
	}
}

// Each edit breaks one rule; the build must refuse it, naming the rule.
func TestBuildVerticle_RefusesABrokenUIPart(t *testing.T) {
	main := func(old, new string) map[string]string {
		src := uiSrc(t, nil)
		b, err := os.ReadFile(filepath.Join(src, "ui/compositions/main.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), old) {
			t.Fatalf("main.json has no %q", old)
		}
		return map[string]string{"ui/compositions/main.json": strings.Replace(string(b), old, new, 1)}
	}
	cases := []struct {
		name string
		edit map[string]string
		want string
	}{
		{"unknown slot", map[string]string{"ui/tile.json": `{"slot":"sidebar","label":"x","query":"count"}`}, `slot "sidebar" is not one of`},
		{"slot without its reference", map[string]string{"ui/tile.json": `{"slot":"run-action","label":"x"}`}, "slot run-action needs a ray"},
		{"record-view without a type", map[string]string{"ui/tile.json": `{"slot":"record-view","label":"x","viewer":"preview","when":{"kind":"claim"}}`}, "when with a type"},
		{"input outside the closed set", map[string]string{"ui/tile.json": `{"slot":"home-tile","label":"x","query":"card","inputs":{"item":"env.HOME"}}`}, `"env.HOME", which is not a shell value`},
		{"unbound $VAR in a slot", map[string]string{"ui/tile.json": `{"slot":"home-tile","label":"x","query":"card"}`}, "uses $item, which inputs does not bind"},
		{"unknown CURIE prefix", map[string]string{"ui/tile.json": `{"slot":"review-view","label":"x","viewer":"preview","when":{"kind":"claim","type":"qq:thing"}}`}, `the prefix "qq" is not in the package's aliases.json`},
		{"bad when kind", map[string]string{"ui/tile.json": `{"slot":"review-view","label":"x","viewer":"preview","when":{"kind":"person"}}`}, `kind "person" is not one of claim, foton, scope`},
		{"unknown field", map[string]string{"ui/tile.json": `{"slot":"home-tile","label":"x","query":"count","name":"tile"}`}, `unknown field "name"`},
		{"act required", map[string]string{"ui/blocks/text.json": `{"viewer":"preview","requires":["sign@1"]}`}, "an act never hides a block"},
		{"unknown capability", map[string]string{"ui/blocks/text.json": `{"viewer":"preview","requires":["network@1"]}`}, `"network@1", which is not a capability`},
		{"block emits an act", map[string]string{"ui/blocks/text.json": `{"viewer":"preview","emits":{"record":"iri"}}`}, "emits record, an act"},
		{"inline viewer without a tree", map[string]string{"ui/blocks/text.json": `{"viewer":{"viewer":"t","at":"x"}}`}, "is not a content hash"},
		{"missing query", main(`"query":"items"`, `"query":"nosuch"`), `the query "nosuch" is not in the package`},
		{"param not a $VAR", main(`"params":{"doc":"subject.iri"}`, `"params":{"doc":"subject.iri","other":"subject.iri"}`), "param other is not a $VAR of the query"},
		{"unbound $VAR in a use", main(`"refine":{"status":{"values":["open","done"]}},`, ``), "the query uses $status, which neither params nor refine binds"},
		{"source not in the set", main(`"params":{"doc":"subject.iri"}`, `"params":{"doc":"env.HOME"}`), `"env.HOME" is not a source`},
		{"undeclared var", main(`"params":{"item":"var.picked"}`, `"params":{"item":"var.nobody"}`), "reads a var the composition does not declare"},
		{"set into a query", main(`"params":{"item":"var.picked"}`, `"params":{"item":"var.roles"}`), "a set does not go into a query"},
		{"refine also in params", main(`"params":{"doc":"subject.iri"}`, `"params":{"doc":"subject.iri","status":"subject.iri"}`), "refine status is also in params"},
		{"typed refine without default", main(`{"values":["open","done"]}`, `{"type":"literal"}`), "a typed refine needs a default"},
		{"option not of the block", main(`"columns":["Item","Label"]`, `"colour":"red"`), "option colour is not an option of the block"},
		{"option of the wrong type", main(`"columns":["Item","Label"]`, `"columns":"Item"`), "option columns wants a set of literal"},
		{"set var into a scalar option", main(`"focus":"var.node"`, `"focus":"var.roles"`), "option focus wants a iri, var.roles is a set of literal"},
		{"wired sign", main(`"emits":{"select":"picked","focus":"node"}`, `"emits":{"sign":"picked"}`), "wires sign, an act"},
		{"event the block does not emit", main(`"emits":{"select":"picked","focus":"node"}`, `"emits":{"hover":"picked"}`), "the block does not emit hover"},
		{"type mismatch", main(`"emits":{"select":"picked","focus":"node"}`, `"emits":{"select":"node"}`), "emits select (iri) into the var node (hash)"},
		{"sign act on a record control", main(`"intent":"sign","label":"Confirm","control":"confirm"`, `"intent":"sign","label":"Confirm","control":"log"`), "a sign act needs a decision control"},
		{"act field not in the template", main(`{"intent":"record","control":"log","fields":{"document":"subject.iri"}}`, `{"intent":"record","control":"log","fields":{"doc":"subject.iri"}}`), "field doc is not a field"},
		{"file outside the package", main(`"file":"content/text/intro.md"`, `"file":"../../etc/passwd"`), `the file "../../etc/passwd" is not content of the package`},
		{"unnamed part with a heading", main(`{"parts":[{"id":"deep"`, `{"heading":"H","parts":[{"id":"deep"`), "has no id"},
		{"duplicate part id", main(`{"id":"intro"`, `{"id":"list"`), `the id "list" is used twice`},
		{"unbound child param", main(`"params":{"subject":"var.picked","item":"var.picked"}`, `"params":{"subject":"var.picked"}`), "needs the param item, which is not bound"},
		{"subject from a set", main(`"params":{"subject":"var.picked","item":"var.picked"}`, `"params":{"subject":"var.roles","item":"var.picked"}`), "a subject comes from an iri or hash var"},
		{"child does not emit", main(`"emits":{"chose":"picked"}`, `"emits":{"gone":"picked"}`), `the composition "detail" does not emit gone`},
		{"subject as a param", map[string]string{"ui/compositions/detail.json": `{"kind":"view","params":["subject"],"parts":[]}`}, `param "subject" is a reserved name`},
		{"placeholder without an input", main(`{"id":"intro","heading":"Intro","blocks":[{"use":"text","file":"content/text/intro.md"}]}`, `{"from":"chapters"}`), `from "chapters" is not a declared input`},
		{"report var without default", main(`"kind":"view"`, `"kind":"report"`), "var picked has no default: in a report"},
		{"render ray not here", main(`"kind":"view"`, `"kind":"view","render":{"ray":"other"}`), `render: the ray "other" is not in the package`},
		{"cycle", map[string]string{"ui/compositions/detail.json": `{"kind":"view","params":["item"],"emits":{"chose":"chosen"},"vars":{"chosen":{"type":"iri"}},
			"parts":[{"id":"back","composition":"main"}]}`}, "compositions nest in a cycle"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildUI(t, c.edit)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want a refusal with %q, got %v", c.want, err)
			}
		})
	}
}

func TestBuildVerticle_RefusesNestingDeeperThanEight(t *testing.T) {
	edit := map[string]string{}
	for i := 0; i < 10; i++ {
		var parts string
		if i < 9 {
			parts = `{"id":"n","composition":"c` + string(rune('0'+i+1)) + `"}`
		}
		edit["ui/compositions/c"+string(rune('0'+i))+".json"] = `{"kind":"view","parts":[` + parts + `]}`
	}
	_, err := buildUI(t, edit)
	if err == nil || !strings.Contains(err.Error(), "nests 10 deep; at most 8") {
		t.Fatalf("want depth refused, got %v", err)
	}
}

// The hostile packages of userinterface (qm-workbench/hostile-packages, layer "ktonpkg build"),
// adjusted to the format: good/ builds; every case is refused for its reason.
func TestBuildVerticle_HostilePackages(t *testing.T) {
	var cases []struct{ Dir, Expect, Reason string }
	b, err := os.ReadFile("testdata/hostile/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cases); err != nil || len(cases) == 0 {
		t.Fatalf("cases: %v", err)
	}
	want := map[string]string{
		"good":                          "",
		"case-malformed-json":           "unexpected EOF",
		"case-missing-viewer":           `the viewer "chart" is not in the package`,
		"case-missing-query":            `the query "nosuch" is not in the package`,
		"case-missing-control":          `the control "reject" is not in the package`,
		"case-path-outside":             "is not content of the package",
		"case-input-outside-closed-set": "is not a shell value",
		"case-wired-sign":               "wires sign, an act",
		"case-block-emits-act":          "emits record, an act",
		"case-type-mismatch":            "emits select (iri) into the var selected (literal)",
		"case-set-into-query":           "a set does not go into a query",
		"case-unknown-capability":       `"network@1", which is not a capability`,
		"case-duplicate-name":           `unknown field "name"`,
		"case-composition-cycle":        "compositions nest in a cycle",
		"case-depth-over-8":             "at most 8",
		"case-report-var-no-default":    "has no default",
		"case-subject-as-param":         `param "subject" is a reserved name`,
	}
	for _, c := range cases {
		t.Run(c.Dir, func(t *testing.T) {
			w, ok := want[c.Dir]
			if !ok {
				t.Fatalf("no expectation for %s (%s)", c.Dir, c.Reason)
			}
			_, _, err := BuildVerticle(filepath.Join(t.TempDir(), "p"), "p", "v1", filepath.Join("testdata/hostile", c.Dir), newKey(t), Options{Clock: fixedClock()})
			switch {
			case c.Expect == "accept" && err != nil:
				t.Fatalf("good must build: %v", err)
			case c.Expect != "accept" && (err == nil || !strings.Contains(err.Error(), w)):
				t.Fatalf("%s: want a refusal with %q, got %v", c.Reason, w, err)
			}
		})
	}
}

func TestTreeIDOf_MatchesThePublishedVector(t *testing.T) {
	id, err := ktonpkg.TreeIDOf(map[string][]byte{
		"viewer.json":  []byte(`{"name":"demo","module":"index.js"}` + "\n"),
		"index.js":     []byte("export default function(ctx){}\n"),
		"assets/a.css": []byte("body{}\n"),
	})
	if err != nil || id != "sha256:8a70967c00267cad43505a7b0b72c8c70311cbd8a3dd44d959a948e1778ebe1b" {
		t.Fatalf("TreeIDOf = %s, %v (the vector given to kton-web, round 4)", id, err)
	}
}

// A viewer tree the package carries lives at viewers/<viewer>/; its pin names this revision and the
// tree's id, and the block on it emits and takes only what its viewer.json declares.
func TestBuildVerticle_ChecksACarriedViewer(t *testing.T) {
	tree := map[string][]byte{
		"viewer.json": []byte(`{"viewer":"table","module":"index.js","emits":{"select":{"from":"intent.select","type":"iri"},` +
			`"focus":{"from":"state.focus","type":"hash"}},"accepts":{"columns":{"set":"literal"},"mask":{"set":"literal"},"focus":"iri"}}`),
		"index.js": []byte("export function mount(el){}\n"),
	}
	id, err := ktonpkg.TreeIDOf(tree)
	if err != nil {
		t.Fatal(err)
	}
	carried := func(at, treeID, viewerJSON string) map[string]string {
		e := map[string]string{
			"viewers/table/index.js":    string(tree["index.js"]),
			"viewers/table/viewer.json": string(tree["viewer.json"]),
			"ui/blocks/table.json": `{"viewer":{"viewer":"table","at":"` + at + `","tree":"` + treeID + `"},"requires":["query@1"],
				"options":{"columns":{"set":"literal"},"mask":{"set":"literal"},"focus":"iri"},"emits":{"select":"iri","focus":"hash"}}`,
		}
		if viewerJSON != "" {
			e["viewers/table/viewer.json"] = viewerJSON
		}
		return e
	}
	r, err := buildUI(t, carried("render@v1", id, ""))
	if err != nil {
		t.Fatalf("a carried viewer pinned right must build: %v", err)
	}
	trees, err := r.ViewerTrees()
	if err != nil || len(trees["table"]) != 2 {
		t.Fatalf("ViewerTrees: %v %v", trees, err)
	}
	if got, _ := ktonpkg.TreeIDOf(trees["table"]); got != id {
		t.Fatalf("the carried tree reads back as %s, pinned %s", got, id)
	}
	other := `{"viewer":"table","module":"index.js","emits":{"select":{"type":"iri"}},"accepts":{"columns":{"set":"literal"},"mask":{"set":"literal"},"focus":"iri"}}`
	otherID, _ := ktonpkg.TreeIDOf(map[string][]byte{"viewer.json": []byte(other), "index.js": tree["index.js"]})
	for _, c := range []struct {
		name string
		edit map[string]string
		want string
	}{
		{"tree changed", carried("render@v1", "sha256:"+strings.Repeat("c", 64), ""), "its files have the tree " + id},
		{"at not this revision", carried("render@v0", id, ""), `a carried viewer's at is "render@v1"`},
		{"event not reported", carried("render@v1", otherID, other), `emits focus, which the viewer "table" does not report`},
		{"tree not pinned", map[string]string{"viewers/loose/viewer.json": `{"viewer":"loose"}`}, `carries the viewer tree "loose", but no viewer or block pins it`},
		{"viewer.json with name, not viewer", map[string]string{"viewers/loose/viewer.json": `{"name":"loose"}`}, `viewer.json says viewer ""; it must name its folder`},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildUI(t, c.edit)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want a refusal with %q, got %v", c.want, err)
			}
		})
	}
}

// The C3.2 area: scopes, queries with params from the shell's values, actions with a meaning,
// documents, governing, and findings by rule id.
func TestBuildVerticle_ChecksTheArea(t *testing.T) {
	area := func(a string) map[string]string {
		return map[string]string{
			"queries/record.rq":    "SELECT ?r WHERE { ?r <https://kton.dev/v/person> $person }\n",
			"queries/raci.rq":      "SELECT ?a ?r WHERE { ?a <https://kton.dev/v/r> ?r }\n",
			"queries/approvals.rq": "SELECT ?claim ?record ?outcome ?signer WHERE { ?claim <https://kton.dev/v/about> $record ; <https://kton.dev/v/outcome> ?outcome ; <https://kton.dev/v/signer> ?signer }\n",
			"queries/history.rq":   "SELECT * WHERE { ?claim <https://kton.dev/v/about> $record ; <https://kton.dev/v/replaces> ?replaces }\n",
			"controls/attend.json": `{"kind":"record","meaning":"trained-on","say":{"template":"reads"}}`,
			"ui/area.json":         `{"slot":"activity","label":"Training","query":"count","area":` + a + `}`,
		}
	}
	good := `{"scopes":{"records":"qm.training","reads":["roles"]},"record":"record","raci":"raci","approvals":"approvals","history":"history",
		"params":{"person":"subject.id","record":"subject.iri"},"actions":["attend","confirm"],"documents":["SOP-02"],"governing":"SOP-02",
		"findings":{"package":"qm-rules","rules":["SOP02-R-TRAINER-L3"]}}`
	r, err := buildUI(t, area(good))
	if err != nil {
		t.Fatalf("the C3.2 area must build: %v", err)
	}
	ui, _ := r.UIEntries()
	if a := ui["area"].Area; a == nil || a.Scopes.Records != "qm.training" || a.Governing != "SOP-02" || a.Params["person"] != "subject.id" || a.Approvals != "approvals" {
		t.Fatalf("area read back: %+v", ui["area"].Area)
	}
	cs, _ := r.Controls()
	if cs["attend"].Meaning != "trained-on" {
		t.Fatalf("meaning read back: %+v", cs["attend"])
	}
	for _, c := range []struct {
		name string
		edit map[string]string
		want string
	}{
		{"the old single scope", area(strings.Replace(good, `"scopes":{"records":"qm.training","reads":["roles"]}`, `"scope":"qm.training"`, 1)), `unknown field "scope"`},
		{"no records scope", area(strings.Replace(good, `"records":"qm.training",`, ``, 1)), "scopes.records names no scope"},
		{"unbound $VAR", area(strings.Replace(good, `"person":"subject.id",`, ``, 1)), `uses $person, which params does not bind`},
		{"approvals without its shape", with(area(good), "queries/approvals.rq", "SELECT ?claim ?record ?tier WHERE { ?claim ?p $record }\n"), `approvals: the query "approvals" does not select ?outcome`},
		{"param outside the shell values", area(strings.Replace(good, `"subject.id"`, `"env.USER"`, 1)), `binds "env.USER", which is not a shell value`},
		{"param no query uses", area(strings.Replace(good, `"record":"subject.iri"`, `"record":"subject.iri","doc":"subject.id"`, 1)), "param doc is not a $VAR of an area query"},
		{"query name as a rule", area(strings.Replace(good, `"SOP02-R-TRAINER-L3"`, `"closed deviation date"`, 1)), "is not a rule id"},
		{"rule twice", area(strings.Replace(good, `["SOP02-R-TRAINER-L3"]`, `["A-1","A-1"]`, 1)), `names the rule "A-1" twice`},
		{"meaning outside C3.4", map[string]string{"controls/attend.json": `{"meaning":"rejected","say":{"template":"reads"}}`}, `meaning "rejected" is not one of`},
		{"meaning on the template", map[string]string{"templates/reads.json": `{"name":"reads","meaning":"approved","target":"claim","predicate":"https://kton.dev/v/reads","fields":{"document":{"type":"string","required":true}}}`}, "a meaning belongs to the control"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildUI(t, c.edit)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want a refusal with %q, got %v", c.want, err)
			}
		})
	}
}

func with(m map[string]string, k, v string) map[string]string {
	m[k] = v
	return m
}

// Layout arranges one level's parts: every part placed once, areas are part ids and rectangles,
// sizes from the closed set; report and deck keep their order.
func TestBuildVerticle_ChecksLayout(t *testing.T) {
	part := func(id string) string {
		return `{"id":"` + id + `","heading":"` + id + `","blocks":[{"use":"text","file":"content/text/intro.md"}]}`
	}
	dash := func(kind, layout string, ids ...string) map[string]string {
		var ps []string
		for _, id := range ids {
			ps = append(ps, part(id))
		}
		return map[string]string{"ui/compositions/dash.json": `{"kind":"` + kind + `","layout":` + layout + `,"parts":[` + strings.Join(ps, ",") + `]}`}
	}
	grid := `{"type":"grid","columns":["1fr","3fr","240px"],"rows":["auto","50%"],"areas":[["filter","graph","card"],["filter","graph","more"]],"narrow":"tabs"}`
	if _, err := buildUI(t, dash("view", grid, "filter", "graph", "card", "more")); err != nil {
		t.Fatalf("a grid must build: %v", err)
	}
	nested := map[string]string{"ui/compositions/dash.json": `{"kind":"view","layout":{"type":"split","ratio":0.3},"parts":[` + part("list") +
		`,{"id":"record","heading":"Record","layout":{"type":"tabs","active":"history"},"parts":[` + part("details") + `,` + part("history") + `]}]}`}
	if _, err := buildUI(t, nested); err != nil {
		t.Fatalf("split with tabs in a part must build: %v", err)
	}
	for _, c := range []struct {
		name string
		edit map[string]string
		want string
	}{
		{"unknown type", dash("view", `{"type":"masonry"}`, "a"), `layout type "masonry" is not one of`},
		{"css in a size", dash("view", `{"type":"columns","columns":["calc(100% - 1px)"]}`, "a"), `layout size "calc(100% - 1px)"`},
		{"part not placed", dash("view", grid, "filter", "graph", "card", "more", "extra"), `does not place the part "extra"`},
		{"area not a part", dash("view", grid, "filter", "graph", "card"), `names "more", which is not a part here`},
		{"area not a rectangle", dash("view", `{"type":"grid","columns":["1fr","1fr"],"areas":[["a","b"],["b","b"]]}`, "a", "b"), `the area of "b" is not a rectangle`},
		{"ragged grid", dash("view", `{"type":"grid","columns":["1fr","1fr"],"areas":[["a","b"],["a"]]}`, "a", "b"), "row 2 has 1 cells, not 2"},
		{"sizes do not match", dash("view", `{"type":"grid","columns":["1fr"],"areas":[["a","b"]]}`, "a", "b"), "2 columns of areas but 1 column sizes"},
		{"split of three", dash("view", `{"type":"split"}`, "a", "b", "c"), "split places exactly two named parts"},
		{"ratio out of range", dash("view", `{"type":"split","ratio":1.5}`, "a", "b"), "ratio 1.5 is not between 0 and 1"},
		{"field of another type", dash("view", `{"type":"tabs","ratio":0.5}`, "a", "b"), "layout ratio belongs to split, not tabs"},
		{"active not a part", dash("view", `{"type":"tabs","active":"zz"}`, "a", "b"), `layout active "zz" is not a part here`},
		{"layout on a report", dash("report", `{"type":"tabs"}`, "a", "b"), "layout on a report"},
		{"layout on a leaf part", map[string]string{"ui/compositions/dash.json": `{"kind":"view","parts":[{"id":"a","layout":{"type":"tabs"},"blocks":[{"use":"text","file":"content/text/intro.md"}]}]}`}, "this part has none"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildUI(t, c.edit)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want a refusal with %q, got %v", c.want, err)
			}
		})
	}
}

// An optional $VAR may stay unbound ("all" vs "mine", S6.7): never bound when not in params, or
// unbound while its source has no value. Checked at build like params.
func TestBuildVerticle_OptionalQueryParams(t *testing.T) {
	src := uiSrc(t, nil)
	b, err := os.ReadFile(filepath.Join(src, "ui/compositions/main.json"))
	if err != nil {
		t.Fatal(err)
	}
	deep := `{"use":"table","query":"card","params":{"item":"var.picked"},"empty":"show"}`
	if !strings.Contains(string(b), deep) {
		t.Fatal("main.json changed: no deep use")
	}
	main := func(use string) map[string]string {
		return map[string]string{"ui/compositions/main.json": strings.Replace(string(b), deep, use, 1)}
	}
	items := `"refine":{"status":{"values":["open","done"]}}`
	area := func(a string) map[string]string {
		return map[string]string{
			"queries/training.rq": "SELECT ?o WHERE { ?o <https://kton.dev/v/for> $person }\n",
			"ui/area.json":        `{"slot":"activity","label":"Training","query":"count","area":` + a + `}`,
		}
	}
	for _, c := range []struct {
		name string
		edit map[string]string
	}{
		{"never bound", main(`{"use":"table","query":"card","optional":["item"],"empty":"show"}`)},
		{"bound while picked", main(`{"use":"table","query":"card","params":{"item":"var.picked"},"optional":["item"],"empty":"show"}`)},
		{"area query", area(`{"scopes":{"records":"qm.training"},"training":"training","optional":["person"]}`)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := buildUI(t, c.edit); err != nil {
				t.Fatalf("must build: %v", err)
			}
		})
	}
	r, err := buildUI(t, main(`{"use":"table","query":"card","optional":["item"],"empty":"show"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cs, _ := r.Compositions(); cs["main"].Parts[2].Parts[0].Blocks[0].Optional[0] != "item" {
		t.Fatalf("optional read back: %+v", cs["main"].Parts[2].Parts[0].Blocks[0])
	}
	for _, c := range []struct {
		name string
		edit map[string]string
		want string
	}{
		{"still unbound without optional", main(`{"use":"table","query":"card","empty":"show"}`), "uses $item, which neither params nor refine binds"},
		{"not a $VAR", main(`{"use":"table","query":"card","params":{"item":"var.picked"},"optional":["who"],"empty":"show"}`), "optional who is not a $VAR of the query"},
		{"twice", main(`{"use":"table","query":"card","optional":["item","item"],"empty":"show"}`), "optional item is named twice"},
		{"refined", map[string]string{"ui/compositions/main.json": strings.Replace(string(b), items, items+`,"optional":["status"]`, 1)}, "optional status is also refined"},
		{"no query", map[string]string{"ui/compositions/main.json": strings.Replace(string(b), `{"use":"text","file":"content/text/intro.md"}`, `{"use":"text","file":"content/text/intro.md","optional":["x"]}`, 1)}, "params, refine and optional need a query"},
		{"area: not a $VAR", area(`{"scopes":{"records":"qm.training"},"training":"training","optional":["person","who"]}`), "optional who is not a $VAR of the query"},
		{"area: still unbound", area(`{"scopes":{"records":"qm.training"},"training":"training"}`), "uses $person, which params does not bind"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildUI(t, c.edit)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want a refusal with %q, got %v", c.want, err)
			}
		})
	}
}
