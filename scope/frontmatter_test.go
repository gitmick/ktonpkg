package scope

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFrontmatter(t *testing.T) {
	front, body, err := splitFront(`---
id: SOP-02
title: "Employee \"Training\""
class: SOP
# a comment
slots:
  raci: { scope: qm.raci.SOP-02, query: ../../q/raci.rq, as: table, columns: [Activity, R, "Deputy (1st)"], every: chapter }
  rollen: { scope: qm.positions, query: q.rq, as: table,
            columns: [A, B] }
quelle:
  report: { uri: "target-qms:out/r.json", hash: sha256:93d6 }
level: 2
---
Body.
`)
	if err != nil {
		t.Fatal(err)
	}
	if front.str("id") != "SOP-02" || front.str("title") != `Employee "Training"` || body != "Body.\n" {
		t.Fatalf("scalars: %q %q %q", front.str("id"), front.str("title"), body)
	}
	if v, _ := front.get("level"); v != 2 {
		t.Fatalf("level: %#v", v)
	}
	sv, _ := front.get("slots")
	slots := sv.(*ymap)
	if len(slots.keys) != 2 || slots.keys[0] != "raci" || slots.keys[1] != "rollen" {
		t.Fatalf("slot order: %v", slots.keys)
	}
	rv, _ := slots.get("raci")
	raci := rv.(*ymap)
	cols, _ := raci.get("columns")
	if raci.str("every") != "chapter" || len(cols.([]any)) != 3 || cols.([]any)[2] != "Deputy (1st)" {
		t.Fatalf("raci: %+v", raci.vals)
	}
	qv, _ := front.get("quelle")
	rep, _ := qv.(*ymap).get("report")
	if rep.(*ymap).str("hash") != "sha256:93d6" {
		t.Fatalf("quelle: %+v", rep)
	}
	for _, bad := range []string{"---\na: 'single'\n---\n", "---\na: [1, 2\n---\n", "---\na: 1\na: 2\n---\n", "---\na: &x 1\n---\n"} {
		if _, _, err := splitFront(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

// Every frontmatter of qm-packages' 20 documents reads (when the sources are on this machine).
func TestFrontmatter_QMPackages(t *testing.T) {
	docs := qmPackages(t)
	files, _ := filepath.Glob(filepath.Join(docs, "*", "dokument.md"))
	more, _ := filepath.Glob(filepath.Join(docs, "*", "kapitel", "*.md"))
	files = append(files, more...)
	if len(files) < 20 {
		t.Fatalf("only %d files", len(files))
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := splitFront(string(b)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

// qmPackages is qm-packages' documents folder, or the test is skipped where it is not.
func qmPackages(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("QM_PACKAGES")
	if dir == "" {
		dir = "/home/lanes/workstreams/active/userinterface/incoming/sources/qm-packages"
	}
	if _, err := os.Stat(filepath.Join(dir, "documents")); err != nil {
		t.Skip("qm-packages sources are not on this machine (set QM_PACKAGES)")
	}
	return filepath.Join(dir, "documents")
}
