package scope

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg"
	"kton.dev/plankton/core"
)

// kton-review K1: a relabelled envelope names a key that never signed; Signer must not repeat it.
func TestClaimInfo_SignerIsTheKeyThatVerifies(t *testing.T) {
	key := newKey(t)
	dir := filepath.Join(t.TempDir(), "render")
	if _, _, err := BuildVerticle(dir, "render", "v1", uiSrc(t, nil), key, Options{Clock: fixedClock()}); err != nil {
		t.Fatal(err)
	}
	real := core.KeyIDHex(key.Public().(ed25519.PublicKey))
	p, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, seal, err := p.Sealed(); err != nil || seal.Signer != real {
		t.Fatalf("an honest seal is signed by %s, got %q (%v)", real, seal.Signer, err)
	}

	// Relabel every envelope to another key's id and put that key's .pub beside the real one.
	other := newKey(t).Public().(ed25519.PublicKey)
	if err := p.AddKey(other); err != nil {
		t.Fatal(err)
	}
	relabel(t, filepath.Join(dir, NektonDir), real, core.KeyIDHex(other))
	if _, seal, err := p.Sealed(); err != nil || seal.Signer != real {
		t.Fatalf("a relabelled seal is still signed by %s only, got %q (%v)", real, seal.Signer, err)
	}

	// Without the real key in keys/, no key verifies: Signer is empty.
	if err := os.Remove(filepath.Join(dir, KeyDir, real+".pub")); err != nil {
		t.Fatal(err)
	}
	if _, seal, err := p.Sealed(); err != nil || seal.Signer != "" {
		t.Fatalf("no carried key verifies, Signer must be empty, got %q (%v)", seal.Signer, err)
	}
}

func relabel(t *testing.T, dir, from, to string) {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(b), from) {
			return err
		}
		n++
		return os.WriteFile(path, []byte(strings.ReplaceAll(string(b), from, to)), 0o644)
	})
	if err != nil || n == 0 {
		t.Fatalf("relabel: %d files, %v", n, err)
	}
}

func TestRevision_MembersAndMemberBytes(t *testing.T) {
	r, err := buildUI(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := r.Members()
	if err != nil {
		t.Fatal(err)
	}
	var comp *Member
	for i, m := range ms {
		if !hashPattern.MatchString(m.Hash) {
			t.Fatalf("member %+v: hash not sha256:<lower hex>", m)
		}
		if i > 0 && (ms[i-1].Role > m.Role || ms[i-1].Role == m.Role && ms[i-1].Name > m.Name) {
			t.Fatalf("members not sorted at %d: %+v after %+v", i, m, ms[i-1])
		}
		if m.Role == RoleComposition && m.Name == "main" {
			comp = &ms[i]
		}
	}
	if comp == nil {
		t.Fatalf("composition main missing from %+v", ms)
	}
	body, err := r.MemberBytes(comp.Hash)
	if err != nil || core.HashBytes(body) != comp.Hash {
		t.Fatalf("MemberBytes(main): %v", err)
	}
	if _, err := r.MemberBytes(strings.TrimPrefix(comp.Hash, "sha256:")); err == nil {
		t.Fatal("a bare hex is not a member hash")
	}
	stray, err := r.P.PutBlob([]byte("in blobs/, not a part"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.MemberBytes(stray); err == nil || !strings.Contains(err.Error(), "is not a part of") {
		t.Fatalf("a blob no isPartOf names must be refused, got %v", err)
	}
}

func TestRevision_ViewerTreeProblems(t *testing.T) {
	tree := map[string][]byte{
		"viewer.json": []byte(`{"viewer":"table","emits":{"select":{"type":"iri"},"focus":{"type":"hash"}},` +
			`"accepts":{"columns":{"set":"literal"},"mask":{"set":"literal"},"focus":"iri"}}`),
		"index.js": []byte("export function mount(el){}\n"),
	}
	id, err := ktonpkg.TreeIDOf(tree)
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	block := func(at, tree string) map[string]string {
		return map[string]string{"ui/blocks/table.json": `{"viewer":{"viewer":"table","at":"` + at + `","tree":"` + tree + `"},
			"requires":["query@1"],"options":{"columns":{"set":"literal"},"mask":{"set":"literal"},"focus":"iri"},"emits":{"select":"iri","focus":"hash"}}`}
	}
	r, err := buildUI(t, block(commit, id))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.ViewerTreeProblems("table", tree); len(got) != 0 {
		t.Fatalf("a fetched tree that matches its pin has no problems: %v", got)
	}
	changed := map[string][]byte{"viewer.json": tree["viewer.json"], "index.js": []byte("// changed\n")}
	noFocus := map[string][]byte{"index.js": tree["index.js"],
		"viewer.json": []byte(`{"viewer":"table","emits":{"select":{"type":"iri"}},"accepts":{"columns":{"set":"literal"},"mask":{"set":"literal"},"focus":"iri"}}`)}
	misnamed := map[string][]byte{"index.js": tree["index.js"], "viewer.json": []byte(`{"name":"table"}`)}
	for _, c := range []struct {
		name   string
		r      *Revision
		viewer string
		files  map[string][]byte
		want   string
	}{
		{"files differ from the pin", r, "table", changed, "the fetched files have the tree"},
		{"event not reported", r, "table", noFocus, `emits focus, which the viewer "table" does not report`},
		{"viewer.json without viewer", r, "table", misnamed, `viewer.json says viewer ""`},
		{"no viewer.json", r, "table", map[string][]byte{"index.js": tree["index.js"]}, "has no viewer.json"},
		{"viewer not pinned", r, "other", tree, `pins the viewer "other"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := strings.Join(c.r.ViewerTreeProblems(c.viewer, c.files), "\n")
			if !strings.Contains(got, c.want) {
				t.Fatalf("want %q, got %q", c.want, got)
			}
		})
	}
	// The default fixture pins table at "kton-web@1": not a commit.
	r2, err := buildUI(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r2.ViewerTreeProblems("table", tree), "\n"); !strings.Contains(got, "40-hex kton-web commit") {
		t.Fatalf("an at that is not a commit must be reported, got %q", got)
	}
}
