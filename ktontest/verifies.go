// Package ktontest ist das kleine Stück, das ein Go-Test braucht, um
// aufzuschreiben, welche Anforderung er einlöst.
//
// Es steht für sich, aus zwei Gründen: `testing` gehört nicht in eine
// Bibliothek, die ausgeliefert wird, und `ktonpkg` kann `improve` nicht
// importieren. Beide Testmengen brauchen es.
package ktontest

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
)

// Ein Test zeichnet auf, welche Anforderung er einlöst.
//
// Die Raute sagt über fünf ihrer Spezifikationspunkte: „belegt — anderswo".
// FS-6 und FS-7 (Aktionsschlüssel) werden gerechnet, nicht gelaufen; FS-8
// (`Verify`) läuft vor dem Einspielen; FS-9 ist das Ausbleiben eines Laufs.
// Das Paket kann sie nicht belegen, und darum stand dort bisher ein **Text**
// im Frontmatter — eine Behauptung, die kein Register finden konnte. Genau
// deshalb blieben URS-1 und URS-4 bis URS-6 in `trace.status` offen.
//
// Hier wird daraus eine Aufzeichnung. Die Verbindung steht dort, wo der
// Nachweis entsteht — im Test selbst, wie `verifies` am Step, und aus
// demselben Grund: sie kann nicht vergessen werden, wenn der Test sich ändert.
//
// Benannt wird die Anforderung über ihren **Inhaltshash**, nicht über eine
// Kennung und nicht über einen Dateinamen. Ändert sich ihr Wortlaut, nennt der
// Claim einen Hash, den es nicht mehr gibt, und `trace.status` führt sie
// wieder als offen. Laut statt still — dieselbe Eigenschaft wie überall sonst.
//
// Ohne `KTON_VERIFIES` tut das hier nichts. Ein gewöhnliches `go test` soll
// nichts schreiben; die Aufzeichnung ist ein eigener Lauf mit eigener Absicht.

// VerifiesEnv nennt die Datei, in die aufgezeichnet wird.
const VerifiesEnv = "KTON_VERIFIES"

// Testnachweis ist eine Zeile der Aufzeichnung.
type Testnachweis struct {
	// Anforderung ist der Inhaltshash der Anforderungsdatei.
	Anforderung string `json:"anforderung"`
	Test        string `json:"test"`
	// Erfüllt sagt, ob der Test bestanden hat. Ein durchgefallener Test wird
	// aufgezeichnet wie ein bestandener — sonst wäre die Abwesenheit eines
	// Nachweises zweideutig.
	Erfüllt bool   `json:"erfuellt"`
	Notiz   string `json:"notiz,omitempty"`
}

var (
	verifiesMu   sync.Mutex
	verifiesFile *os.File
)

// Record zeichnet einen Nachweis auf, ohne `testing`.
//
// Nicht jeder Nachweis ist ein Go-Test. `cmd/diamondtest` ist ein
// Integrationslauf mit eigener Prüfmechanik, und die Anforderung, die er
// einlöst, ist dieselbe Art Gegenstand wie jede andere. Die Aufzeichnung darf
// daran nicht hängen.
func Record(n Testnachweis) error {
	pfad := os.Getenv(VerifiesEnv)
	if pfad == "" {
		return nil
	}
	verifiesMu.Lock()
	defer verifiesMu.Unlock()
	if verifiesFile == nil {
		f, err := os.OpenFile(pfad, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		verifiesFile = f
	}
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = verifiesFile.Write(append(raw, '\n'))
	return err
}

// Verifies zeichnet auf, dass dieser Test die genannte Anforderung einlöst.
//
// Aufzurufen **am Anfang** des Tests: das Ergebnis wird über `t.Cleanup`
// nachgetragen, damit auch ein Fehlschlag in die Aufzeichnung kommt.
func Verifies(t *testing.T, anforderung string, notiz ...string) {
	t.Helper()
	if os.Getenv(VerifiesEnv) == "" {
		return
	}
	var n string
	if len(notiz) > 0 {
		n = notiz[0]
	}
	t.Cleanup(func() {
		if err := Record(Testnachweis{
			Anforderung: anforderung, Test: t.Name(),
			Erfüllt: !t.Failed(), Notiz: n,
		}); err != nil {
			t.Errorf("%s: %v", VerifiesEnv, err)
		}
	})
}
