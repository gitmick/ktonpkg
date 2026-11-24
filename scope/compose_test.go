package scope

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func composeFixture(t *testing.T, edit map[string]string) map[string]*Revision {
	t.Helper()
	r, err := buildUI(t, edit)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]*Revision{"render": r}
}

func useByKey(c *Composed, key string) *ComposedUse {
	for i := range c.Uses {
		if c.Uses[i].Key == key {
			return &c.Uses[i]
		}
	}
	return nil
}

var fixtureSubject = Subject{IRI: "https://kton.dev/doc/1", Claim: "sha256:" + strings.Repeat("c", 64), Identifier: "DOC-1"}

// The fixture's main view: a var without a value makes its uses wait; given, they ask.
func TestCompose_VarsWaitAndBind(t *testing.T) {
	pkgs := composeFixture(t, nil)
	req := ComposeRequest{Package: "render", Composition: "main", Subject: fixtureSubject, Packages: pkgs}
	c, needs, err := Compose(req)
	if err != nil || len(needs) > 0 {
		t.Fatalf("compose: %v %v", err, needs)
	}
	if c.Root.Name != "main" || c.Root.Hash == "" || !hashPattern.MatchString(c.Root.Hash) {
		t.Fatalf("root: %+v", c.Root)
	}
	items := useByKey(c, "list#items")
	if items == nil || items.Ask == "" || items.Params["doc"].Value != fixtureSubject.IRI || items.Params["status"].Value != "open" {
		t.Fatalf("items: %+v", items)
	}
	if len(items.Acts) != 2 || items.Acts[0].About != fixtureSubject.Claim || items.Acts[0].Package != "render" {
		t.Fatalf("acts: %+v", items.Acts)
	}
	deep := useByKey(c, "#3/deep#1")
	if deep == nil || deep.Ask != "" || strings.Join(deep.Waiting, ",") != "var.picked" {
		t.Fatalf("deep must wait for var.picked: %+v", deep)
	}
	var detail *Section
	for i := range c.Sections {
		if c.Sections[i].Key == "detail" {
			detail = &c.Sections[i]
		}
	}
	if detail == nil || strings.Join(detail.Waiting, ",") != "var.picked" || len(c.Compositions) != 1 {
		t.Fatalf("the nested detail must wait as a whole: %+v, %d compositions", detail, len(c.Compositions))
	}
	// eachPart on every named part, with the part's own key
	if useByKey(c, "intro#each1") == nil || useByKey(c, "list#each1") == nil {
		t.Fatal("eachPart missing")
	}

	// With a value for picked, the deep use asks and the detail composes, its event wired back.
	req.State = State{Vars: map[string]json.RawMessage{"picked": json.RawMessage(`"https://kton.dev/p/7"`)}}
	c, _, err = Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	deep = useByKey(c, "#3/deep#1")
	if deep.Ask == "" || deep.Params["item"].Value != "https://kton.dev/p/7" {
		t.Fatalf("deep: %+v", deep)
	}
	if len(c.Compositions) != 2 || c.Compositions[1].Key != "detail" || c.Compositions[1].Name != "detail" {
		t.Fatalf("compositions: %+v", c.Compositions)
	}
	card := useByKey(c, "detail/card#1")
	if card == nil || card.Params["item"].Value != "https://kton.dev/p/7" {
		t.Fatalf("the child binds its param from the parent: %+v", card)
	}
	if len(c.Publishes) != 1 || c.Publishes[0].Event != "chose" || c.Publishes[0].To.Var != "picked" || c.Publishes[0].From.Composition != "detail" {
		t.Fatalf("publishes: %+v", c.Publishes)
	}

	// A bad value from the URL falls back and is reported, never refused.
	req.State = State{Vars: map[string]json.RawMessage{"picked": json.RawMessage(`42`)}}
	c, _, err = Compose(req)
	if err != nil || len(c.StateErrors) != 1 || c.StateErrors[0].Key != "var.picked" {
		t.Fatalf("state errors: %v %+v", err, c.StateErrors)
	}
}

// every repeats over a query's rows: the first round returns the need, the next the parts.
func TestCompose_EveryNeedsRounds(t *testing.T) {
	src := uiSrc(t, nil)
	main := mainJSON(src, t)
	every := `{"id":"person","heading":"Person","every":{"query":"count"},"blocks":[{"use":"table","query":"card","params":{"item":"row.n"}}]}`
	pkgs := composeFixture(t, map[string]string{"ui/compositions/main.json": strings.Replace(main, `"parts":[`, `"parts":[`+every+`,`, 1)})
	req := ComposeRequest{Package: "render", Composition: "main", Subject: fixtureSubject, Packages: pkgs, Results: map[string]Answer{}}
	c, needs, err := Compose(req)
	if err != nil || c != nil || len(needs) != 1 || needs[0].Query.Name != "count" {
		t.Fatalf("round 1: %v %v %+v", err, c, needs)
	}
	var a Answer
	a.Head.Vars = []string{"n"}
	a.Results.Bindings = []map[string]Term{{"n": {Type: "uri", Value: "https://kton.dev/p/1"}}, {"n": {Type: "uri", Value: "https://kton.dev/p/2"}}}
	req.Results[needs[0].Key] = a
	c, needs, err = Compose(req)
	if err != nil || len(needs) > 0 {
		t.Fatalf("round 2: %v %+v", err, needs)
	}
	for i, want := range []string{"https://kton.dev/p/1", "https://kton.dev/p/2"} {
		u := useByKey(c, fmt.Sprintf("person.%d#1", i+1))
		if u == nil || u.Params["item"].Value != want {
			t.Fatalf("row %d: %+v", i+1, u)
		}
	}
}

func TestCompose_Refusals(t *testing.T) {
	pkgs := composeFixture(t, nil)
	for _, c := range []struct {
		name string
		req  ComposeRequest
		code string
	}{
		{"no subject", ComposeRequest{Package: "render", Composition: "main", Packages: pkgs}, "subject"},
		{"not admitted", ComposeRequest{Package: "other", Composition: "main", Subject: fixtureSubject, Packages: pkgs}, "unknown-ref"},
		{"no such composition", ComposeRequest{Package: "render", Composition: "nope", Subject: fixtureSubject, Packages: pkgs}, "unknown-ref"},
		{"sign without a claim", ComposeRequest{Package: "render", Composition: "main", Subject: Subject{IRI: "https://kton.dev/doc/1"}, Packages: pkgs}, "unbound-var"},
		{"a param not bound", ComposeRequest{Package: "render", Composition: "detail", Subject: fixtureSubject, Packages: pkgs}, "param"},
		{"a param not declared", ComposeRequest{Package: "render", Composition: "main", Subject: fixtureSubject, Packages: pkgs,
			Params: map[string]Term{"x": {Type: "literal", Value: "1"}}}, "param"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := Compose(c.req)
			ce, ok := err.(*ComposeError)
			if !ok || ce.Code != c.code {
				t.Fatalf("want a %s refusal, got %v", c.code, err)
			}
		})
	}
}

// A composition for a class composes only for a subject of that class; the input manifest says it.
func TestCompose_ClassFromTheInput(t *testing.T) {
	src := uiSrc(t, nil)
	pkgs := composeFixture(t, map[string]string{"ui/compositions/doc.json": `{"kind":"report","class":"SOP","inputs":["chapters"],"parts":[{"from":"chapters"}]}`})
	_ = src
	files := map[string][]byte{"chapters.json": []byte(`{"chapters":"D","parts":[{"id":"a","heading":"A","blocks":[{"use":"text","file":"content/a-1.md"}]}]}`),
		"content/a-1.md": []byte("Hello.\n")}
	input := func(class string) *Input {
		m, err := ManifestFor(class, files)
		if err != nil {
			t.Fatal(err)
		}
		all := map[string][]byte{"manifest.json": m}
		for k, v := range files {
			all[k] = v
		}
		in, err := LoadInput("chapters", func(p string) ([]byte, error) { return all[p], nil })
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	req := ComposeRequest{Package: "render", Composition: "doc", Subject: fixtureSubject, Packages: pkgs, Inputs: map[string]*Input{"chapters": input("SOP")}}
	c, _, err := Compose(req)
	if err != nil || c.Subject.Class != "SOP" || c.Inputs["chapters"].Manifest == "" {
		t.Fatalf("compose: %v %+v", err, c)
	}
	if u := useByKey(c, "a#1"); u == nil || u.Content.Source != "chapters" || string(u.Content.Bytes) != "Hello.\n" {
		t.Fatalf("content from the input: %+v", u)
	}
	req.Inputs["chapters"] = input("MAN")
	if _, _, err := Compose(req); err == nil || err.(*ComposeError).Code != "class" {
		t.Fatalf("a MAN input for an SOP composition: %v", err)
	}
	req.Inputs = nil
	if _, _, err := Compose(req); err == nil || err.(*ComposeError).Code != "input" {
		t.Fatalf("no input: %v", err)
	}
}

func TestLoadInput_Refusals(t *testing.T) {
	files := map[string][]byte{"chapters.json": []byte(`{"chapters":"D","parts":[]}`)}
	m, _ := ManifestFor("SOP", files)
	read := func(over map[string][]byte) func(string) ([]byte, error) {
		return func(p string) ([]byte, error) {
			if b, ok := over[p]; ok {
				return b, nil
			}
			if p == "manifest.json" {
				return m, nil
			}
			return files[p], nil
		}
	}
	if _, err := LoadInput("c", read(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadInput("c", read(map[string][]byte{"chapters.json": []byte(`{}`)})); err == nil || !strings.Contains(err.Error(), "the input manifest says") {
		t.Fatalf("a changed file: %v", err)
	}
	bad := strings.Replace(string(m), InputSchema, "x/v9", 1)
	if _, err := LoadInput("c", read(map[string][]byte{"manifest.json": []byte(bad)})); err == nil || !strings.Contains(err.Error(), "manifest schema") {
		t.Fatalf("another schema: %v", err)
	}
	esc := strings.Replace(string(m), `"chapters.json"`, `"../chapters.json"`, 1)
	if _, err := LoadInput("c", read(map[string][]byte{"manifest.json": []byte(esc)})); err == nil || !strings.Contains(err.Error(), "segment") {
		t.Fatalf("a path out of the input: %v", err)
	}
}

func TestFill_QueryForms(t *testing.T) {
	rows := func(vars []string, rs ...[]Term) Answer {
		var a Answer
		a.Head.Vars = vars
		for _, r := range rs {
			b := map[string]Term{}
			for i, v := range vars {
				b[v] = r[i]
			}
			a.Results.Bindings = append(a.Results.Bindings, b)
		}
		return a
	}
	uri := func(v string) Term { return Term{Type: "uri", Value: v} }
	lit := func(v string) Term { return Term{Type: "literal", Value: v} }
	c := &Composed{Subject: Subject{IRI: "https://x/D", Identifier: "D", Class: "SOP"},
		Sections: []Section{{Key: "a", ID: "a", Heading: "A", Level: 1, Uses: []string{"a#t", "a#tree", "a#n", "a#empty", "a#g"}},
			{Key: "_u", ID: "_u", Level: 1, Unnamed: true}},
		Uses: []ComposedUse{
			{Key: "a#t", ID: "t", Form: "table", Ask: "k1", Query: &QueryRef{Package: "p", Name: "q1", Hash: "h1"}, Scope: "qm.raci.D",
				Options: map[string]json.RawMessage{"columns": json.RawMessage(`["Activity","R"]`)}, Empty: "omit"},
			{Key: "a#tree", ID: "tree", Form: "tree", Ask: "k2", Query: &QueryRef{Input: "chapters", Name: "q2", Hash: "h2"}, Caption: "Org", Empty: "omit"},
			{Key: "a#n", ID: "n", Form: "number", Ask: "k3", Query: &QueryRef{Package: "p", Name: "q3", Hash: "h3"},
				Options: map[string]json.RawMessage{"label": json.RawMessage(`"open items"`)}},
			{Key: "a#empty", ID: "empty", Form: "table", Ask: "k4", Query: &QueryRef{Package: "p", Name: "q4", Hash: "h4"}, Empty: "show"},
			{Key: "a#g", ID: "g", Form: "graph", Block: "p/graph"},
		}}
	results := map[string]Answer{
		"k1": rows([]string{"a", "r"}, []Term{lit("Plan <x> & do"), uri("https://s/ns#QA")}),
		"k2": rows([]string{"node", "label", "holder", "parent"}, []Term{uri("n1"), lit("Root"), lit("Ann"), {}}),
		"k3": rows([]string{"n"}, []Term{lit("7")}),
		"k4": rows([]string{"x"}),
	}
	f, err := Fill(c, results)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(f.Files["tables/a--t.json"]); got != "{\n  \"columns\": [\n    \"Activity\",\n    \"R\"\n  ],\n  \"rows\": [\n    [\n      \"Plan <x> & do\",\n      \"QA\"\n    ]\n  ],\n  \"boldHeader\": true\n}" {
		t.Errorf("table as zusammensetzen writes it:\n%s", got)
	}
	if !strings.Contains(string(f.Files["diagrams/a--tree.json"]), `"label": "Root"`) {
		t.Errorf("tree: %s", f.Files["diagrams/a--tree.json"])
	}
	if string(f.Files["sections/a-1.md"]) != "**7** open items\n" {
		t.Errorf("number: %q", f.Files["sections/a-1.md"])
	}
	if !strings.Contains(string(f.Files["tables/a--empty.json"]), `"rows": []`) {
		t.Errorf("empty show writes the table: %s", f.Files["tables/a--empty.json"])
	}
	if len(f.Report.Sections) != 1 || len(f.Report.Sections[0].Blocks) != 4 || f.Report.Sections[0].Rules == nil {
		t.Fatalf("sections: %+v", f.Report.Sections)
	}
	img := f.Report.Sections[0].Blocks[1]
	if img.Type != "image" || img.File != "images/a--tree.png" || img.Source != "diagrams/a--tree.json" || *img.Caption != "Org" {
		t.Errorf("tree block: %+v", img)
	}
	if len(f.Slots) != 5 || *f.Slots[0].Rows != 1 || f.Slots[0].Scope != "qm.raci.D" || f.Slots[1].Query != "chapters/q2" || f.Slots[4].Skipped == "" {
		t.Errorf("slots: %+v", f.Slots)
	}
	if _, err := Fill(c, map[string]Answer{}); err == nil {
		t.Error("a missing answer must fail")
	}
}

func TestQueryKeyAndBindQuery(t *testing.T) {
	p := map[string]Term{"b": {Type: "literal", Value: `say "hi"`}, "a": {Type: "uri", Value: "https://x/a"}}
	k, err := QueryKey("sha256:1", p)
	if err != nil || k != `sha256:1?a=<https://x/a>&b="say \"hi\""` {
		t.Fatalf("key: %q %v", k, err)
	}
	q, err := BindQuery([]byte("SELECT * WHERE { $a ?p $b . ?s ?p $free }"), p)
	if err != nil || q != `SELECT * WHERE { <https://x/a> ?p "say \"hi\"" . ?s ?p $free }` {
		t.Fatalf("bind: %q %v", q, err)
	}
	if _, err := QueryKey("h", map[string]Term{"a": {Type: "uri", Value: "https://x/a b"}}); err == nil {
		t.Fatal("an IRI with a space must be refused")
	}
}
