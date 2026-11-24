package scope

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regressions for the review of PR #1 (23.11.): one test per finding.

func TestReview_FrontmatterPlainScalars(t *testing.T) {
	front, _, err := splitFront("---\r\nid: 10\r\nheading: Zweck, Geltungsbereich [neu]\r\nlevel: 1\r\n---\r\nText.\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if front.str("id") != "10" || front.str("heading") != "Zweck, Geltungsbereich [neu]" {
		t.Fatalf("id %q heading %q", front.str("id"), front.str("heading"))
	}
	f2, _, err := splitFront("---\nslots:\n  a: { x: [1, 2], y: b }\n---\n")
	if err != nil {
		t.Fatal(err)
	}
	av, _ := f2.get("slots")
	a, _ := av.(*ymap).get("a")
	if a.(*ymap).str("y") != "b" {
		t.Fatalf("flow collections still end plain scalars at , ] }: %+v", a)
	}
}

func TestReview_NestedStateTwoLevelsDeep(t *testing.T) {
	pkgs := composeFixture(t, map[string]string{
		"ui/compositions/outer.json": `{"kind":"view","parts":[{"id":"a","heading":"A","composition":"mid"}]}`,
		"ui/compositions/mid.json":   `{"kind":"view","parts":[{"id":"b","heading":"B","composition":"leaf"}]}`,
		"ui/compositions/leaf.json":  `{"kind":"view","vars":{"x":{"type":"literal","default":"d"}},"parts":[{"id":"c","heading":"C","blocks":[{"use":"table","query":"count"}]}]}`,
	})
	state := State{Nested: map[string]State{"a": {Nested: map[string]State{"b": {Vars: map[string]json.RawMessage{"x": json.RawMessage(`"given"`)}}}}}}
	c, _, err := Compose(ComposeRequest{Package: "render", Composition: "outer", Subject: fixtureSubject, Packages: pkgs, State: state})
	if err != nil {
		t.Fatal(err)
	}
	leaf := c.Compositions[2]
	if leaf.Key != "a/b" || string(leaf.Vars["x"]) != `"given"` || len(c.StateErrors) != 0 {
		t.Fatalf("leaf %+v, state errors %+v", leaf, c.StateErrors)
	}
}

func TestReview_LayoutStateOverInputParts(t *testing.T) {
	pkgs := composeFixture(t, map[string]string{
		"ui/compositions/tabbed.json": `{"kind":"view","inputs":["chapters"],"layout":{"type":"tabs"},"parts":[{"from":"chapters"}]}`})
	files := map[string][]byte{"chapters.json": []byte(`{"chapters":"D","parts":[{"id":"k1","heading":"One"},{"id":"k2","heading":"Two"}]}`)}
	m, _ := ManifestFor("", files)
	files["manifest.json"] = m
	in, err := LoadInput("chapters", func(p string) ([]byte, error) { return files[p], nil })
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := Compose(ComposeRequest{Package: "render", Composition: "tabbed", Subject: fixtureSubject, Packages: pkgs,
		Inputs: map[string]*Input{"chapters": in}, State: State{Layout: map[string]LayoutState{"": {Active: "k2"}}}})
	if err != nil || len(c.StateErrors) != 0 {
		t.Fatalf("a tab of an input part: %v %+v", err, c.StateErrors)
	}
}

func TestReview_OneKeyPerSection(t *testing.T) {
	// A package's build refuses a part that both nests and holds parts; an input's parts are not
	// built, so compose checks it: a/b would be the input's part and the nested composition's part.
	pkgs := composeFixture(t, map[string]string{
		"ui/compositions/doc.json":   `{"kind":"view","inputs":["chapters"],"parts":[{"from":"chapters"}]}`,
		"ui/compositions/inner.json": `{"kind":"view","parts":[{"id":"b","heading":"B too","blocks":[{"use":"table","query":"count"}]}]}`,
	})
	files := map[string][]byte{"chapters.json": []byte(`{"chapters":"D","parts":[{"id":"a","heading":"A","composition":"inner",
		"parts":[{"id":"b","heading":"B","blocks":[{"use":"table","query":{"package":"render","query":"count"}}]}]}]}`)}
	m, _ := ManifestFor("", files)
	files["manifest.json"] = m
	in, err := LoadInput("chapters", func(p string) ([]byte, error) { return files[p], nil })
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Compose(ComposeRequest{Package: "render", Composition: "doc", Subject: fixtureSubject, Packages: pkgs,
		Inputs: map[string]*Input{"chapters": in}})
	if ce, ok := err.(*ComposeError); !ok || ce.Code != "part" || !strings.Contains(ce.Message, "a/b") {
		t.Fatalf("two sections keyed a/b must be refused, got %v", err)
	}
}

func TestReview_FillRefusesClashesAndHiddenUses(t *testing.T) {
	c := &Composed{Subject: Subject{IRI: "https://x/D"},
		Sections: []Section{{Key: "a", ID: "a", Level: 1, Uses: []string{"a#1"}}, {Key: "b", ID: "b", Level: 1, Uses: []string{"b#1"}}},
		Uses: []ComposedUse{{Key: "a#1", ID: "1", Form: "image", Content: &ContentRef{Path: "content/images/flow.png", Bytes: []byte("A")}},
			{Key: "b#1", ID: "1", Form: "image", Content: &ContentRef{Path: "content/other/flow.png", Bytes: []byte("B")}}}}
	if _, err := Fill(c, nil); err == nil || !strings.Contains(err.Error(), "images/flow.png") {
		t.Fatalf("two images named flow.png: %v", err)
	}
	c.Uses[1].Content.Bytes = []byte("A") // the same file twice is fine
	if _, err := Fill(c, nil); err != nil {
		t.Fatal(err)
	}
	c.Sections[0].Unnamed = true
	if _, err := Fill(c, nil); err == nil || !strings.Contains(err.Error(), "no section of its own") {
		t.Fatalf("uses on an unnamed part: %v", err)
	}
}

func TestReview_ConverterIdsAndNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "q.rq"), []byte("SELECT ?a WHERE { ?a ?p $chapter }\n"))
	writeFile(t, filepath.Join(dir, "dokument.md"), []byte("---\nid: D\nclass: SOP\nslots:\n  raci: { scope: qm.raci.D, query: ../vocabulary/qm-core/queries/raci.rq, as: table, every: chapter }\n---\n"))
	writeFile(t, filepath.Join(dir, "..", "vocabulary", "qm-core", "queries", "raci.rq"), []byte("SELECT ?a WHERE { ?a ?p $chapter }\n"))
	writeFile(t, filepath.Join(dir, "kapitel", "01-a.md"), []byte("---\nid: a\nheading: A\nlevel: 1\n---\nOne.\n{{slot: raci}}\nTwo.\n{{slot: raci}}\n"))
	c, err := ConvertDokument(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, b := range c.Chapters.Parts[0].Blocks {
		if b.ID != "" {
			ids = append(ids, b.ID)
		}
	}
	if strings.Join(ids, ",") != "raci-2,raci-3" {
		t.Fatalf("slot use ids beside the eachPart raci: %v", ids)
	}
	writeFile(t, filepath.Join(dir, "img", "a", "flow.png"), []byte("A"))
	writeFile(t, filepath.Join(dir, "img", "b", "flow.png"), []byte("B"))
	writeFile(t, filepath.Join(dir, "kapitel", "02-b.md"), []byte("---\nid: b\nheading: B\nlevel: 1\n---\n{{image: img/a/flow.png}}\n{{image: img/b/flow.png}}\n"))
	if _, err := ConvertDokument(dir); err == nil || !strings.Contains(err.Error(), "has the name flow.png") {
		t.Fatalf("two images named flow.png: %v", err)
	}
	_ = os.Remove
}

func TestReview_RefineDefaultAndLanguageTag(t *testing.T) {
	if _, err := refineDefault(Refine{Type: "literal", Default: json.RawMessage(`0.5`)}); err == nil {
		t.Error("0.5 is not an integer")
	}
	if tm, err := refineDefault(Refine{Type: "integer", Default: json.RawMessage(`3`)}); err != nil || tm.Datatype != xsdInteger {
		t.Errorf("3: %+v %v", tm, err)
	}
	if _, err := (Term{Type: "literal", Value: "x", Lang: "en} UNION { ?s ?p ?o"}).Sparql(); err == nil {
		t.Error("a language tag that is not one must be refused")
	}
	if _, err := (Term{Type: "uri", Value: "https://x/\x00"}).Sparql(); err == nil {
		t.Error("a control character in an IRI must be refused")
	}
	if s, err := (Term{Type: "literal", Value: "x", Lang: "de-AT"}).Sparql(); err != nil || s != `"x"@de-AT` {
		t.Errorf("de-AT: %q %v", s, err)
	}
}

func TestReview_NestedSubjectIsAnIRI(t *testing.T) {
	r, err := buildUI(t, map[string]string{
		"ui/compositions/outer.json": `{"kind":"view","parts":[{"id":"a","heading":"A","composition":"inner","params":{"subject":"subject.id"}}]}`,
		"ui/compositions/inner.json": `{"kind":"view","parts":[{"id":"b","heading":"B","blocks":[{"use":"table","query":"count"}]}]}`,
	})
	if err != nil {
		t.Skipf("the build already refuses a literal subject: %v", err)
	}
	_, _, err = Compose(ComposeRequest{Package: "render", Composition: "outer", Subject: Subject{IRI: "https://x/D", ID: "sha256:" + strings.Repeat("1", 64)},
		Packages: map[string]*Revision{"render": r}})
	if ce, ok := err.(*ComposeError); !ok || ce.Code != "subject-from-var" {
		t.Fatalf("a literal subject must be refused, got %v", err)
	}
}
