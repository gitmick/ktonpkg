package ktonpkg

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A '*', '?' or '[' in the folder's own path is a name, not a pattern (kton-review G2).
func TestNamedFiles_TheFolderIsNotAPattern(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"a*/x.json", "ab/sibling.json", "a?/y.json", "a[b]/z.json", "ab2/w.json"} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for dir, want := range map[string][]string{"a*": {"x.json"}, "a?": {"y.json"}, "a[b]": {"z.json"}} {
		got, err := NamedFiles(filepath.Join(root, dir), ".json")
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, g := range got {
			names = append(names, filepath.Base(g))
		}
		if !reflect.DeepEqual(names, want) {
			t.Errorf("%s: %v, want %v", dir, names, want)
		}
	}
	if got, err := NamedFiles(filepath.Join(root, "missing"), ".json"); err != nil || got != nil {
		t.Errorf("a missing folder lists nothing: %v %v", got, err)
	}
	if got, _ := NamedFiles(filepath.Join(root, "ab"), ".rq"); got != nil {
		t.Errorf("another suffix: %v", got)
	}
}
