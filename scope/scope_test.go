package scope

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gitmick/ktonpkg"
	pregistry "kton.dev/plankton/registry"
)

func fixedClock() func() time.Time {
	t := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	return func() time.Time { t = t.Add(time.Second); return t }
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// miniBundle ist ein kleiner Workflow im alten Format: prep kopiert einen Datensatz (Dateiloch),
// count nimmt dessen erste N Zeilen (Parameterloch) — dieselbe Gestalt wie die Raute, nur kürzer.
func miniBundle(t *testing.T) *ktonpkg.Bundle {
	t.Helper()
	dir := t.TempDir()
	b := ktonpkg.New(dir, ktonpkg.KindRay, "mini")
	prep, _ := b.AddFile("steps/prep/prep.sh", []byte("cp data.csv daten.txt\n"))
	count, _ := b.AddFile("steps/count/count.sh", []byte("head -n \"$1\" daten.txt > out.txt\n"))
	if _, err := b.AddFile("testdaten/data.csv", []byte("a\nb\nc\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddFile("requirements/URS-1.md", []byte("# URS-1\nZählen.\n")); err != nil {
		t.Fatal(err)
	}
	b.Ray.Steps = []ktonpkg.Step{
		{ID: "prep", Label: "Vorbereitung", Rationale: "die Daten übernehmen",
			Protocol: ktonpkg.Protocol{Command: &ktonpkg.Command{Args: "img@sha256:00\n<command-file>"}},
			Inputs: []ktonpkg.Input{
				{Slot: "prep.sh", Path: "steps/prep/prep.sh", Hash: prep, Variable: "command-file"},
				{Slot: "data.csv", From: "hole:data"},
			}, Outputs: []string{"daten.txt"}},
		{ID: "count",
			Protocol: ktonpkg.Protocol{Command: &ktonpkg.Command{Args: "img@sha256:00\n<command-file>\n<n>"}},
			Params:   map[string]string{"n": "hole:n"},
			Inputs: []ktonpkg.Input{
				{Slot: "count.sh", Path: "steps/count/count.sh", Hash: count, Variable: "command-file"},
				{Slot: "daten.txt", From: "upstream:prep:daten.txt"},
			}, Outputs: []string{"out.txt"}},
	}
	b.Ray.Holes = []ktonpkg.Hole{
		{Name: "data", Step: "prep", Slot: "data.csv", Type: ktonpkg.HoleFile, Required: true, Default: "testdaten/data.csv"},
		{Name: "n", Step: "count", Param: "n", Type: ktonpkg.HoleParam, Required: true, Default: "2"},
	}
	b.Requires = []ktonpkg.Requirement{{Kind: "tool", Spectrum: "sha256:" + strings.Repeat("e", 64), Name: "tool"}}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	if p := b.Verify(); len(p) > 0 {
		t.Fatalf("das alte Paket selbst ist nicht in Ordnung: %v", p)
	}
	return b
}

func convert(t *testing.T) (*ktonpkg.Bundle, *Package, *Revision, ed25519.PrivateKey) {
	t.Helper()
	b := miniBundle(t)
	key := newKey(t)
	p, r, err := FromBundle(b, filepath.Join(t.TempDir(), "mini"), "rev-1", key, Options{Clock: fixedClock()})
	if err != nil {
		t.Fatal(err)
	}
	return b, p, r, key
}

func TestFromBundle_TheConvertedPackageIsStructurallySound(t *testing.T) {
	_, p, _, _ := convert(t)
	if problems := p.Verify(); len(problems) > 0 {
		t.Fatalf("Verify: %v", problems)
	}
	again, err := Open(p.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != p.ID || again.Name != "mini" {
		t.Fatalf("Open liest ein anderes Paket: %s %q", again.ID, again.Name)
	}
	revs, err := again.Revisions()
	if err != nil || len(revs) != 1 || revs[0].Label != "rev-1" {
		t.Fatalf("eine Revision rev-1 erwartet, bekam %v (%v)", revs, err)
	}
}

// Die Berechnung bleibt dieselbe: der zurückgelesene Ray hat dieselbe Ray-Id und dieselben
// Potentiale wie der ursprüngliche — die Identität wandert mit, sie wird nicht neu erfunden.
func TestFromBundle_TheRayReadBackIsTheSameComputation(t *testing.T) {
	b, p, _, _ := convert(t)
	r, err := p.Revision("rev-1")
	if err != nil {
		t.Fatal(err)
	}
	back, err := r.Ray()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := b.Ray.ID()
	got, err := back.ID()
	if err != nil || got != want {
		t.Fatalf("Ray-Id %s, erwartet %s (%v)", got, want, err)
	}
	for _, s := range b.Ray.Steps {
		w, _ := b.Ray.FotonID(s.ID, nil, nil)
		g, _ := back.FotonID(s.ID, nil, nil)
		if w != g {
			t.Errorf("%s: Potential %s, erwartet %s", s.ID, g, w)
		}
	}
	if s := back.Step("prep"); s.Label != "Vorbereitung" || s.Rationale != "die Daten übernehmen" {
		t.Errorf("Beschriftung und Begründung müssen als Claims mitreisen, bekam %q / %q", s.Label, s.Rationale)
	}
	if h := back.Hole("n"); h == nil || h.Default != "2" {
		t.Errorf("die Vorgabe des Parameterlochs fehlt: %+v", h)
	}
}

// Die lesbare Ansicht ist das gewohnte Ordnerformat; die alte Prüfung besteht sie.
func TestWriteView_IsReadableByTheOldTools(t *testing.T) {
	b, _, r, _ := convert(t)
	dir, err := r.WriteView()
	if err != nil {
		t.Fatal(err)
	}
	view, err := ktonpkg.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p := view.Verify(); len(p) > 0 {
		t.Fatalf("die Ansicht besteht die Prüfung des alten Formats nicht: %v", p)
	}
	want, _ := b.Ray.ID()
	if got, _ := view.Ray.ID(); got != want {
		t.Fatalf("die Ansicht beschreibt einen anderen Ray: %s gegen %s", got, want)
	}
	if len(view.Requires) != 1 {
		t.Fatalf("die Bedingung fehlt in der Ansicht: %+v", view.Requires)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal("die Ansicht erklärt sich nicht")
	}
}

// Siegel sind frei: mehrere, auch Zwischensiegel. Was sie bedeuten, entscheidet das Profil.
func TestSeal_AnyNumberOfSealsEachNamingAHeadOfItsRevision(t *testing.T) {
	_, p, r, key := convert(t)
	p.WithSigner(key, Options{Clock: fixedClock()})
	r.P = p
	if _, err := r.Seal(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Claim(p.ID, Description, map[string]any{"value": "nach dem ersten Siegel"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Seal(); err != nil {
		t.Fatal(err)
	}
	r2, err := p.NewRevision("rev-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r2.Seal(); err != nil {
		t.Fatal(err)
	}
	seals, err := p.Seals()
	if err != nil || len(seals) != 3 {
		t.Fatalf("drei Siegel erwartet, bekam %d (%v)", len(seals), err)
	}
	if problems := p.Verify(); len(problems) > 0 {
		t.Fatalf("Verify: %v", problems)
	}
}

func TestVerify_ReportsTamperedBytesAndForeignSignatures(t *testing.T) {
	_, p, r, _ := convert(t)
	claims, _ := r.Claims()
	var blob string
	for _, c := range claims {
		if c.Predicate == PartOf && str(value(c)["role"]) == RoleCode {
			blob = c.Subject
		}
	}
	if err := os.WriteFile(p.blobPath(blob), []byte("etwas anderes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Ein Claim mit einem Schlüssel, der nicht beiliegt.
	stranger := &Package{Dir: p.Dir, ID: p.ID, Name: p.Name, key: newKey(t), clock: fixedClock()}
	if _, err := stranger.claim(r.ID, p.ID, Description, map[string]any{"value": "untergeschoben"}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(p.Verify(), "\n")
	if !strings.Contains(got, "haben den Hash") {
		t.Errorf("die veränderten Bytes werden nicht gemeldet:\n%s", got)
	}
	if !strings.Contains(got, "keinem beiliegenden Schlüssel") {
		t.Errorf("der fremd unterschriebene Claim wird nicht gemeldet:\n%s", got)
	}
}

// Die Ray-Id ist die Id des zusammengesetzten Potentials. Sie hängt an der Berechnung, nicht an
// Uhr, Schlüssel, Paketname oder Revision: zwei Pakete mit demselben Ray haben verschiedene
// Paket-Ids, aber dieselbe Ray-Id.
func TestRayID_IsThePotentialsIdAndIndependentOfThePackage(t *testing.T) {
	b, p1, r1, _ := convert(t)
	later := func() func() time.Time {
		t := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		return func() time.Time { t = t.Add(time.Minute); return t }
	}()
	p2, r2, err := FromBundle(b, filepath.Join(t.TempDir(), "andere"), "v7", newKey(t), Options{Clock: later})
	if err != nil {
		t.Fatal(err)
	}
	if p1.ID == p2.ID {
		t.Fatalf("verschiedene Pakete sollten verschiedene Paket-Ids haben")
	}
	id1, err := r1.RayID()
	if err != nil {
		t.Fatal(err)
	}
	id2, err := r2.RayID()
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("Ray-Id %s und %s: dieselbe Berechnung muss dieselbe Ray-Id haben", short(id1), short(id2))
	}
	reg, err := pregistry.Open(filepath.Join(p1.Dir, PlanktonDir))
	if err != nil {
		t.Fatal(err)
	}
	f, ok := reg.Foton(id1)
	if !ok || f.Protocol.Kind != RayKind {
		t.Fatalf("die Ray-Id muss ein Potential der Art %s im Register sein", RayKind)
	}
}
