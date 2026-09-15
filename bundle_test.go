package ktonpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kton.dev/plankton/core"
)

// Die Prüfungen gehen von den beiden Beispielen aus, an denen das Format
// entstanden ist: der **Raute** und dem **Report**. Was dort nicht gebraucht
// wird, steht nicht im Format.

// raute baut das Paket der Raute: vier Skripte, ein Dateiloch am Anfang, ein
// Zahlparameter im Zweig B.
func raute(t *testing.T) *Bundle {
	t.Helper()
	b := New(t.TempDir(), KindRay, "raute")

	hash := map[string]string{}
	for name, body := range map[string]string{
		"scripts/aufbereiten.sh": "#!/bin/sh\ncat dataset.csv > daten.txt\n",
		"scripts/zweigB.sh":      "#!/bin/sh\nsed 's/^/B: /' daten.txt | head -n \"$1\" > b.txt\n",
		"scripts/zweigC.sh":      "#!/bin/sh\nsed 's/^/C: /' daten.txt > c.txt\n",
		"scripts/zusammen.sh":    "#!/bin/sh\ncat b.txt c.txt > ergebnis.txt\n",
		// Der Testdatensatz gehört ins Paket: ohne ihn läuft der Workflow
		// beim Anwender nicht, und ohne Lauf gibt es kein OQ.
		"testdaten/beispiel.csv": "probe,wert\nP-1,12.4\nP-2,11.8\n",
	} {
		h, err := b.AddFile(name, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		hash[name] = h
	}
	cmd := func(path string) Input {
		return Input{Slot: "run.sh", Path: path, Hash: hash[path], Variable: "command-file"}
	}
	b.Ray.Steps = []Step{
		{ID: "aufbereitung", Label: "Aufbereitung",
			Protocol: Protocol{Command: &Command{Args: "sh <command-file>"}},
			Inputs: []Input{cmd("scripts/aufbereiten.sh"),
				{Slot: "dataset.csv", From: FromHole + "dataset"}},
			Outputs: []string{"daten.txt"}},
		{ID: "zweig-b", Label: "Zweig B",
			Protocol: Protocol{Command: &Command{Args: "sh <command-file> <anzahl>"}},
			Params:   map[string]string{"anzahl": FromHole + "anzahl"},
			Inputs: []Input{cmd("scripts/zweigB.sh"),
				{Slot: "daten.txt", From: FromUpstream + "aufbereitung:daten.txt"}},
			Outputs: []string{"b.txt"}},
		{ID: "zweig-c", Label: "Zweig C",
			Protocol: Protocol{Command: &Command{Args: "sh <command-file>"}},
			Inputs: []Input{cmd("scripts/zweigC.sh"),
				{Slot: "daten.txt", From: FromUpstream + "aufbereitung:daten.txt"}},
			Outputs: []string{"c.txt"}},
		{ID: "zusammenführung", Label: "Zusammenführung",
			Protocol: Protocol{Command: &Command{Args: "sh <command-file>"}},
			Inputs: []Input{cmd("scripts/zusammen.sh"),
				{Slot: "b.txt", From: FromUpstream + "zweig-b:b.txt"},
				{Slot: "c.txt", From: FromUpstream + "zweig-c:c.txt"}},
			Outputs: []string{"ergebnis.txt"}},
	}
	b.Ray.Holes = []Hole{
		{Name: "dataset", Step: "aufbereitung", Slot: "dataset.csv",
			Type: HoleFile, Required: true, Default: "testdaten/beispiel.csv"},
		{Name: "anzahl", Step: "zweig-b", Param: "anzahl",
			Type: HoleParam, Required: true, Default: "1"},
	}
	return b
}

// report baut das Paket des Reports: die Nutzlast unter ihren Namen, ein
// Slot als Loch, die Rendermethode als Protokoll — ohne eigenes method.json.
func reportBundle(t *testing.T) *Bundle {
	t.Helper()
	b := New(t.TempDir(), KindReport, "durchstich")
	b.Package.Entry = "kton/render.json"

	hash := map[string]string{}
	for name, body := range map[string]string{
		"report.json":      `{"schemaVersion":"2.0.0","sections":[]}`,
		"01-daten.md":      "# Daten\n",
		"method/render.sh": "#!/bin/sh\ncat report/*.md > report.html\n",
	} {
		h, err := b.AddFile(name, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		hash[name] = h
	}
	b.Ray.Steps = []Step{{
		ID: "render", Label: "Render durchstich",
		Protocol: Protocol{Command: &Command{Args: "sh <command-file>"}},
		Inputs: []Input{
			{Slot: "run.sh", Path: "method/render.sh", Hash: hash["method/render.sh"],
				Variable: "command-file"},
			{Slot: "report.json", Path: "report.json", Hash: hash["report.json"]},
			{Slot: "01-daten.md", Path: "01-daten.md", Hash: hash["01-daten.md"]},
			{Slot: "slots/verlauf.png", From: FromHole + "verlauf"},
		},
		Outputs: []string{"report.html"},
	}}
	b.Ray.Holes = []Hole{{
		Name: "verlauf", Step: "render", Slot: "slots/verlauf.png",
		Type: HoleFile, FilledBy: "step:/Projekte/Analyse/Step 1",
	}}
	return b
}

func TestRauteWritesAndReadsBack(t *testing.T) {
	b := raute(t)
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	// Die Nutzlast liegt unter ihren Namen — das ist der Punkt.
	if _, err := os.Stat(filepath.Join(b.Dir, "scripts", "zweigB.sh")); err != nil {
		t.Fatalf("die Nutzlast muss lesbar liegen: %v", err)
	}
	// Und kton/ trägt die Records.
	for _, want := range []string{"kton/package.json", "kton/ray.json"} {
		if _, err := os.Stat(filepath.Join(b.Dir, filepath.FromSlash(want))); err != nil {
			t.Fatalf("%s fehlt: %v", want, err)
		}
	}
	back, err := Open(b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if back.Package.Kind != KindRay || len(back.Ray.Steps) != 4 || len(back.Ray.Holes) != 2 {
		t.Fatalf("Paket nicht wie geschrieben: %+v", back.Package)
	}
	if problems := back.Verify(); len(problems) != 0 {
		t.Fatalf("keine Befunde erwartet, bekam %v", problems)
	}
}

func TestVerifyCatchesATamperedPayload(t *testing.T) {
	b := raute(t)
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(b.Dir, "scripts", "zweigC.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nrm -rf /\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	back, err := Open(b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	problems := back.Verify()
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "passt nicht zu seinem Hash") {
		t.Fatalf("der verfälschte Inhalt muss auffallen, bekam %v", problems)
	}
}

func TestVerifyCatchesAMissingFileAndADanglingEdge(t *testing.T) {
	b := raute(t)
	b.Ray.Steps[2].Inputs[1].From = FromUpstream + "gibtesnicht:daten.txt"
	b.Ray.Steps[3].Inputs[0].Path = "scripts/weg.sh"
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	problems := b.Verify()
	var msgs []string
	for _, p := range problems {
		msgs = append(msgs, p.String())
	}
	joined := strings.Join(msgs, " | ")
	for _, want := range []string{"unbekannten Step", "liegt nicht im Paket"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("%q erwartet in: %s", want, joined)
		}
	}
}

func TestVerifyCatchesAnUndeclaredHole(t *testing.T) {
	b := raute(t)
	b.Ray.Holes = b.Ray.Holes[:1] // "anzahl" entfernen, Parameter bleibt stehen
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	problems := b.Verify()
	if len(problems) == 0 || !strings.Contains(problems[0].String(), "nicht erklärt") {
		t.Fatalf("das nicht erklärte Loch muss auffallen, bekam %v", problems)
	}
}

func TestPayloadMayNotHideInKton(t *testing.T) {
	b := New(t.TempDir(), KindRay, "x")
	if _, err := b.AddFile("kton/heimlich.json", []byte("{}")); err == nil {
		t.Fatal("kton/ ist für die Records — Nutzlast gehört nicht hinein")
	}
}

func TestOrderFollowsTheDiamond(t *testing.T) {
	b := raute(t)
	order, err := b.Ray.Order()
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	if pos["aufbereitung"] > pos["zweig-b"] || pos["zweig-b"] > pos["zusammenführung"] {
		t.Fatalf("Rautenordnung erwartet, bekam %v", order)
	}
}

// --- Identität ------------------------------------------------------------

func TestActionKeyNeedsEveryHoleBound(t *testing.T) {
	b := raute(t)
	_, err := b.Ray.ActionKey("aufbereitung", nil, nil)
	if err == nil {
		t.Fatal("ohne gebundenen Datensatz darf es keinen Schlüssel geben")
	}
	var pot *ErrPotential
	if !asPotential(err, &pot) || !strings.Contains(err.Error(), "dataset.csv") {
		t.Fatalf("ein Potential mit Namen erwartet, bekam %v", err)
	}
	key, err := b.Ray.ActionKey("aufbereitung",
		Bindings{"dataset": core.HashBytes([]byte("messwerte\n"))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "sha256:") {
		t.Fatalf("Schlüssel erwartet, bekam %q", key)
	}
}

func TestActionKeyChangesWithTheScalarParameter(t *testing.T) {
	b := raute(t)
	up := map[string]string{"aufbereitung:daten.txt": core.HashBytes([]byte("x\n"))}
	a, err := b.Ray.ActionKey("zweig-b", Bindings{"anzahl": "1"}, up)
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.Ray.ActionKey("zweig-b", Bindings{"anzahl": "2"}, up)
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Fatal("ein anderer Parameterwert ist eine andere Berechnung")
	}
}

// Das Paket hat eine eigene Identität — der Bezugspunkt für Claims, ohne
// dass etwas realisiert werden muss.
func TestPackageIdentityIsStableAndContentBound(t *testing.T) {
	b := raute(t)
	first, err := b.Ray.ID()
	if err != nil {
		t.Fatal(err)
	}
	again, err := b.Ray.ID()
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Fatal("dieselbe Vorlage muss dieselbe Identität haben")
	}
	// Ein anderer Skriptinhalt: anderes Paket.
	b2 := raute(t)
	h, err := b2.AddFile("scripts/zweigC.sh", []byte("#!/bin/sh\necho anders\n"))
	if err != nil {
		t.Fatal(err)
	}
	b2.Ray.Steps[2].Inputs[0].Hash = h
	changed, err := b2.Ray.ID()
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("ein anderer Inhalt muss ein anderes Paket ergeben")
	}
}

// --- Report ---------------------------------------------------------------

// Ein Slot des Reports **ist** ein Loch des Render-Potentials — nicht
// zweimal dasselbe an zwei Stellen.
func TestReportSlotIsAHole(t *testing.T) {
	b := reportBundle(t)
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	back, err := Open(b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if problems := back.Verify(); len(problems) != 0 {
		t.Fatalf("keine Befunde erwartet, bekam %v", problems)
	}
	hole := back.Ray.Hole("verlauf")
	if hole == nil || hole.Slot != "slots/verlauf.png" {
		t.Fatalf("der Slot muss als Loch dastehen, bekam %+v", hole)
	}
	// Die Herkunft ist eine Behauptung, kein Identitätsmerkmal.
	if hole.FilledBy == "" {
		t.Fatal("die Herkunft des Slots soll mitreisen")
	}
	withOrigin, err := back.Ray.ID()
	if err != nil {
		t.Fatal(err)
	}
	back.Ray.Holes[0].FilledBy = "step:/woanders/Step 9"
	withOther, err := back.Ray.ID()
	if err != nil {
		t.Fatal(err)
	}
	if withOrigin != withOther {
		t.Fatal("die Herkunft darf die Identität nicht verändern")
	}
}

// Ein Report ohne gefüllten Slot ist trotzdem ein gültiges Paket — er ist ein
// Potential, kein halbes Ding.
func TestReportWithAnEmptySlotIsValid(t *testing.T) {
	b := reportBundle(t)
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	if problems := b.Verify(); len(problems) != 0 {
		t.Fatalf("keine Befunde erwartet, bekam %v", problems)
	}
	if missing := b.Ray.MissingBindings(nil); len(missing) != 0 {
		t.Fatalf("der Slot ist nicht verpflichtend, bekam %v", missing)
	}
	if _, err := b.Ray.ActionKey("render", nil, nil); err == nil {
		t.Fatal("ohne Bild gibt es keinen Aktionsschlüssel")
	}
}

// --- Claims ---------------------------------------------------------------

func TestClaimsTravelWithThePackage(t *testing.T) {
	b := raute(t)
	id, err := b.Ray.ID()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddClaim(testSigner(t), Claim{
		Subject:   Ref{Hash: id, Name: "raute"},
		Predicate: "https://improve.scinteco.com/terms/runs-on",
		Object:    map[string]any{"runserver": "default", "tool": "bash"},
		By:        "michael", When: "2026-09-14T08:20:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	back, err := Open(b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Claims) != 1 || back.Claims[0].Claim.Subject.Hash != id {
		t.Fatalf("der Claim muss mitreisen und sein Subjekt nennen, bekam %+v", back.Claims)
	}
	// Ein Claim verändert sein Subjekt nie.
	afterID, err := back.Ray.ID()
	if err != nil {
		t.Fatal(err)
	}
	if afterID != id {
		t.Fatal("ein Claim darf die Identität des Pakets nicht verändern")
	}
}

func TestPackageWithoutClaimsIsValid(t *testing.T) {
	b := raute(t)
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	back, err := Open(b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Claims) != 0 {
		t.Fatalf("keine Claims erwartet, bekam %d", len(back.Claims))
	}
	if problems := back.Verify(); len(problems) != 0 {
		t.Fatalf("ein Paket ohne Claims ist gültig, bekam %v", problems)
	}
}

func TestUnknownFormatVersionIsRefused(t *testing.T) {
	b := raute(t)
	b.Package.FormatVersion = "kton-package/99"
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(b.Dir); err == nil {
		t.Fatal("eine unbekannte Fassung hätte abgewiesen werden müssen")
	}
}

func asPotential(err error, target **ErrPotential) bool {
	e, ok := err.(*ErrPotential)
	if ok {
		*target = e
	}
	return ok
}

// --- Werkzeugpaket ----------------------------------------------------------

// Ein Werkzeugpaket ist derselbe Umschlag: Nutzlast unter ihren Namen, die
// Records in kton/ — dazu ein Spectrum und die Referenzausgaben, auf die es
// sich bezieht.
func werkzeug(t *testing.T) *Bundle {
	t.Helper()
	b := New(t.TempDir(), KindTool, "werkzeug")
	b.Package.Entry = "kton/tool.json"

	skript, err := b.AddFile("members/01-json.R", []byte("cat('{}', file='json.out')\n"))
	if err != nil {
		t.Fatal(err)
	}
	norm, err := b.AddFile("normalizer/fences.R", []byte("# normalisiert\n"))
	if err != nil {
		t.Fatal(err)
	}
	referenz := []byte("{}")
	if _, err := b.AddFile(ReferenceDir+"/json.out", referenz); err != nil {
		t.Fatal(err)
	}

	b.Ray.Steps = []Step{
		{ID: "json", Protocol: Protocol{Command: &Command{Args: "Rscript"}, Tool: "jam-r"},
			Inputs:  []Input{{Slot: "01-json.R", Path: "members/01-json.R", Hash: skript, Variable: "command-file"}},
			Outputs: []string{"json.out"}},
		{ID: "normalisieren", Protocol: Protocol{Command: &Command{Args: "Rscript eingabe normalisiert.out"}, Tool: "jam-r"},
			Inputs: []Input{
				{Slot: "fences.R", Path: "normalizer/fences.R", Hash: norm, Variable: "command-file"},
				{Slot: "eingabe", From: FromHole + "eingabe"}},
			Outputs: []string{"normalisiert.out"}},
	}
	b.Ray.Holes = []Hole{{Name: "eingabe", Step: "normalisieren", Slot: "eingabe",
		Type: HoleFile, Required: true}}

	f, err := b.Ray.Foton("normalisieren", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.Spectrum = &Spectrum{
		Spectrum: "werkzeug", Normalizer: f.Protocol.Ref,
		Members: []SpectrumMember{{Name: "json", Output: core.HashBytes(referenz)}},
	}
	return b
}

func TestToolPackageRoundTrips(t *testing.T) {
	b := werkzeug(t)
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	back, err := Open(b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if back.Package.Kind != KindTool {
		t.Fatalf("kind \"tool\" erwartet, bekam %q", back.Package.Kind)
	}
	if back.Spectrum == nil || len(back.Spectrum.Members) != 1 {
		t.Fatalf("das Spectrum muss mitreisen, bekam %+v", back.Spectrum)
	}
	if problems := back.Verify(); len(problems) != 0 {
		t.Fatalf("keine Befunde erwartet, bekam %v", problems)
	}
}

// Ein Spectrum, dessen Referenzausgaben nicht mitreisen, ist eine Liste von
// Behauptungen — das muss auffallen.
func TestSpectrumNeedsItsReferenceOutputs(t *testing.T) {
	b := werkzeug(t)
	b.Spectrum.Members = append(b.Spectrum.Members, SpectrumMember{
		Name: "fehlt", Output: core.HashBytes([]byte("irgendwas"))})
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	problems := b.Verify()
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "keine Referenzausgabe") {
		t.Fatalf("die fehlende Referenzausgabe muss auffallen, bekam %v", problems)
	}
}

// Eine verfälschte Referenz fällt unter demselben Satz auf wie eine fehlende
// — und das ist keine Ungenauigkeit, sondern die Folge der
// Inhaltsadressierung: gesucht wird der **Hash**, den das Spectrum nennt. Sind
// die Bytes andere, ist das Genannte nicht mehr da. „Verfälscht" und „fehlt"
// sind derselbe Befund.
func TestSpectrumCatchesATamperedReference(t *testing.T) {
	b := werkzeug(t)
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Dir, ReferenceDir, "json.out"),
		[]byte("etwas anderes"), 0o644); err != nil {
		t.Fatal(err)
	}
	back, err := Open(b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	problems := back.Verify()
	if len(problems) == 0 || !strings.Contains(problems[0].Message, "keine Referenzausgabe") {
		t.Fatalf("die verfälschte Referenz muss auffallen, bekam %v", problems)
	}
}

// Der genannte Normalisierer muss ein Step **dieses** Rays sein — sonst zeigt
// das Spectrum auf ein Potential, das niemand im Paket findet.
func TestSpectrumNormalizerMustBeAStepOfTheRay(t *testing.T) {
	b := werkzeug(t)
	b.Spectrum.Normalizer = core.HashBytes([]byte("ein fremder Potential-Ref"))
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	problems := b.Verify()
	if len(problems) == 0 || !strings.Contains(problems[0].Message, "kein Step dieses Rays") {
		t.Fatalf("der fremde Normalisierer muss auffallen, bekam %v", problems)
	}
}

// SPEC §10: ein Werkzeug ohne Referenzmenge qualifiziert nichts.
func TestSpectrumWithoutMembersIsRefused(t *testing.T) {
	b := werkzeug(t)
	b.Spectrum.Members = nil
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	problems := b.Verify()
	if len(problems) == 0 || !strings.Contains(problems[0].Message, "keine Member") {
		t.Fatalf("ein Spectrum ohne Member muss auffallen, bekam %v", problems)
	}
}

// --- Paketidentität ----------------------------------------------------------

// Anforderungen und Spezifikation gehören zum Paket. Blieben sie außerhalb
// der Identität, ließe sich eine Anforderung ändern, ohne dass es dem Paket
// anzusehen wäre — und die V-Struktur stünde auf einer Behauptung.
func TestPackageIDCoversTheDocuments(t *testing.T) {
	b := raute(t)
	if _, err := b.AddFile("requirements/URS.md", []byte("URS-1 Es muss gehen.\n")); err != nil {
		t.Fatal(err)
	}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	vorher, err := b.ID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddFile("requirements/URS.md",
		[]byte("URS-1 Es muss anders gehen.\n")); err != nil {
		t.Fatal(err)
	}
	nachher, err := b.ID()
	if err != nil {
		t.Fatal(err)
	}
	if vorher == nachher {
		t.Fatal("eine geänderte Anforderung muss ein anderes Paket ergeben")
	}
}

// Ein Claim verändert sein Subjekt nie — auch nicht das Paket als Ganzes.
func TestPackageIDIsUnchangedByClaims(t *testing.T) {
	b := raute(t)
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	vorher, err := b.ID()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddClaim(testSigner(t), Claim{
		Subject: Ref{Hash: vorher}, Predicate: "https://beispiel/verifies",
		By: "wer auch immer"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	back, err := Open(b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	nachher, err := back.ID()
	if err != nil {
		t.Fatal(err)
	}
	if vorher != nachher {
		t.Fatal("ein Claim darf die Paketidentität nicht verändern")
	}
}

// Der Zeitstempel im Kopf gehört nicht in die Identität — sonst wäre jedes
// zweimal geschriebene Paket ein anderes.
func TestPackageIDIgnoresTheHeader(t *testing.T) {
	b := raute(t)
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	erst, err := b.ID()
	if err != nil {
		t.Fatal(err)
	}
	b.Package.Created = "2030-01-01T00:00:00Z"
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	zweit, err := b.ID()
	if err != nil {
		t.Fatal(err)
	}
	if erst != zweit {
		t.Fatal("der Kopf darf die Identität nicht verändern")
	}
}

// Ein Workflow bringt seinen Testdatensatz mit — sonst ist er beim Anwender
// nicht prüfbar, und OQ bliebe ein Versprechen aus fremder Hand.
func TestRayBrauchtTestdaten(t *testing.T) {
	b := raute(t)
	for i := range b.Ray.Holes {
		if b.Ray.Holes[i].Name == "dataset" {
			b.Ray.Holes[i].Default = ""
		}
	}
	problems := b.Verify()
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "kein OQ") {
		t.Fatalf("das fehlende Testdatum muss auffallen, bekam %v", problems)
	}
}

// Und der genannte Datensatz muss wirklich im Paket liegen.
func TestTestdatenMuessenImPaketLiegen(t *testing.T) {
	b := raute(t)
	for i := range b.Ray.Holes {
		if b.Ray.Holes[i].Name == "dataset" {
			b.Ray.Holes[i].Default = "testdaten/gibtsnicht.csv"
		}
	}
	problems := b.Verify()
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "liegt nicht im Paket") {
		t.Fatalf("der fehlende Datensatz muss auffallen, bekam %v", problems)
	}
}

// Ein Werkzeugpaket und ein Verticle sind davon nicht betroffen: das eine
// bringt seine Member mit, das andere bekommt seine Anfrage zur Laufzeit.
func TestNurRaysBrauchenTestdaten(t *testing.T) {
	b := werkzeug(t)
	for _, p := range b.Verify() {
		if strings.Contains(p.Message, "Testdatensatz") {
			t.Fatalf("ein Werkzeugpaket braucht keinen Testdatensatz: %v", p)
		}
	}
}

// Der Punkt von TreeID: dieselben Bytes an einem anderen Ort sind dieselbe
// Identität. Ein Commit-Hash kann das nicht — er benennt einen Stand in einem
// Repository, und ohne das Repository ist er nicht prüfbar.
func TestTreeIDIstUnabhaengigVomOrt(t *testing.T) {
	schreib := func(dir string) {
		if err := os.MkdirAll(filepath.Join(dir, "cmd", "x"), 0o755); err != nil {
			t.Fatal(err)
		}
		for rel, body := range map[string]string{
			"go.mod":        "module beispiel\n",
			"cmd/x/main.go": "package main\n\nfunc main() {}\n",
			"kleines.txt":   "etwas\n",
		} {
			if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)),
				[]byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	a, b := t.TempDir(), t.TempDir()
	schreib(a)
	schreib(b)

	ida, err := TreeID(a, SkipBuildArtefacts)
	if err != nil {
		t.Fatal(err)
	}
	idb, err := TreeID(b, SkipBuildArtefacts)
	if err != nil {
		t.Fatal(err)
	}
	if ida != idb {
		t.Fatalf("zwei Kopien, zwei Kennungen: %s vs %s", ida, idb)
	}

	// Gebautes und die Buchhaltung der Versionsverwaltung zählen nicht mit —
	// sonst hinge die Identität der Quelle daran, ob jemand gebaut hat.
	if err := os.MkdirAll(filepath.Join(b, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, ".git", "objects", "irgendwas"),
		[]byte("blob"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(b, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "bin", "x"), []byte("ELF"), 0o644); err != nil {
		t.Fatal(err)
	}
	nach, err := TreeID(b, SkipBuildArtefacts)
	if err != nil {
		t.Fatal(err)
	}
	if nach != ida {
		t.Fatal("Gebautes oder .git hat die Identität der Quelle verändert")
	}

	// Eine echte Änderung dagegen muss sie verändern.
	if err := os.WriteFile(filepath.Join(b, "kleines.txt"), []byte("anders\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	geaendert, err := TreeID(b, SkipBuildArtefacts)
	if err != nil {
		t.Fatal(err)
	}
	if geaendert == ida {
		t.Fatal("eine geänderte Datei hat die Identität nicht verändert")
	}
}

// Die Rückverfolgung hat genau eine Quelle je Tatsache: das Dokument sagt,
// was verlangt ist und wer es außerhalb belegt; der Step sagt, was er
// einlöst. Nichts steht zweimal — der erste Versuch, eine `trace.json` neben
// den Dokumenten, war genau die Matrix, die in jedem QM auseinanderläuft.
func TestAbdeckungLiestAusEinerQuelleJeTatsache(t *testing.T) {
	dir := t.TempDir()
	b := New(dir, KindRay, "beispiel")
	b.Ray.Steps = []Step{
		{ID: "rechnen", Verifies: []string{"FS-1"},
			Protocol: Protocol{Command: &Command{Args: "bild\n<command-file>"}}},
	}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddFile("requirements/URS.md",
		[]byte("**URS-1** Etwas soll gehen.\n→ FS-1, FS-2\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddFile("specification/FS.md", []byte(
		"**FS-1** Es rechnet.\n← URS-1\n**(OQ)**\n\n"+
			"**FS-2** Es weigert sich ohne Wert.\n← URS-1\n"+
			"Beleg: `ValidateBindings` — das Ausbleiben eines Laufs. **(OQ)**\n")); err != nil {
		t.Fatal(err)
	}

	ab, err := b.Abdeckungen()
	if err != nil {
		t.Fatal(err)
	}
	stand := map[string]Abdeckung{}
	for _, a := range ab {
		stand[a.Key] = a
	}
	if got := stand["FS-1"]; got.Zustand != "hier" || len(got.By) != 1 || got.Stage != "OQ" {
		t.Fatalf("FS-1 sollte hier belegt sein (OQ), ist %+v", got)
	}
	if got := stand["FS-2"]; got.Zustand != "anderswo" || got.Evidence == "" {
		t.Fatalf("FS-2 sollte anderswo belegt sein, ist %+v", got)
	}
	// Und das V schließt sich nach oben, ohne dass jemand es hinschreibt.
	if got := stand["URS-1"]; !got.Belegt || got.Zustand != "über die Zerlegung" {
		t.Fatalf("URS-1 sollte über FS-1 und FS-2 belegt sein, ist %+v", got)
	}
}

// Teilerfüllung ist Nichterfüllung — auch hier.
func TestEineOffeneZerlegungLaesstDieAnforderungOffen(t *testing.T) {
	dir := t.TempDir()
	b := New(dir, KindRay, "beispiel")
	b.Ray.Steps = []Step{{ID: "rechnen", Verifies: []string{"FS-1"},
		Protocol: Protocol{Command: &Command{Args: "bild\n<command-file>"}}}}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	b.AddFile("requirements/URS.md", []byte("**URS-1** Etwas.\n→ FS-1, FS-2\n"))
	b.AddFile("specification/FS.md", []byte("**FS-1** Eins.\n\n**FS-2** Zwei.\n"))

	ab, err := b.Abdeckungen()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range ab {
		if a.Key == "URS-1" && a.Belegt {
			t.Fatal("URS-1 gilt als belegt, obwohl FS-2 offen ist")
		}
		if a.Key == "FS-2" && a.Zustand != "offen" {
			t.Fatalf("FS-2 sollte offen sein, ist %q", a.Zustand)
		}
	}
}

// Ein Step, der eine Anforderung einlöst, die es nicht gibt, ist schlimmer
// als keine Rückverfolgung: er sieht aus wie ein Nachweis.
func TestStepDarfNurVorhandeneAnforderungenEinloesen(t *testing.T) {
	dir := t.TempDir()
	b := New(dir, KindRay, "beispiel")
	b.Ray.Steps = []Step{{ID: "rechnen", Verifies: []string{"FS-9"},
		Protocol: Protocol{Command: &Command{Args: "bild\n<command-file>"}}}}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	b.AddFile("specification/FS.md", []byte("**FS-1** Eins.\n"))

	var gefunden bool
	for _, p := range b.verifyVerifies() {
		if strings.Contains(p.Message, "FS-9") {
			gefunden = true
		}
	}
	if !gefunden {
		t.Fatal("ein erfundener Anforderungsverweis wurde nicht gemeldet")
	}
}

// Die Rückverfolgung zählt zur Fassung, nicht zum Potential: wer sie lockert,
// hat ein anderes Paket — aber nicht eine andere Berechnung, und ein
// angenommenes OQ überlebt es.
func TestRueckverfolgungAendertFassungNichtPotential(t *testing.T) {
	dir := t.TempDir()
	b := New(dir, KindRay, "beispiel")
	b.Ray.Steps = []Step{{ID: "rechnen",
		Protocol: Protocol{Command: &Command{Args: "bild\n<command-file>"}}}}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	fassungVorher, err := b.ID()
	if err != nil {
		t.Fatal(err)
	}
	potentialVorher, err := b.Ray.ID()
	if err != nil {
		t.Fatal(err)
	}

	b.Ray.Steps[0].Verifies = []string{"FS-1"}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	fassung, err := b.ID()
	if err != nil {
		t.Fatal(err)
	}
	potential, err := b.Ray.ID()
	if err != nil {
		t.Fatal(err)
	}
	if fassung == fassungVorher {
		t.Fatal("die Fassung hat sich durch die Rückverfolgung nicht geändert — " +
			"dann ließe sie sich still lockern")
	}
	if potential != potentialVorher {
		t.Fatal("das Potential hat sich geändert — die Berechnung ist dieselbe " +
			"geblieben, ein angenommenes OQ müsste sie überleben")
	}
}
