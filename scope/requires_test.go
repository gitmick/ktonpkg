package scope

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSource_GivenIsRecordedAndRead(t *testing.T) {
	src := Source{Repo: "https://github.com/example/pkgs.git", Commit: strings.Repeat("a", 40), Path: "qm/core", Dirty: true}
	_, r, err := BuildVerticle(filepath.Join(t.TempDir(), "p"), "render", "v1", uiSrc(t, nil), newKey(t),
		Options{Clock: fixedClock(), Source: &src})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Source()
	if err != nil || got == nil || *got != src {
		t.Fatalf("Source: %+v %v", got, err)
	}
	if err := r.SetSource(src); err == nil || !strings.Contains(err.Error(), "already names its source") {
		t.Fatalf("a second source must be refused: %v", err)
	}
}

func TestSource_OutsideGitNamesNone(t *testing.T) {
	r, err := buildUI(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s, err := r.Source(); err != nil || s != nil {
		t.Fatalf("a folder outside git names no source: %+v %v", s, err)
	}
}

func TestSource_Problems(t *testing.T) {
	c := strings.Repeat("b", 40)
	for _, x := range []struct {
		s    Source
		want string
	}{
		{Source{Commit: "abc"}, "not a git object id"},
		{Source{Commit: strings.ToUpper(c)}, "not a git object id"},
		{Source{Commit: c, Path: "../x"}, "not a clean path"},
		{Source{Commit: c, Path: "/abs"}, "not a clean path"},
		{Source{Commit: c, Path: "a//b"}, "not a clean path"},
		{Source{Commit: c, Repo: "https://user:tok@github.com/x/y.git"}, "carries a user or password"},
	} {
		if got := strings.Join(x.s.Problems(), "; "); !strings.Contains(got, x.want) {
			t.Errorf("%+v: got %q, want %q", x.s, got, x.want)
		}
	}
	if p := (Source{Commit: strings.Repeat("c", 64), Path: "a/b"}).Problems(); len(p) != 0 {
		t.Errorf("a sha256 repository's commit and a clean path are fine: %v", p)
	}
}

func TestGitSource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.invalid",
			"-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	src := uiSrc(t, nil)
	dst := filepath.Join(repo, "pkgs", "render")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		// another file system: copy instead
		files, err := readTree(src)
		if err != nil {
			t.Fatal(err)
		}
		for rel, b := range files {
			p := filepath.Join(dst, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if s, err := GitSource(dst); err != nil || s != nil {
		t.Fatalf("a repository without a commit names nothing: %+v %v", s, err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "one")
	git("remote", "add", "origin", "https://someone:secret@git.example.invalid/team/pkgs.git")
	head := git("rev-parse", "HEAD")

	s, err := GitSource(dst)
	if err != nil || s == nil {
		t.Fatalf("GitSource: %+v %v", s, err)
	}
	want := Source{Repo: "https://git.example.invalid/team/pkgs.git", Commit: head, Path: "pkgs/render"}
	if *s != want {
		t.Fatalf("GitSource: %+v, want %+v", *s, want)
	}

	// a change elsewhere in the repository does not make this folder dirty; one inside it does
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, _ := GitSource(dst); s.Dirty {
		t.Fatal("a change outside the source folder is not the folder's")
	}
	if err := os.WriteFile(filepath.Join(dst, "content", "new.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, _ := GitSource(dst); !s.Dirty {
		t.Fatal("an untracked file in the source folder makes it dirty")
	}

	// BuildVerticle records it by itself
	_, r, err := BuildVerticle(filepath.Join(t.TempDir(), "p"), "render", "v1", dst, newKey(t), Options{Clock: fixedClock()})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Source()
	if err != nil || got == nil || got.Commit != head || got.Path != "pkgs/render" || !got.Dirty || strings.Contains(got.Repo, "secret") {
		t.Fatalf("the build's source: %+v %v", got, err)
	}
}

func TestWithoutCredentials(t *testing.T) {
	for in, want := range map[string]string{
		"https://u:p@github.com/a/b.git": "https://github.com/a/b.git",
		"ssh://git@github.com/a/b.git":   "ssh://github.com/a/b.git",
		"git@github.com:a/b.git":         "github.com:a/b.git",
		"https://github.com/a/b.git":     "https://github.com/a/b.git",
		"/srv/git/pkgs.git":              "/srv/git/pkgs.git",
	} {
		if got := withoutCredentials(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

func TestPackageRequires(t *testing.T) {
	ownerDir := filepath.Join(t.TempDir(), "runs")
	src := uiSrc(t, map[string]string{"controls/rerun-request.json": `{"kind":"record","say":{"template":"reads"}}`})
	if _, _, err := BuildVerticle(ownerDir, "dryrun1.runs", "v7", src, newKey(t), Options{Clock: fixedClock()}); err != nil {
		t.Fatal(err)
	}
	op, err := Open(ownerDir)
	if err != nil {
		t.Fatal(err)
	}
	owner, seal, err := op.Sealed()
	if err != nil {
		t.Fatal(err)
	}
	head := str(seal.Object["hash"])

	use := `{"use":"table","query":"card","params":{"item":"var.picked"},"empty":"show"}`
	act := `{"intent":"record","control":"rerun-request","from":{"package":"dryrun1.runs","at":"v7","head":"` + head +
		`"},"fields":{"document":"var.picked"}}`
	withAct := strings.Replace(readMain(t), use, strings.TrimSuffix(use, "}")+`,"acts":[`+act+`]}`, 1)
	build := func(requires string) (*Revision, error) {
		edit := map[string]string{"ui/compositions/main.json": withAct}
		if requires != "" {
			edit[RequiresFile] = requires
		}
		return buildUI(t, edit)
	}

	r, err := build(`{"packages":[
		{"package":"qm.core","why":"the org chart"},
		{"package":"dryrun1.runs","at":"v7","head":"` + head + `","why":"rerun-request"}]}`)
	if err != nil {
		t.Fatalf("requires that agree with the control's pin must build: %v", err)
	}
	qs, err := r.PackageRequires()
	if err != nil || len(qs) != 2 || qs[0].Package != "dryrun1.runs" || qs[0].Head != head || qs[1].Package != "qm.core" ||
		qs[1].Why != "the org chart" {
		t.Fatalf("PackageRequires: %+v %v", qs, err)
	}
	refs, err := r.PackageReferences()
	if err != nil || len(refs) != 1 || refs[0].Package != "dryrun1.runs" || refs[0].What != "control" ||
		refs[0].Name != "rerun-request" || refs[0].At != "v7" {
		t.Fatalf("PackageReferences: %+v %v", refs, err)
	}
	if p := PackageRequireProblems(qs[0], owner, seal); len(p) != 0 {
		t.Fatalf("the installed owner meets the require: %v", p)
	}
	if p := PackageRequireProblems(qs[1], nil, ClaimInfo{}); len(p) != 1 || !strings.Contains(p[0], "not installed") {
		t.Fatalf("a missing package: %v", p)
	}
	if p := PackageRequireProblems(PackageRequire{Package: "dryrun1.runs", At: "v8"}, owner, seal); len(p) != 1 ||
		!strings.Contains(p[0], "requires dryrun1.runs@v8; the installed revision is v7") {
		t.Fatalf("another revision: %v", p)
	}
	if p := PackageRequireProblems(PackageRequire{Package: "dryrun1.runs"}, owner, seal); len(p) != 0 {
		t.Fatalf("without at any revision will do: %v", p)
	}

	// the tool requirements of the view are not confused by a package require
	if _, err := r.WriteViewTo(t.TempDir()); err != nil {
		t.Fatalf("WriteView: %v", err)
	}

	other := "sha256:" + strings.Repeat("e", 64)
	for _, c := range []struct{ name, requires, want string }{
		{"itself", `{"packages":[{"package":"render"}]}`, "does not require itself"},
		{"twice", `{"packages":[{"package":"a.b"},{"package":"a.b"}]}`, `requires "a.b" twice`},
		{"head without at", `{"packages":[{"package":"a.b","head":"` + other + `"}]}`, "needs at"},
		{"bad head", `{"packages":[{"package":"a.b","at":"v1","head":"sha256:X"}]}`, "is not sha256:"},
		{"bad name", `{"packages":[{"package":"../x"}]}`, "not a package name"},
		{"unknown field", `{"packages":[{"package":"a.b","version":"1"}]}`, "unknown field"},
		{"other revision than the control", `{"packages":[{"package":"dryrun1.runs","at":"v6"}]}`,
			"the control is pinned at dryrun1.runs@v7; requires pins dryrun1.runs@v6"},
		{"other head than the control", `{"packages":[{"package":"dryrun1.runs","at":"v7","head":"` + other + `"}]}`,
			"requires pins " + other},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := build(c.requires); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
}
