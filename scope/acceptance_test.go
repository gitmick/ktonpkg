package scope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The v0.10.0 acceptance (§ When, F11): for each of qm-packages' 20 documents, its class composition
// and its chapters, converted from documents/<ID>/, compose and fill back to the document's
// report-v2 in reports/<ID>/, the static content byte-equal. Query slots (RACI, the org chart) need
// the scopes' current state and a SPARQL engine, which these tests do not have: they are answered
// with no rows, so their blocks drop out (empty omit) and are left out of the comparison.
func TestAcceptance_QMPackagesRoundTrip(t *testing.T) {
	docs := qmPackages(t)
	root := filepath.Dir(docs)
	key := newKey(t)

	// qm-core: the vocabulary's queries.
	coreSrc := t.TempDir()
	qs, _ := filepath.Glob(filepath.Join(root, "vocabulary", "qm-core", "queries", "*.rq"))
	for _, q := range qs {
		writeFile(t, filepath.Join(coreSrc, "queries", filepath.Base(q)), readFile(t, q))
	}
	pkgDir := t.TempDir()
	_, core, err := BuildVerticle(filepath.Join(pkgDir, "qm-core"), "qm-core", "v1", coreSrc, key, Options{Clock: fixedClock()})
	if err != nil {
		t.Fatalf("qm-core: %v", err)
	}

	// Convert every document; the class composition comes from the class's first document.
	names, _ := filepath.Glob(filepath.Join(docs, "*", "dokument.md"))
	sort.Strings(names)
	convs := map[string]*DokumentConversion{}
	classes := map[string][]string{}
	for _, n := range names {
		c, err := ConvertDokument(filepath.Dir(n))
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(filepath.Dir(n)), err)
		}
		convs[c.ID] = c
		classes[c.Class] = append(classes[c.Class], c.ID)
	}
	if len(convs) != 20 {
		t.Fatalf("%d documents converted, want 20", len(convs))
	}

	// qm-reports: the blocks and the class compositions.
	src := t.TempDir()
	options := map[string]string{
		"table": `{"columns":{"set":"literal"}}`,
		"tree":  `{"node":"literal","label":"literal","holder":"literal","parent":"literal"}`,
	}
	for _, b := range []string{"text", "table", "image", "diagram", "tree"} {
		decl := `{"viewer":{"viewer":"kton-web/` + b + `","at":"` + strings.Repeat("a", 40) + `","tree":"sha256:` + strings.Repeat("b", 64) + `"}`
		if o, ok := options[b]; ok {
			decl += `,"options":` + o
		}
		writeFile(t, filepath.Join(src, "ui", "blocks", b+".json"), []byte(decl+"}"))
	}
	for cls, ids := range classes {
		c := ClassComposition(cls, convs[ids[0]].EachPart)
		b, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(src, "ui", "compositions", c.Composition+".json"), b)
	}
	_, reports, err := BuildVerticle(filepath.Join(pkgDir, "qm-reports"), "qm-reports", "v1", src, key, Options{Clock: fixedClock()})
	if err != nil {
		t.Fatalf("qm-reports: %v", err)
	}
	packages := map[string]*Revision{"qm-core": core, "qm-reports": reports}

	var mismatches []string
	for _, id := range sortedKeys(convs) {
		c := convs[id]
		t.Run(id, func(t *testing.T) {
			files, err := c.InputFiles()
			if err != nil {
				t.Fatal(err)
			}
			in, err := LoadInput("chapters", func(p string) ([]byte, error) {
				b, ok := files[p]
				if !ok {
					return nil, fmt.Errorf("no %s", p)
				}
				return b, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			req := ComposeRequest{Package: "qm-reports", Composition: strings.ToLower(c.Class) + "-class",
				Subject: Subject{IRI: "https://scinteco.com/ns/qms#" + id, Identifier: id, Title: ""},
				Inputs:  map[string]*Input{"chapters": in}, Packages: packages, Results: map[string]Answer{}}
			composed, needs, err := Compose(req)
			if err != nil || len(needs) > 0 {
				t.Fatalf("compose: %v (needs %d)", err, len(needs))
			}
			if composed.Subject.Class != c.Class {
				t.Errorf("the class comes from the manifest: %q", composed.Subject.Class)
			}
			answers := map[string]Answer{}
			for _, u := range composed.Uses {
				if u.Ask != "" {
					answers[u.Ask] = Answer{}
				}
			}
			filled, err := Fill(composed, answers)
			if err != nil {
				t.Fatal(err)
			}
			theirs := readReport(t, filepath.Join(root, "reports", id, "report.json"))
			mismatches = append(mismatches, compareReport(t, id, filled, theirs, filepath.Join(root, "reports", id))...)
		})
	}
	// The documents were edited after qm-packages' reports were made: MAN-OrgChart's first section
	// was renamed and its ownership text reworded (position -> role), and seven PNG diagrams were
	// redrawn as Mermaid sources. Fill follows the documents, so these differences are the sources',
	// not the round trip's. Any other difference fails.
	known := map[string]bool{}
	for _, d := range []string{"MAN-01", "SOP-07", "SOP-16"} {
		known[d+":diagram"] = true
	}
	var unexpected []string
	diagrams := 0
	for _, m := range mismatches {
		doc, _, _ := strings.Cut(m, ":")
		switch {
		case known[doc+":diagram"] && strings.Contains(m, ": diagram diagrams/") && strings.Contains(m, "qm-packages: image images/"):
			diagrams++
		case doc == "MAN-OrgChart":
		default:
			unexpected = append(unexpected, m)
		}
	}
	if len(unexpected) > 0 || diagrams != 7 || len(mismatches) != 12 {
		t.Errorf("%d differences (%d diagrams), want the 12 known ones:\n%s", len(mismatches), diagrams, strings.Join(mismatches, "\n"))
	}
}

// compareReport compares the static content: sections (id, heading, level, rules) and every block
// that is not a query slot, with each file byte-equal to qm-packages' own.
func compareReport(t *testing.T, id string, f *Filled, theirs ReportV2, dir string) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, id+": "+fmt.Sprintf(format, args...)) }
	// Static: a file in sections/, tables/, images/ or diagrams/ that no slot wrote. Slot output is
	// <part>--<use> (fill), or the old pipeline's RACI/, Schulungsmatrix/, Organigramm/ and its
	// "-stand.md" notes, which the documents replaced with slots.
	static := func(bs []ReportBlock) []ReportBlock {
		var keep []ReportBlock
		for _, b := range bs {
			dir, _, _ := strings.Cut(b.File, "/")
			if has([]string{"sections", "tables", "images", "diagrams"}, dir) && !strings.Contains(b.File, "--") &&
				!strings.HasSuffix(b.File, "-stand.md") {
				keep = append(keep, b)
			}
		}
		return keep
	}
	if len(f.Report.Sections) != len(theirs.Sections) {
		add("%d sections, qm-packages has %d", len(f.Report.Sections), len(theirs.Sections))
		return out
	}
	for i, s := range f.Report.Sections {
		o := theirs.Sections[i]
		if s.ID != o.ID || s.Heading != o.Heading || s.Level != o.Level || strings.Join(s.Rules, ",") != strings.Join(o.Rules, ",") {
			add("section %d: %s %q level %d rules %v; qm-packages: %s %q level %d rules %v", i, s.ID, s.Heading, s.Level, s.Rules, o.ID, o.Heading, o.Level, o.Rules)
		}
		mine, their := static(s.Blocks), static(o.Blocks)
		if len(mine) != len(their) {
			add("section %s: %d static blocks, qm-packages has %d", s.ID, len(mine), len(their))
			continue
		}
		for j := range mine {
			a, b := mine[j], their[j]
			if a.Type != b.Type || a.File != b.File || capOf(a) != capOf(b) {
				add("section %s block %d: %s %s %q; qm-packages: %s %s %q", s.ID, j, a.Type, a.File, capOf(a), b.Type, b.File, capOf(b))
				continue
			}
			want, err := os.ReadFile(filepath.Join(dir, b.File))
			if err != nil {
				add("%s: %v", b.File, err)
				continue
			}
			if !bytes.Equal(f.Files[a.File], want) {
				add("%s differs from qm-packages' bytes", a.File)
			}
		}
	}
	return out
}

func capOf(b ReportBlock) string {
	if b.Caption == nil {
		return ""
	}
	return *b.Caption
}

func readReport(t *testing.T, p string) ReportV2 {
	t.Helper()
	var r ReportV2
	if err := json.Unmarshal(readFile(t, p), &r); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return r
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeFile(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
