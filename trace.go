package ktonpkg

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Von der Anforderung zum Nachweis — ohne eine zweite Buchhaltung.
//
// Der erste Versuch war ein `kton/trace.json`: je Anforderung ein Eintrag mit
// Schlüssel, Dokument, Stufe und Beleg. Das ist die Traceability-Matrix, die
// in jedem QM von Hand gepflegt wird — und die immer ausei­nanderläuft, weil
// dieselbe Tatsache an zwei Orten steht. Sie ist ersatzlos gestrichen.
//
// Stattdessen sagt jede Tatsache genau eine Stelle:
//
//	specification/FS.md   was verlangt ist, worauf es sich stützt (`← URS-3`)
//	                      und, wo dieses Paket es nicht laufen lassen kann,
//	                      wer es stattdessen belegt (`Beleg: …`)
//	requirements/URS.md   worin die Anwenderanforderung zerlegt ist (`→ FS-3`)
//	kton/ray.json         welcher Step welche Anforderung einlöst (`verifies`)
//
// Der Step nennt, was er belegt — wie eine Testannotation, und aus demselben
// Grund: die Verbindung steht dort, wo der Nachweis entsteht, und kann nicht
// vergessen werden, wenn der Step sich ändert.
//
// Die Zuordnung zählt in die **Fassung** (siehe Bundle.ID), nicht in das
// Potential. Beides ist beabsichtigt: wer die Rückverfolgung lockert, hat ein
// anderes Paket — aber nicht eine andere Berechnung, und ein angenommenes OQ
// überlebt es. Dieselbe Regel wie für Dokumentation.

// schluessel findet `**FS-4**` am Zeilenanfang — die Form, in der unsere
// Anforderungsdokumente ihre Kennungen tragen.
var schluessel = regexp.MustCompile(`(?m)^\*\*([A-Z][A-Z0-9]*-[0-9]+)\*\*`)

// pfeil findet die Zerlegung `→ FS-1, FS-2`.
var pfeil = regexp.MustCompile(`(?m)^(?:→|->)\s*(.+)$`)

// zurueck findet `← URS-1` — die Erfüllung nach oben, in der alten Form.
var zurueck = regexp.MustCompile(`(?m)^(?:←|<-)\s*(.+)$`)

// RequirementSatisfies liest je Anforderung, welche sie erfüllt.
func RequirementSatisfies(doc []byte) map[string][]string {
	out := map[string][]string{}
	for key, block := range bloecke(doc) {
		for _, p := range zurueck.FindAllSubmatch(block, -1) {
			for _, k := range kennung.FindAll(p[1], -1) {
				out[key] = append(out[key], string(k))
			}
		}
	}
	return out
}

// beleg findet den Nachweis außerhalb dieses Pakets: `Beleg: …`.
var beleg = regexp.MustCompile(`(?m)^Beleg:\s*(.+)$`)

var kennung = regexp.MustCompile(`[A-Z][A-Z0-9]*-[0-9]+`)

// stufe findet `**(OQ)**` — die Angabe, die das Dokument ohnehin schon macht.
// Auch sie wird gelesen und nicht ein zweites Mal verlangt.
var stufeRe = regexp.MustCompile(`\*\*\((IQ|OQ|PQ)\)\*\*`)

// RequirementStages liest je Anforderung die Qualifizierungsstufe.
func RequirementStages(doc []byte) map[string]string {
	out := map[string]string{}
	for key, block := range bloecke(doc) {
		if m := stufeRe.FindSubmatch(block); m != nil {
			out[key] = string(m[1])
		}
	}
	return out
}

// RequirementKeys liest die Kennungen aus einem Anforderungsdokument.
//
// Es liest, was dasteht, und erfindet keine Struktur: wer eine Kennung nicht
// fett an den Zeilenanfang setzt, hat für dieses Werkzeug keine.
func RequirementKeys(doc []byte) []string {
	var out []string
	gesehen := map[string]bool{}
	for _, m := range schluessel.FindAllSubmatch(doc, -1) {
		k := string(m[1])
		if !gesehen[k] {
			gesehen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// bloecke teilt ein Dokument in seine Anforderungen: Kennung → Text bis zur
// nächsten Kennung.
func bloecke(doc []byte) map[string][]byte {
	out := map[string][]byte{}
	orte := schluessel.FindAllSubmatchIndex(doc, -1)
	for i, m := range orte {
		ende := len(doc)
		if i+1 < len(orte) {
			ende = orte[i+1][0]
		}
		out[string(doc[m[2]:m[3]])] = doc[m[0]:ende]
	}
	return out
}

// RequirementLinks liest je Anforderung, worin sie zerlegt ist.
//
// Damit schließt sich das V nach oben: eine Anwenderanforderung ist nicht
// deshalb belegt, weil ein Step sie nennt — sie ist belegt, wenn **alles**,
// worin sie zerlegt wurde, belegt ist. Eine Zerlegung, von der ein Teil offen
// ist, ist offen; dieselbe Regel wie beim Spectrum.
func RequirementLinks(doc []byte) map[string][]string {
	out := map[string][]string{}
	for key, block := range bloecke(doc) {
		for _, p := range pfeil.FindAllSubmatch(block, -1) {
			for _, k := range kennung.FindAll(p[1], -1) {
				out[key] = append(out[key], string(k))
			}
		}
	}
	return out
}

// RequirementEvidence liest die `Beleg:`-Zeile — den Nachweis außerhalb
// dieses Pakets.
//
// Das ist der ehrlichste Teil. Ein Paket belegt, was es **laufen lassen**
// kann. „Derselbe Inhalt ergibt denselben Aktionsschlüssel" wird gerechnet,
// nicht gelaufen; „ohne Wert wird nicht realisiert" ist das Ausbleiben eines
// Laufs; `Verify` läuft vor dem Einspielen. Solche Punkte als offen zu führen
// wäre falsch, als belegt gelogen. Sie sind belegt — vom Werkzeug, und der
// Text sagt von welchem. Prüfen können wir ihn nicht; benennen schon.
func RequirementEvidence(doc []byte) map[string]string {
	out := map[string]string{}
	for key, block := range bloecke(doc) {
		if m := beleg.FindSubmatch(block); m != nil {
			out[key] = strings.TrimSpace(string(m[1]))
		}
	}
	return out
}

// Abdeckung ist der Befund zu einer Anforderung.
type Abdeckung struct {
	Anforderung
	// By nennt die Steps, die sie einlösen.
	By []string `json:"by,omitempty"`
	// Erfüllt nennt die Anforderungen, die **diese** erfüllen — die
	// Gegenrichtung zu `satisfies`, gerechnet und nicht geschrieben.
	Erfüllt []string `json:"erfuellt,omitempty"`
	// Zustand ist "hier", "anderswo", "über die Zerlegung" oder "offen".
	// Vier, nicht zwei: ein Nachweis, den ein anderes Werkzeug führt, ist
	// weder ein Lauf in diesem Paket noch gar keiner.
	Zustand string `json:"zustand"`
	Belegt  bool   `json:"belegt"`
}

// Abdeckungen hält die Anforderungen gegen das, was die Steps einlösen.
//
// Zwei Wege, eine Anforderung zu belegen, und ein dritter, der keiner ist:
//
//	hier                ein Step dieses Pakets löst sie ein
//	anderswo            das Paket kann sie nicht laufen lassen und sagt, wer sie belegt
//	über die Zerlegung  alles, was sie erfüllt, ist belegt
//	offen               nichts davon
func (b *Bundle) Abdeckungen() ([]Abdeckung, error) {
	anf, err := b.Anforderungen()
	if err != nil {
		return nil, err
	}
	durch := map[string][]string{}
	if b.Ray != nil {
		steps := append([]Step(nil), b.Ray.Steps...)
		sort.Slice(steps, func(i, j int) bool { return steps[i].ID < steps[j].ID })
		for _, s := range steps {
			for _, k := range s.Verifies {
				durch[k] = append(durch[k], s.ID)
			}
		}
	}

	out := make([]Abdeckung, 0, len(anf))
	for _, a := range anf {
		ab := Abdeckung{Anforderung: a, By: durch[a.Key]}
		switch {
		case len(ab.By) > 0:
			ab.Zustand, ab.Belegt = "hier", true
		case ab.Evidence != "":
			ab.Zustand, ab.Belegt = "anderswo", true
		default:
			ab.Zustand = "offen"
		}
		out = append(out, ab)
	}

	// Die Gegenrichtung rechnen: wer erfüllt wen.
	for i := range out {
		for _, oben := range out[i].Satisfies {
			for j := range out {
				if out[j].Key == oben {
					out[j].Erfüllt = append(out[j].Erfüllt, out[i].Key)
				}
			}
		}
	}
	// Und nach oben schließen: eine Anforderung ist belegt, wenn **alles**,
	// was sie erfüllt, belegt ist. Teilerfüllung ist Nichterfüllung —
	// dieselbe Regel wie beim Spectrum.
	direkt := map[string]bool{}
	for _, a := range out {
		direkt[a.Key] = a.Belegt
	}
	for i := range out {
		if out[i].Belegt || len(out[i].Erfüllt) == 0 {
			continue
		}
		alle := true
		for _, t := range out[i].Erfüllt {
			if !direkt[t] {
				alle = false
			}
		}
		if alle {
			out[i].Zustand, out[i].Belegt = "über die Zerlegung", true
		}
	}
	sort.Slice(out, func(i, j int) bool { return vorher(out[i].Key, out[j].Key) })
	return out, nil
}

// verifyVerifies prüft, dass jeder Step nur Anforderungen nennt, die es gibt.
//
// Eine Rückverfolgung, die auf Erfundenes zeigt, ist schlimmer als keine —
// sie sieht aus wie ein Nachweis.
func (b *Bundle) verifyVerifies() []Problem {
	if b.Ray == nil {
		return nil
	}
	anf, err := b.Anforderungen()
	if err != nil {
		return []Problem{{Message: err.Error()}}
	}
	da := map[string]bool{}
	for _, a := range anf {
		da[a.Key] = true
	}
	var out []Problem
	for _, s := range b.Ray.Steps {
		for _, k := range s.Verifies {
			if !da[k] {
				out = append(out, Problem{Step: s.ID,
					Message: fmt.Sprintf("der Step löst %q ein — diese Anforderung "+
						"steht in keinem Dokument dieses Pakets", k)})
			}
		}
	}
	return out
}

// VerifiesHash ist die Zuordnung Step → Anforderung als Hash, für die
// Paketidentität.
func (r *Ray) VerifiesHash() (map[string]any, bool) {
	out := map[string]any{}
	for _, s := range r.Steps {
		if len(s.Verifies) == 0 {
			continue
		}
		v := append([]string(nil), s.Verifies...)
		sort.Strings(v)
		liste := make([]any, len(v))
		for i, k := range v {
			liste[i] = k
		}
		out[s.ID] = liste
	}
	return out, len(out) > 0
}

// vorher ordnet Kennungen so, wie ein Mensch sie liest: FS-2 vor FS-10.
func vorher(a, b string) bool {
	pa, na := teile(a)
	pb, nb := teile(b)
	if pa != pb {
		return pa < pb
	}
	return na < nb
}

func teile(k string) (string, int) {
	i := strings.LastIndex(k, "-")
	if i < 0 {
		return k, 0
	}
	n := 0
	for _, c := range k[i+1:] {
		if c < '0' || c > '9' {
			return k, 0
		}
		n = n*10 + int(c-'0')
	}
	return k[:i], n
}
