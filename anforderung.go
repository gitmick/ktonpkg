package ktonpkg

import (
	"fmt"
	"sort"
	"strings"

	"kton.dev/plankton/core"
)

// Eine Anforderung ist eine **Datei**, und damit ein Gegenstand.
//
// Vorher standen alle in zwei Sammeldokumenten, `requirements/URS.md` und
// `specification/FS.md`. Dann hat eine Anforderung keine eigene Identität: um
// „FS-4" zu zitieren, muss man sagen, aus welcher Fassung von FS.md — und ein
// Tippfehler in FS-9 entwertet den Nachweis für FS-1, weil sich der Hash des
// ganzen Dokuments ändert.
//
// Je Datei eine Anforderung, und der Inhaltshash der Datei **ist** die
// Anforderung. Zitierbar ohne Dokumentfassung, einzeln versionierbar, und ein
// Claim kann festhalten, welchen **Wortlaut** er eingelöst hat.
//
// Die Richtung ist nur eine: eine Spezifikation sagt, welche
// Anwenderanforderung sie erfüllt (`satisfies`). Die Gegenrichtung wird
// gerechnet. Vorher stand sie zweimal da — `→ FS-1, FS-2` oben und
// `← URS-1` unten — und zwei Orte für dieselbe Tatsache laufen auseinander.
//
//	---
//	key: FS-4
//	title: Skalarer Parameter über die Befehlszeile
//	type: specification
//	satisfies: [URS-3]
//	stage: OQ
//	---
//
//	Ein skalarer Parameter steht als Wert am Step und geht über die
//	Befehlszeile in die Berechnung.

// Anforderung ist eine Anforderung oder Spezifikation samt ihrer Identität.
type Anforderung struct {
	Key   string `json:"key"`
	Title string `json:"title,omitempty"`
	// Type ist "requirements" oder "specification".
	Type string `json:"type,omitempty"`
	// Satisfies nennt die Anforderungen, die diese erfüllt — nach oben.
	Satisfies []string `json:"satisfies,omitempty"`
	// Stage ist IQ, OQ oder PQ.
	Stage string `json:"stage,omitempty"`
	// Evidence nennt den Nachweis außerhalb dieses Pakets, wo das Paket ihn
	// nicht führen kann.
	Evidence string `json:"evidence,omitempty"`
	// Path ist der Paketpfad, Hash die Identität **dieser** Anforderung.
	Path string `json:"path"`
	Hash string `json:"hash"`
	// Sammeldokument sagt, dass die Anforderung aus einer Sammeldatei
	// stammt. Dann ist `Hash` der Hash des ganzen Dokuments und nicht der
	// dieser Anforderung — schwächer, und es soll dastehen.
	Sammeldokument bool `json:"sammeldokument,omitempty"`
}

// Anforderungen liest alle Anforderungen eines Pakets.
//
// Eine Datei mit Frontmatter ist **eine** Anforderung. Eine ohne ist eine
// Sammeldatei der alten Form; sie wird weiter gelesen, damit vorhandene Pakete
// nicht verstummen, und jede Anforderung daraus trägt `sammeldokument`.
func (b *Bundle) Anforderungen() ([]Anforderung, error) {
	var out []Anforderung
	for _, dir := range []string{"requirements", "specification"} {
		dateien, err := b.List(dir)
		if err != nil {
			continue
		}
		for _, rel := range dateien {
			if !strings.HasSuffix(rel, ".md") {
				continue
			}
			body, err := b.ReadFile(rel)
			if err != nil {
				return nil, err
			}
			hash := core.HashBytes(body)
			if fm, rest, ok := frontmatter(body); ok {
				a := ausFrontmatter(fm)
				a.Path, a.Hash = rel, hash
				if a.Type == "" {
					a.Type = dir
				}
				if a.Key == "" {
					return nil, fmt.Errorf("%s: Frontmatter ohne `key`", rel)
				}
				if a.Title == "" {
					a.Title = ersteZeile(rest)
				}
				out = append(out, a)
				continue
			}
			// Sammeldatei der alten Form.
			extern := RequirementEvidence(body)
			stufen := RequirementStages(body)
			links := RequirementLinks(body)
			rueck := RequirementSatisfies(body)
			for _, k := range RequirementKeys(body) {
				a := Anforderung{Key: k, Type: dir, Path: rel, Hash: hash,
					Stage: stufen[k], Evidence: extern[k], Sammeldokument: true,
					Satisfies: rueck[k]}
				out = append(out, a)
			}
			// In der alten Form stand die Zerlegung oben (`→ FS-1, FS-2`)
			// **und** die Erfüllung unten (`← URS-1`) — zweimal dieselbe
			// Tatsache. Beides wird gelesen und auf die eine Richtung
			// gebracht, die jetzt gilt.
			for oben, teile := range links {
				for _, t := range teile {
					for i := range out {
						if out[i].Key == t && !enthält(out[i].Satisfies, oben) {
							out[i].Satisfies = append(out[i].Satisfies, oben)
						}
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return vorher(out[i].Key, out[j].Key) })
	return out, nil
}

// ersteZeile nimmt den ersten Satz des Rumpfes als Titel, wenn keiner
// dasteht — besser als eine leere Überschrift.
func ersteZeile(rest []byte) string {
	for _, z := range strings.Split(string(rest), "\n") {
		z = strings.TrimSpace(strings.TrimLeft(z, "#"))
		if z != "" {
			if len(z) > 70 {
				z = z[:70]
			}
			return strings.TrimSpace(z)
		}
	}
	return ""
}

func enthält(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// frontmatter trennt einen YAML-Kopf vom Rumpf.
func frontmatter(doc []byte) (map[string][]string, []byte, error2) {
	s := string(doc)
	if !strings.HasPrefix(s, "---\n") {
		return nil, doc, false
	}
	ende := strings.Index(s[4:], "\n---")
	if ende < 0 {
		return nil, doc, false
	}
	kopf := s[4 : 4+ende]
	rest := s[4+ende:]
	if i := strings.Index(rest, "\n"); i >= 0 {
		rest = rest[i+1:]
	}
	felder := map[string][]string{}
	for _, zeile := range strings.Split(kopf, "\n") {
		zeile = strings.TrimRight(zeile, " \t")
		if zeile == "" || strings.HasPrefix(strings.TrimSpace(zeile), "#") {
			continue
		}
		if strings.HasPrefix(zeile, "  - ") || strings.HasPrefix(zeile, "- ") {
			// Listenfortsetzung des zuletzt gesehenen Feldes.
			continue
		}
		i := strings.Index(zeile, ":")
		if i < 0 {
			continue
		}
		name := strings.TrimSpace(zeile[:i])
		wert := strings.TrimSpace(zeile[i+1:])
		felder[name] = werte(wert)
	}
	return felder, []byte(rest), true
}

// error2 ist ein Bool mit sprechendem Namen an der Stelle, an der sonst ein
// `ok` stünde — der Kopf ist da oder nicht.
type error2 = bool

// werte liest `a`, `[a, b]` oder `"a"`.
func werte(v string) []string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		v = v[1 : len(v)-1]
		var out []string
		for _, t := range strings.Split(v, ",") {
			t = strings.Trim(strings.TrimSpace(t), `"'`)
			if t != "" {
				out = append(out, t)
			}
		}
		return out
	}
	v = strings.Trim(v, `"'`)
	if v == "" {
		return nil
	}
	return []string{v}
}

func ausFrontmatter(f map[string][]string) Anforderung {
	erst := func(k string) string {
		if v := f[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	return Anforderung{
		Key: erst("key"), Title: erst("title"), Type: erst("type"),
		Stage: erst("stage"), Evidence: erst("evidence"),
		Satisfies: f["satisfies"],
	}
}
