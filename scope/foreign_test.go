package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A view's act may say another package's control, pinned to its sealed revision (package, at, head).
func TestForeignControls(t *testing.T) {
	// The owner: a package with a record control rerun-request over the template reads.
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
	view := func(act string) map[string]string {
		b := strings.Replace(readMain(t), use, `{"use":"table","query":"card","params":{"item":"var.picked"},"empty":"show","acts":[`+act+`]}`, 1)
		return map[string]string{"ui/compositions/main.json": b}
	}
	act := func(intent, control, at, h, fields string) string {
		return `{"intent":"` + intent + `","control":"` + control + `","from":{"package":"dryrun1.runs","at":"` + at + `","head":"` + h + `"},"fields":{` + fields + `}}`
	}
	good := act("record", "rerun-request", "v7", head, `"document":"var.picked"`)
	r, err := buildUI(t, view(good))
	if err != nil {
		t.Fatalf("a view with a foreign control must build: %v", err)
	}
	fcs, err := r.ForeignControls()
	if err != nil || len(fcs) != 1 || fcs[0].Control != "rerun-request" || fcs[0].From.Head != head || fcs[0].Fields[0] != "document" {
		t.Fatalf("ForeignControls: %+v %v", fcs, err)
	}
	if got := ForeignControlProblems(fcs[0], owner, seal); len(got) != 0 {
		t.Fatalf("a pin that matches the owner has no problems: %v", got)
	}

	// The host's checks against the owner.
	other := strings.Repeat("e", 64)
	for _, c := range []struct {
		name, act, want string
	}{
		{"other head", act("record", "rerun-request", "v7", "sha256:"+other, `"document":"var.picked"`), "from pins the head sha256:" + other},
		{"other revision", act("record", "rerun-request", "v8", head, `"document":"var.picked"`), "from pins dryrun1.runs@v8; the sealed revision is v7"},
		{"no such control", act("record", "rerun", "v7", head, `"document":"var.picked"`), `the control "rerun" is not in dryrun1.runs@v7`},
		{"wrong kind", act("sign", "rerun-request", "v7", head, `"document":"var.picked"`), "a sign act needs a decision control"},
		{"unknown field", act("record", "rerun-request", "v7", head, `"why":"var.picked"`), "field why is not a field of the control's template"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, err := buildUI(t, view(c.act))
			if err != nil {
				t.Fatalf("the form is fine, so it builds: %v", err)
			}
			fcs, _ := r.ForeignControls()
			if got := strings.Join(ForeignControlProblems(fcs[0], owner, seal), "\n"); !strings.Contains(got, c.want) {
				t.Fatalf("want %q, got %q", c.want, got)
			}
		})
	}
	if got := ForeignControlProblems(fcs[0], nil, ClaimInfo{}); len(got) != 1 || !strings.Contains(got[0], "is not installed") {
		t.Fatalf("an owner not installed: %v", got)
	}

	// The form, at build.
	for _, c := range []struct {
		name string
		act  string
		want string
	}{
		{"bad head", act("record", "rerun-request", "v7", "abc", ``), `from.head "abc" is not sha256`},
		{"bad label", act("record", "rerun-request", "v 7", head, ``), `from.at "v 7" is not a revision label`},
		{"no control", act("record", "", "v7", head, ``), "names no control"},
		{"itself", `{"intent":"record","control":"log","from":{"package":"render","at":"v1","head":"` + head + `"}}`, "from names this package"},
		{"a run", `{"intent":"run","ray":"render","from":{"package":"dryrun1.runs","at":"v7","head":"` + head + `"}}`, "from is for another package's control"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildUI(t, view(c.act))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want a refusal with %q, got %v", c.want, err)
			}
		})
	}
}

func readMain(t *testing.T) string {
	t.Helper()
	return mainJSON(uiSrc(t, nil), t)
}

func mainJSON(src string, t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(src, "ui/compositions/main.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
