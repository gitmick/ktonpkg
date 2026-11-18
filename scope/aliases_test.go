package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testAliases = `{"schema":"https://kton.dev/aliases/v0","prefixes":{"qa":"https://kton.dev/v/qa/"},` +
	`"terms":{"reviewed":"qa:reviewed"},"templates":{"review":"qa/review"}}`

func templateSource(t *testing.T, aliases string) (string, string) {
	t.Helper()
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "t"), 0o755)
	os.MkdirAll(filepath.Join(src, "q"), 0o755)
	os.WriteFile(filepath.Join(src, "t", "review.json"), []byte(`{"name":"qa/review"}`), 0o644)
	os.WriteFile(filepath.Join(src, "q", "open.rq"), []byte("SELECT * WHERE { ?s ?p ?o }"), 0o644)
	if aliases != "" {
		os.WriteFile(filepath.Join(src, "t", AliasFile), []byte(aliases), 0o644)
	}
	return filepath.Join(src, "t"), filepath.Join(src, "q")
}

// A vocabulary package carries its alias file as its own part: it is not a template named
// "aliases", and Aliases returns its bytes.
func TestBuildTemplates_CarriesTheAliasFile(t *testing.T) {
	tdir, qdir := templateSource(t, testAliases)
	p, _, err := BuildTemplates(filepath.Join(t.TempDir(), "vok"), "vok", "1", tdir, qdir, newKey(t), Options{Clock: fixedClock()})
	if err != nil {
		t.Fatal(err)
	}
	if probs := p.Verify(); len(probs) > 0 {
		t.Fatal(probs)
	}
	r, _, err := p.Sealed()
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := r.Templates()
	if _, ok := ts["aliases"]; ok || len(ts) != 1 {
		t.Fatalf("the alias file must not be a template: %v", Names(ts))
	}
	a, err := r.Aliases()
	if err != nil || string(a) != testAliases {
		t.Fatalf("Aliases = %q, %v", a, err)
	}
}

func TestBuildTemplates_WithoutAliasFileAliasesIsNil(t *testing.T) {
	tdir, qdir := templateSource(t, "")
	p, _, err := BuildTemplates(filepath.Join(t.TempDir(), "vok"), "vok", "1", tdir, qdir, newKey(t), Options{Clock: fixedClock()})
	if err != nil {
		t.Fatal(err)
	}
	r, _, _ := p.Sealed()
	if a, err := r.Aliases(); err != nil || a != nil {
		t.Fatalf("Aliases = %q, %v; want nil", a, err)
	}
}

// An alias file that does not parse as kton.dev/aliases/v0 is refused at build time.
func TestBuildTemplates_RefusesABrokenAliasFile(t *testing.T) {
	for name, body := range map[string]string{
		"not json":       `{"prefixes":`,
		"other schema":   `{"schema":"https://example.org/aliases/v9"}`,
		"term not text":  `{"terms":{"reviewed":42}}`,
		"prefixes array": `{"prefixes":["qa"]}`,
		"empty prefix":   `{"prefixes":{"":"https://kton.dev/v/"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			tdir, qdir := templateSource(t, body)
			_, _, err := BuildTemplates(filepath.Join(t.TempDir(), "vok"), "vok", "1", tdir, qdir, newKey(t), Options{Clock: fixedClock()})
			if err == nil || !strings.Contains(err.Error(), "alias") {
				t.Fatalf("want an alias error, got %v", err)
			}
		})
	}
}

// Verify finds a broken alias file laid in by hand, and a second alias file in one revision.
func TestVerify_ChecksTheAliasFile(t *testing.T) {
	tdir, qdir := templateSource(t, "")
	p, err := Create(filepath.Join(t.TempDir(), "vok"), "vok", newKey(t), Options{Clock: fixedClock()})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := p.NewRevision("1")
	r.SetProfile(ProfileTemplates)
	body, _ := os.ReadFile(filepath.Join(tdir, "review.json"))
	r.AddTemplate("review", body)
	_ = qdir
	h, _ := p.PutBlob([]byte(`{"terms":{"x":1}}`))
	r.Claim(h, PartOf, member(RoleAliases, nil))
	r.AddAliases([]byte(testAliases))
	probs := strings.Join(p.Verify(), "\n")
	if !strings.Contains(probs, "alias file") || !strings.Contains(probs, "more than one alias file") {
		t.Fatalf("Verify must report the broken and the second alias file: %s", probs)
	}
	if _, err := r.Aliases(); err == nil {
		t.Fatal("Aliases must refuse a revision with two alias files")
	}
}

// A verticle's templates/aliases.json is the same alias part, not a template.
func TestBuildVerticle_CarriesTheAliasFile(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "templates"), 0o755)
	os.WriteFile(filepath.Join(src, "templates", "review.json"), []byte(`{"name":"qa/review"}`), 0o644)
	os.WriteFile(filepath.Join(src, "templates", AliasFile), []byte(testAliases), 0o644)
	p, r, err := BuildVerticle(filepath.Join(t.TempDir(), "v"), "v", "1", src, newKey(t), Options{Clock: fixedClock()})
	if err != nil {
		t.Fatal(err)
	}
	if probs := p.Verify(); len(probs) > 0 {
		t.Fatal(probs)
	}
	ts, _ := r.Templates()
	a, _ := r.Aliases()
	if _, ok := ts["aliases"]; ok || string(a) != testAliases {
		t.Fatalf("templates %v, aliases %q", Names(ts), a)
	}
}
