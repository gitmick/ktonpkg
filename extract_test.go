package ktonpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rauteLauf baut die Ausführungen eines Laufs der Raute, so wie ein Adapter sie aus den Fotons liest:
// aufbereitung → zweig-b, zweig-c → zusammenführung. Die Skripte sind in jedem Lauf dieselben
// Bytes; Datensatz und anzahl sind, was der Lauf hineingab.
func rauteLauf(t *testing.T, store map[string][]byte, run string, dataset []string, anzahl int) []Execution {
	t.Helper()
	put := func(body string) string {
		h := HashOf([]byte(body))
		store[h] = []byte(body)
		return h
	}
	code := map[string]string{
		"aufbereiten.R": `writeLines(readLines("dataset.csv"), "daten.txt")`,
		"zweigB.R":      `n <- as.integer(commandArgs(TRUE)[1]); writeLines(paste0("B: ", head(readLines("daten.txt"), n)), "b.txt")`,
		"zweigC.R":      `writeLines(paste0("C: ", readLines("daten.txt")), "c.txt")`,
		"zusammen.R":    `writeLines(c(readLines("b.txt"), readLines("c.txt")), "ergebnis.txt")`,
	}
	lines := func(prefix string, xs []string) string {
		var b strings.Builder
		for _, x := range xs {
			b.WriteString(prefix + x + "\n")
		}
		return b.String()
	}
	ds := put(lines("", dataset))
	daten := put(lines("", dataset))
	b := put(lines("B: ", dataset[:anzahl]))
	c := put(lines("C: ", dataset))
	erg := put(lines("B: ", dataset[:anzahl]) + lines("C: ", dataset))
	exec := func(name, script string, ins []ExecFile, outs []ExecFile, args ...string) Execution {
		ins = append(ins, ExecFile{Slot: script, Hash: put(code[script])})
		return Execution{
			ID: "sha256:" + fmt.Sprintf("%s-%s", run, name), Name: name,
			Inputs: ins, Outputs: outs, CommandFile: script,
			Command:     append([]string{"Rscript", script}, args...),
			Environment: "oci://example.org/jam-r@sha256:00",
			Label:       name, Rationale: "wegen " + name,
		}
	}
	return []Execution{
		exec("aufbereitung", "aufbereiten.R", []ExecFile{{"dataset.csv", ds}}, []ExecFile{{"daten.txt", daten}}),
		exec("zweig-b", "zweigB.R", []ExecFile{{"daten.txt", daten}}, []ExecFile{{"b.txt", b}}, fmt.Sprint(anzahl)),
		exec("zweig-c", "zweigC.R", []ExecFile{{"daten.txt", daten}}, []ExecFile{{"c.txt", c}}),
		exec("zusammenführung", "zusammen.R", []ExecFile{{"b.txt", b}, {"c.txt", c}}, []ExecFile{{"ergebnis.txt", erg}}),
	}
}

func fetchFrom(store map[string][]byte) Fetch {
	return func(h string) ([]byte, error) {
		body, ok := store[h]
		if !ok {
			return nil, fmt.Errorf("nicht vorhanden")
		}
		return body, nil
	}
}

func twoRuns(t *testing.T) ([]Execution, map[string][]byte) {
	store := map[string][]byte{}
	execs := append(rauteLauf(t, store, "a", []string{"probe-01", "probe-02", "probe-03", "probe-04"}, 3),
		rauteLauf(t, store, "b", []string{"alt-1", "alt-2", "alt-3"}, 1)...)
	return execs, store
}

func TestPropose_TwoRunsGroupIntoFourStepsWiredByHash(t *testing.T) {
	execs, _ := twoRuns(t)
	p, err := Propose(execs)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts) > 0 {
		t.Fatalf("Konflikte: %v", p.Conflicts)
	}
	if len(p.Runs) != 2 || len(p.Steps) != 4 {
		t.Fatalf("2 Läufe und 4 Steps erwartet, bekam %d und %d", len(p.Runs), len(p.Steps))
	}
	wiring := map[string]string{}
	for _, s := range p.Steps {
		if len(s.Executions) != 2 {
			t.Fatalf("%s: in jedem Lauf eine Ausführung erwartet, bekam %v", s.ID, s.Executions)
		}
		for _, in := range s.Inputs {
			wiring[s.ID+"/"+in.Slot] = in.Upstream
		}
	}
	for k, want := range map[string]string{
		"zweig-b/daten.txt":        "aufbereitung:daten.txt",
		"zweig-c/daten.txt":        "aufbereitung:daten.txt",
		"zusammenführung/b.txt":    "zweig-b:b.txt",
		"zusammenführung/c.txt":    "zweig-c:c.txt",
		"aufbereitung/dataset.csv": "",
	} {
		if wiring[k] != want {
			t.Errorf("%s: Verdrahtung %q, erwartet %q", k, wiring[k], want)
		}
	}
}

// Kandidaten sind Beobachtungen, keine Entscheidungen: jede Eingabe von außen ist einer — auch der
// Code, markiert als Kommandodatei —, und ein Parameter nur dort, wo die Läufe verschieden waren.
func TestPropose_CandidatesAreObservationsNotGuesses(t *testing.T) {
	execs, _ := twoRuns(t)
	p, _ := Propose(execs)
	files := map[string]bool{}
	for _, f := range p.Files {
		files[fmt.Sprintf("%s/%s code=%v", f.Step, f.Slot, f.CommandFile)] = true
	}
	for _, want := range []string{
		"aufbereitung/dataset.csv code=false", "aufbereitung/aufbereiten.R code=true",
		"zweig-b/zweigB.R code=true", "zweig-c/zweigC.R code=true", "zusammenführung/zusammen.R code=true",
	} {
		if !files[want] {
			t.Errorf("Dateikandidat fehlt: %s (hat %v)", want, files)
		}
	}
	if len(p.Files) != 5 {
		t.Errorf("genau 5 Eingaben von außen erwartet, bekam %d", len(p.Files))
	}
	if len(p.Params) != 1 || p.Params[0].Step != "zweig-b" || p.Params[0].Position != 2 {
		t.Fatalf("genau einen Parameterkandidaten erwartet (zweig-b, Stelle 2), bekam %+v", p.Params)
	}
}

// Mit einem einzigen Lauf gibt es keinen Parameterkandidaten: eine Zahl in einer Befehlszeile ist
// kein Parameter, solange niemand das sagt.
func TestPropose_OneRunProposesNoParameters(t *testing.T) {
	store := map[string][]byte{}
	p, err := Propose(rauteLauf(t, store, "a", []string{"x", "y"}, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Params) != 0 {
		t.Fatalf("aus einem Lauf keine Parameter, bekam %+v", p.Params)
	}
}

func refRun(t *testing.T, p *Proposal, execs []Execution, firstLine string) string {
	t.Helper()
	for _, r := range p.Runs {
		for _, id := range r.Executions {
			if strings.HasSuffix(id, "-aufbereitung") {
				for _, e := range execs {
					if e.ID == id && strings.HasPrefix(firstLine, "") {
						if strings.Contains(id, firstLine) {
							return r.ID
						}
					}
				}
			}
		}
	}
	t.Fatalf("kein Lauf %s", firstLine)
	return ""
}

func TestExtract_WhatWasChosenBecomesHolesTheRestStaysFixed(t *testing.T) {
	execs, store := twoRuns(t)
	p, _ := Propose(execs)
	ref := refRun(t, p, execs, "sha256:a-")
	dir := t.TempDir()
	b, err := Extract(p, execs, Choice{
		Name: "raute-extrahiert", Reference: ref,
		Holes:  []HoleChoice{{Name: "dataset", Step: "aufbereitung", Slot: "dataset.csv"}},
		Params: []ParamChoice{{Name: "anzahl", Step: "zweig-b", Position: 2}},
	}, fetchFrom(store), dir)
	if err != nil {
		t.Fatal(err)
	}
	if problems := b.Verify(); len(problems) > 0 {
		t.Fatalf("das extrahierte Paket besteht die eigene Prüfung nicht: %v", problems)
	}
	if len(b.Ray.Holes) != 2 {
		t.Fatalf("zwei Löcher erwartet, bekam %+v", b.Ray.Holes)
	}
	for _, h := range b.Ray.Holes {
		switch h.Name {
		case "anzahl":
			if h.Default != "3" || h.Type != HoleParam {
				t.Errorf("anzahl: Vorgabe aus dem Referenzlauf erwartet (3), bekam %+v", h)
			}
		case "dataset":
			body, err := b.ReadFile(h.Default)
			if err != nil || !strings.HasPrefix(string(body), "probe-01") {
				t.Errorf("dataset: der Datensatz des Referenzlaufs muss als Testdaten mitreisen: %q %v", body, err)
			}
		}
	}
	zb := b.Ray.Step("zweig-b")
	if zb.Protocol.Command.Args != "Rscript\n<command-file>\n<anzahl>" {
		t.Errorf("zweig-b: Befehlszeile %q", zb.Protocol.Command.Args)
	}
	for _, in := range zb.Inputs {
		if in.Slot == "zweigB.R" && (in.Variable != "command-file" || !in.Pinned()) {
			t.Errorf("der Code von zweig-b muss fest und als Kommandodatei im Paket liegen: %+v", in)
		}
	}
	if b.Spectrum == nil || len(b.Spectrum.Members) != 4 {
		t.Fatalf("vier Referenzausgaben im Spectrum erwartet, bekam %+v", b.Spectrum)
	}
	// Das Paket ist nicht nur geschrieben, sondern lesbar: Open sieht dasselbe.
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	id1, _ := b.ID()
	id2, _ := again.ID()
	if id1 != id2 {
		t.Fatalf("geschrieben und gelesen haben verschiedene Identitäten: %s / %s", id1, id2)
	}
	// Ohne Bindungen und ohne Vorgaben sind genau die gewählten Löcher offen.
	if missing := (&Ray{Holes: stripDefaults(b.Ray.Holes)}).MissingBindings(nil); strings.Join(missing, ",") != "anzahl,dataset" {
		t.Errorf("offen erwartet: anzahl,dataset — bekam %v", missing)
	}
}

func stripDefaults(hs []Hole) []Hole {
	out := append([]Hole(nil), hs...)
	for i := range out {
		out[i].Default = ""
	}
	return out
}

// Was niemand wählt, bleibt fest: ohne Auswahl ist das Paket vollständig gebunden — ein Ray ohne
// Löcher, der genau den Referenzlauf beschreibt.
func TestExtract_WithoutChoicesNothingIsAHole(t *testing.T) {
	execs, store := twoRuns(t)
	p, _ := Propose(execs)
	b, err := Extract(p, execs, Choice{Name: "fest", Reference: refRun(t, p, execs, "sha256:b-")},
		fetchFrom(store), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Ray.Holes) != 0 {
		t.Fatalf("keine Löcher erwartet, bekam %+v", b.Ray.Holes)
	}
	if got := b.Ray.Step("zweig-b").Protocol.Command.Args; got != "Rscript\n<command-file>\n1" {
		t.Fatalf("die Befehlszeile des Referenzlaufs erwartet, bekam %q", got)
	}
}

func TestExtract_RefusesAChoiceThatIsNotACandidate(t *testing.T) {
	execs, store := twoRuns(t)
	p, _ := Propose(execs)
	ref := p.Runs[0].ID
	for name, c := range map[string]Choice{
		"Loch an einer Kante":        {Holes: []HoleChoice{{"x", "zweig-b", "daten.txt"}}},
		"Parameter ohne Unterschied": {Params: []ParamChoice{{"y", "zweig-c", 1}}},
		"doppelter Name": {Holes: []HoleChoice{{"n", "aufbereitung", "dataset.csv"}},
			Params: []ParamChoice{{"n", "zweig-b", 2}}},
	} {
		c.Name, c.Reference = "x", ref
		if _, err := Extract(p, execs, c, fetchFrom(store), t.TempDir()); err == nil {
			t.Errorf("%s: hätte abgelehnt werden müssen", name)
		}
	}
}

// Föderierte Ausführungen bringen ihre Bytes von anderswo mit. Was nicht zum Hash passt, kommt
// nicht ins Paket.
func TestExtract_BytesThatDoNotMatchTheirHashAreRefused(t *testing.T) {
	execs, store := twoRuns(t)
	p, _ := Propose(execs)
	bad := func(h string) ([]byte, error) {
		if body, ok := store[h]; ok && strings.Contains(string(body), "writeLines") {
			return []byte("etwas anderes"), nil
		}
		return fetchFrom(store)(h)
	}
	_, err := Extract(p, execs, Choice{Name: "x", Reference: p.Runs[0].ID}, bad, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "haben den Hash") {
		t.Fatalf("falsche Bytes hätten abgelehnt werden müssen, bekam %v", err)
	}
}

func TestExtract_ConflictingWiringIsNotExtracted(t *testing.T) {
	execs, store := twoRuns(t)
	// Im zweiten Lauf kommt daten.txt für zweig-c von außen statt aus aufbereitung.
	for i := range execs {
		if execs[i].ID == "sha256:b-zweig-c" {
			execs[i].Inputs[0].Hash = HashOf([]byte("fremd\n"))
			store[execs[i].Inputs[0].Hash] = []byte("fremd\n")
		}
	}
	p, err := Propose(execs)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts) == 0 {
		t.Fatal("ein Konflikt erwartet")
	}
	if _, err := Extract(p, execs, Choice{Name: "x", Reference: p.Runs[0].ID}, fetchFrom(store),
		filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("mit Konflikten darf nicht extrahiert werden")
	}
	_ = os.Remove
}

// Eine zweite Ausführung derselben Berechnung mit denselben Bytes — anderswo, von jemand anderem —
// ist eine Reproduktion, kein zweiter Step. Ohne das verbände sie über ihre Ausgaben zwei Läufe, und
// jeder Step käme doppelt vor.
func TestPropose_ReproductionsAreNotSteps(t *testing.T) {
	execs, _ := twoRuns(t)
	var again []Execution
	for _, e := range execs {
		if strings.HasPrefix(e.ID, "sha256:a-") {
			r := e
			r.ID = strings.Replace(e.ID, "sha256:a-", "sha256:c-", 1)
			r.Name = e.Name + "-woanders"
			again = append(again, r)
		}
	}
	p, err := Propose(append(execs, again...))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Runs) != 2 || len(p.Steps) != 4 || len(p.Conflicts) != 0 {
		t.Fatalf("2 Läufe, 4 Steps, keine Konflikte erwartet — bekam %d, %d, %v", len(p.Runs), len(p.Steps), p.Conflicts)
	}
	n := 0
	for _, dups := range p.Reproductions {
		n += len(dups)
	}
	if n != 4 {
		t.Fatalf("vier Reproduktionen erwartet, bekam %v", p.Reproductions)
	}
}

// Dieselben Ausführungen ergeben dasselbe Paket — auch wenn der Vorschlag die Läufe anders nummeriert.
// Die Nummer hängt davon ab, wie Foton-Ids sortieren, und die unterscheiden sich zwischen Backends.
func TestExtract_TheRunLabelDoesNotReachThePackageIdentity(t *testing.T) {
	execs, store := twoRuns(t)
	choice := func(p *Proposal, ref string) Choice {
		return Choice{Name: "x", Reference: ref,
			Holes:  []HoleChoice{{Name: "dataset", Step: "aufbereitung", Slot: "dataset.csv"}},
			Params: []ParamChoice{{Name: "anzahl", Step: "zweig-b", Position: 2}}}
	}
	p1, _ := Propose(execs)
	b1, err := Extract(p1, execs, choice(p1, refRun(t, p1, execs, "sha256:a-")), fetchFrom(store), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Dieselben Ausführungen, die Ids so umbenannt, dass der a-Lauf jetzt als zweiter sortiert.
	var renamed []Execution
	for _, e := range execs {
		r := e
		r.ID = strings.Replace(strings.Replace(e.ID, "sha256:a-", "sha256:z-", 1), "sha256:b-", "sha256:c-", 1)
		renamed = append(renamed, r)
	}
	p2, _ := Propose(renamed)
	ref := refRun(t, p2, renamed, "sha256:z-")
	if ref == refRun(t, p1, execs, "sha256:a-") {
		t.Fatalf("der Test braucht eine andere Nummerierung, bekam beide Male %s", ref)
	}
	b2, err := Extract(p2, renamed, choice(p2, ref), fetchFrom(store), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id1, _ := b1.ID()
	id2, _ := b2.ID()
	if id1 != id2 {
		t.Fatalf("dasselbe Paket erwartet, bekam %s und %s (of: %q / %q)", id1, id2, b1.Spectrum.Of, b2.Spectrum.Of)
	}
}
