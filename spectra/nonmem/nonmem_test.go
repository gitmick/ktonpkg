package nonmem

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kton.dev/plankton/core"
)

// The ref is what cockpit-improve-backend records and what the spectrum names. It changes only when
// the script, its arguments or the image change; then every L1 recorded under the old ref stays
// with the old normaliser, which is the point. Update it deliberately, never to make a test pass.
const wantRef = "sha256:38c3d9c95a4e7c73b19c94e1e8201d47b3e225f48df303a6ea40450035c3b1fe"

func TestRef_IsPinned(t *testing.T) {
	ref, err := Ref()
	if err != nil || ref != wantRef {
		t.Fatalf("ref %s (%v), want %s", ref, err, wantRef)
	}
}

// Every application has the normaliser's ref, whatever it normalises; the file is an input, not part
// of the protocol, and the normalised file is the first output (plankton NormalizedOutput).
func TestApplication_SharesTheRefAndTakesTheOutputAsInput(t *testing.T) {
	in, out := core.HashBytes([]byte("a")), core.HashBytes([]byte("b"))
	f, err := Application(in, out)
	if err != nil {
		t.Fatal(err)
	}
	if f.Protocol.Ref != wantRef {
		t.Errorf("application ref %s", f.Protocol.Ref)
	}
	var found bool
	for _, i := range f.Inputs {
		found = found || i.Hash == in
	}
	if !found || len(f.Outputs) != 1 || f.Outputs[0].Hash != out {
		t.Fatalf("inputs %v outputs %v", f.Inputs, f.Outputs)
	}
	other, _ := Application(core.HashBytes([]byte("c")), out)
	if other.Protocol.Ref != f.Protocol.Ref {
		t.Fatal("two applications must share one ref")
	}
}

// Two runs that differ only in date, license, elapsed and CPU time normalise to the same bytes, and a
// changed number does not. Runs the embedded script with the local sh (the package runs it in Image).
func TestScript_TwoRunsAgreeAChangedNumberDoesNot(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "normalize-nonmem.sh")
	if err := os.WriteFile(script, Script, 0o644); err != nil {
		t.Fatal(err)
	}
	norm := func(in string) string {
		out := filepath.Join(dir, "out")
		if b, err := exec.Command("sh", script, in, out).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", in, err, b)
		}
		b, _ := os.ReadFile(out)
		return string(b)
	}
	for _, ext := range []string{"lst", "xml", "cpu"} {
		a, b := filepath.Join("testdata", "a."+ext), filepath.Join("testdata", "b."+ext)
		if raw1, raw2 := read(t, a), read(t, b); raw1 == raw2 {
			t.Fatalf(".%s: the fixtures must differ before normalising", ext)
		}
		if na, nb := norm(a), norm(b); na != nb {
			t.Errorf(".%s: runs differ after normalising:\n%s\n---\n%s", ext, na, nb)
		}
		if ext == "cpu" {
			continue
		}
		changed := filepath.Join(dir, "changed."+ext)
		os.WriteFile(changed, []byte(strings.Replace(read(t, a), "133.202254503871", "133.202254503872", 1)), 0o644)
		if norm(changed) == norm(a) {
			t.Errorf(".%s: a changed objective function value must not normalise away", ext)
		}
	}
}

// A run reproduced on another day differs in the listing's "Current Date:" line only (cockpit-runtime,
// iter-mh1 gof_run1; MH 18.11.). That line is blanked like the other dates.
func TestScript_CurrentDateLineIsBlanked(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "normalize-nonmem.sh")
	if err := os.WriteFile(script, Script, 0o644); err != nil {
		t.Fatal(err)
	}
	norm := func(in string) string {
		out := filepath.Join(dir, "out")
		if b, err := exec.Command("sh", script, in, out).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", in, err, b)
		}
		b, _ := os.ReadFile(out)
		return string(b)
	}
	a, b := filepath.Join("testdata", "date-a.lst"), filepath.Join("testdata", "date-b.lst")
	if read(t, a) == read(t, b) {
		t.Fatal("the fixtures must differ before normalising")
	}
	if na, nb := norm(a), norm(b); na != nb || na != "Current Date: <date>\n" {
		t.Fatalf("normalised %q and %q, want both %q", na, nb, "Current Date: <date>\n")
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
