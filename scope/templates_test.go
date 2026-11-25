package scope

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// A '*' in the source folder's name must not pull a sibling folder's files into the signed
// revision (kton-review G2 on kton-cockpit #256).
func TestBuildTemplates_AStarInTheFolderIsAName(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tpl := `{"name":"%s","target":"either","predicate":"https://kton.dev/v/x","fields":{"d":{"type":"string"}}}`
	write("t*/own.json", fmtS(tpl, "own"))
	write("tx/sibling.json", fmtS(tpl, "sibling"))
	write("q*/own.rq", "SELECT * WHERE { ?s ?p ?o }\n")
	write("qx/sibling.rq", "SELECT * WHERE { ?s ?p ?o }\n")
	_, r, err := BuildTemplates(filepath.Join(t.TempDir(), "vok"), "vok", "1", filepath.Join(root, "t*"),
		filepath.Join(root, "q*"), newKey(t), Options{Clock: fixedClock()})
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := r.Templates()
	qs, _ := r.Queries()
	if len(ts) != 1 || ts["own"] == nil || len(qs) != 1 || qs["own"] == nil {
		t.Fatalf("only the folder's own files: templates %v, queries %v", Names(ts), Names(qs))
	}
}

func fmtS(format, name string) string {
	return fmt.Sprintf(format, name)
}
