package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// toOldLayout puts a package back into the layout before v0.8.0, as packages built with v0.7.0 lie.
func toOldLayout(t *testing.T, dir string) {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, "registry"), 0o755)
	for _, d := range []string{"nekton", "plankton"} {
		if err := os.Rename(filepath.Join(dir, d), filepath.Join(dir, "registry", d)); err != nil {
			t.Fatal(err)
		}
	}
	old := filepath.Join(dir, "blobs", "sha256")
	os.MkdirAll(old, 0o755)
	shards, _ := filepath.Glob(filepath.Join(dir, "blobs", "??"))
	for _, sh := range shards {
		files, _ := os.ReadDir(sh)
		for _, f := range files {
			os.Rename(filepath.Join(sh, f.Name()), filepath.Join(old, f.Name()))
		}
		os.Remove(sh)
	}
}

func TestMigrate_KeepsEveryIdAndVerifies(t *testing.T) {
	tdir, qdir := templateSource(t, testAliases)
	dir := filepath.Join(t.TempDir(), "vok")
	p, r, err := BuildTemplates(dir, "vok", "1", tdir, qdir, newKey(t), Options{Clock: fixedClock()})
	if err != nil {
		t.Fatal(err)
	}
	toOldLayout(t, dir)
	if _, err := Open(dir); err == nil {
		t.Fatal("v0.8.0 reads only the new layout")
	}
	moved, err := Migrate(dir)
	if err != nil || !moved {
		t.Fatalf("Migrate = %v, %v", moved, err)
	}
	for _, gone := range []string{"registry", filepath.Join("blobs", "sha256")} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s is still there", gone)
		}
	}
	q, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if q.ID != p.ID {
		t.Fatalf("package id %s, was %s", q.ID, p.ID)
	}
	if probs := q.Verify(); len(probs) > 0 {
		t.Fatal(probs)
	}
	s, _, err := q.Sealed()
	if err != nil || s.ID != r.ID {
		t.Fatalf("sealed revision %v, was %s (%v)", s, r.ID, err)
	}
	if a, _ := s.Aliases(); string(a) != testAliases {
		t.Fatal("the alias file did not survive the move")
	}
	if moved, err := Migrate(dir); err != nil || moved {
		t.Fatalf("a second Migrate must do nothing: %v, %v", moved, err)
	}
}

func TestMigrate_RefusesBytesThatDoNotMatchTheirName(t *testing.T) {
	tdir, qdir := templateSource(t, "")
	dir := filepath.Join(t.TempDir(), "vok")
	if _, _, err := BuildTemplates(dir, "vok", "1", tdir, qdir, newKey(t), Options{Clock: fixedClock()}); err != nil {
		t.Fatal(err)
	}
	toOldLayout(t, dir)
	files, _ := os.ReadDir(filepath.Join(dir, "blobs", "sha256"))
	os.WriteFile(filepath.Join(dir, "blobs", "sha256", files[0].Name()), []byte("tampered"), 0o644)
	if _, err := Migrate(dir); err == nil || !strings.Contains(err.Error(), "holds bytes with hash") {
		t.Fatalf("want a hash refusal, got %v", err)
	}
}

// A repository keeps public keys by tier folder; a package's readers find them at any depth.
func TestKeysIn_ReadsTierFolders(t *testing.T) {
	dir := t.TempDir()
	k := newKey(t)
	p := &Package{Dir: dir, key: k}
	os.MkdirAll(filepath.Join(dir, KeyDir, "people"), 0o755)
	if err := p.writeKey(); err != nil {
		t.Fatal(err)
	}
	id, _ := p.keyID()
	os.Rename(filepath.Join(dir, KeyDir, id+".pub"), filepath.Join(dir, KeyDir, "people", "ana.pub"))
	pubs, err := KeysIn(filepath.Join(dir, KeyDir))
	if err != nil || len(pubs) != 1 {
		t.Fatalf("KeysIn = %d keys, %v", len(pubs), err)
	}
}
