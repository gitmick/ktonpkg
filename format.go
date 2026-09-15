// Package ktonpkg ist das kton-Paketformat: ein Ordner, in dem die Arbeit
// unter ihren gewohnten Namen liegt und ein Unterordner `kton/` sagt, was das
// ist und was darüber behauptet wird.
//
//	<paket>/
//	  kton/package.json      Art, Fassung, Einstieg
//	  kton/<entry>.json      das Potential — ein Foton oder ein Graph davon
//	  kton/claims/<id>.json  nekton-Claims
//	  …                      die Nutzlast, unter ihren gewohnten Namen
//
// Drei Festlegungen tragen das Format:
//
//  1. **Die Nutzlast behält ihre Namen.** Ein Paket, das man nicht lesen
//     kann, wird nicht geprüft — und ein Report ohne Dateinamen ist kein
//     Report mehr.
//  2. **Die Identität hängt am Inhalt.** Eine feste Eingabe steht als
//     `{slot, path, hash}`: der Pfad sagt, wo die Bytes liegen, der Hash ist
//     die Identität (SPEC §6.1). Ein zweites Manifest gibt es nicht — das
//     Foton **ist** das Manifest.
//  3. **Dieselbe Struktur im Paket und im Repository.** Ein Ordner im
//     Repository bekommt denselben `kton/`-Unterordner; Exportieren ist dann
//     im Grenzfall ein Zippen.
//
// Dieses Paket kennt improve nicht. Es rechnet, prüft, liest und schreibt —
// mehr nicht.
package ktonpkg

import "kton.dev/plankton/core"

// FormatVersion steht in jedem package.json.
const FormatVersion = "kton-package/1"

// Paketarten.
const (
	KindRay      = "ray"
	KindReport   = "report"
	KindVerticle = "verticle"
	// KindTool ist ein Werkzeug: sein Bauplan als Nutzlast, seine Member als
	// Ray, sein Spectrum daneben. Derselbe Umschlag wie alles andere.
	KindTool = "tool"
)

// Package ist `kton/package.json` — bewusst klein. Alles Weitere gehört in
// das Potential und nicht in eine Hülle darum.
type Package struct {
	FormatVersion string `json:"formatVersion"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Entry         string `json:"entry"`
	Created       string `json:"created,omitempty"`
}

// Ray ist das Potential: ein Graph von Steps mit ihren Löchern.
//
// Ein einzelner Step ist der Normalfall eines Reports, vier Steps sind die
// Raute — dieselbe Datei, dieselbe Grammatik.
type Ray struct {
	Name  string `json:"name,omitempty"`
	Steps []Step `json:"steps"`
	Holes []Hole `json:"holes,omitempty"`
}

// Step ist ein Foton mit möglicherweise ungebundenen Stellen.
type Step struct {
	ID        string            `json:"id"`
	Label     string            `json:"label,omitempty"`
	Rationale string            `json:"rationale,omitempty"`
	Protocol  Protocol          `json:"protocol"`
	Params    map[string]string `json:"params,omitempty"`
	Inputs    []Input           `json:"inputs"`
	Outputs   []string          `json:"outputs,omitempty"`
	// Parent ist ein übergeordneter Step, wo die Ablage das verlangt.
	Parent string `json:"parent,omitempty"`
}

// Protocol beschreibt, **was** ausgeführt wird — nicht wo.
//
// Die Kommandodatei steht nicht hier, sondern als Eingabe mit
// `variable: "command-file"`; ihr Hash geht beim Rechnen der Identität in den
// Deskriptor ein. Wo es keine Kommandodatei gibt, benennt `tool` die
// Berechnung, damit zwei Steps mit gleichen Eingaben und verschiedenen Tools
// nicht denselben Schlüssel bekommen.
type Protocol struct {
	Command *Command `json:"command,omitempty"`
	Tool    string   `json:"tool,omitempty"`
}

// Command ist die Befehlszeile.
type Command struct {
	Args string `json:"args,omitempty"`
}

// Verdrahtungspräfixe für `Input.From`.
const (
	FromUpstream = "upstream:"
	FromHole     = "hole:"
)

// Input ist eine Eingabe an einem Slot.
//
// **Entweder** `path` + `hash` (fest, liegt im Paket) **oder** `from`
// (`upstream:<step>:<slot>` oder `hole:<name>`). Das ist die ganze
// Verdrahtungsgrammatik.
type Input struct {
	Slot string `json:"slot"`
	Path string `json:"path,omitempty"`
	Hash string `json:"hash,omitempty"`
	From string `json:"from,omitempty"`
	// Variable bindet die Eingabe an eine Prozessvariable, etwa
	// "command-file". Sie gehört zur Berechnung: dieselben Dateien an
	// anderen Variablen ergeben ein anderes Ergebnis.
	Variable string `json:"variable,omitempty"`
}

// Pinned sagt, ob die Eingabe fest im Paket liegt.
func (in Input) Pinned() bool { return in.Hash != "" || in.Path != "" }

// Holearten.
const (
	HoleFile  = "file"
	HoleParam = "param"
)

// Hole ist eine ungebundene Stelle: ein Dateislot oder ein skalarer Parameter.
type Hole struct {
	Name string `json:"name"`
	Step string `json:"step"`
	// Slot bei einer Datei, Param bei einem Wert — genau eines von beiden.
	Slot  string `json:"slot,omitempty"`
	Param string `json:"param,omitempty"`
	Type  string `json:"type"`

	Required bool `json:"required,omitempty"`
	// Default ist der Wert, mit dem das Loch gefüllt wird, wenn niemand einen
	// nennt. Bei einem **Dateiloch** ist es ein Pfad **im Paket** — der
	// mitgelieferte Testdatensatz.
	//
	// Das ist kein Komfort, sondern die Bedingung dafür, dass ein Workflow
	// überhaupt ein OQ haben kann: ohne Daten läuft er nicht, und ohne Lauf
	// gibt es nichts zu vergleichen. Ein Ray-Paket ohne Testdatensatz ist
	// darum unvollständig (siehe `Verify`).
	Default string `json:"default,omitempty"`
	// FilledBy ist die Herkunft — eine Behauptung, kein Identitätsmerkmal.
	// Sie darf auch als Claim danebenstehen.
	FilledBy    string `json:"filledBy,omitempty"`
	Description string `json:"description,omitempty"`
}

// Spectrum definiert ein Werkzeug **strukturell**: durch die Menge der
// Referenz-Fotonen, die ein Kandidat reproduzieren muss, und einen optionalen
// Normalisierer (SPEC §10).
//
// Die Form ist die der SPEC, Feld für Feld — damit `plankton spectrum check`
// die Datei unmittelbar lesen kann. Kein eigenes Format daneben.
type Spectrum struct {
	Spectrum string `json:"spectrum"`
	Of       string `json:"of,omitempty"`
	// Normalizer ist der Protokoll-Ref des Normalisierer-Potentials. Im Paket
	// ist das der Ref **eines Steps** des Rays — geprüft wird das in Verify.
	Normalizer string           `json:"normalizer,omitempty"`
	Members    []SpectrumMember `json:"members,omitempty"`
}

// ID ist die Kennung des Spectrums: der Hash seiner kanonischen Form.
//
// Der **Name** taugt dafür nicht. Zwei Häuser können beide ein Spectrum
// „jam-r" führen und verschiedene Member meinen; wer darauf eine Abhängigkeit
// gründet, hat einen Zettel mit einem Wort darauf. Der Hash dagegen benennt
// genau die Äquivalenzklasse von Werkzeugen, die diese Referenzausgaben
// erzeugen — und darum kann man ihn verlangen.
func (s Spectrum) ID() (string, error) {
	b, err := core.CanonValue(s)
	if err != nil {
		return "", err
	}
	return core.HashBytes(b), nil
}

// SpectrumMember ist eine Referenzausgabe.
type SpectrumMember struct {
	Name   string `json:"name"`
	Output string `json:"output"`
	Model  string `json:"model,omitempty"`
	Data   string `json:"data,omitempty"`
}

// Requirement ist, was ein Paket zum Laufen **braucht**.
//
// Der Unterschied zum `runs-on`-Claim ist der ganze Punkt: der Claim sagt,
// *worauf es lief*, als das Paket entstand — eine Beobachtung, und er nennt
// dafür einen lokalen Instanznamen. Ein Requirement sagt, *was es braucht*,
// und nennt dafür eine **Spectrum-Kennung**: irgendein Werkzeug, das diese
// Referenzausgaben reproduziert, tut es. Das eine ist Vergangenheit, das
// andere eine Bedingung an die Zukunft.
//
// Requirements zählen zur Paketidentität. Sonst könnte jemand die Bedingungen
// lockern, ohne dass sich die Identität ändert — dieselbe stille Ausweitung,
// die bei den Funktionsgruppen verboten ist.
type Requirement struct {
	// Kind ist heute immer "tool".
	Kind string `json:"kind"`
	// Spectrum ist die Kennung der verlangten Äquivalenzklasse.
	Spectrum string `json:"spectrum"`
	// Name ist der Menschenname dazu ("jam-r") — zum Lesen, nicht zum
	// Auflösen.
	Name string `json:"name,omitempty"`
	// Steps nennt die Steps, die es brauchen. Leer heißt: alle.
	Steps []string `json:"steps,omitempty"`
}

// Claim ist ein Satz über etwas im Paket (nekton, SPEC §7).
type Claim struct {
	Subject   Ref    `json:"subject"`
	Predicate string `json:"predicate"`
	// ObjectID ist das, worüber der Claim spricht, in einer Form, unter der
	// man ihn **wiederfindet** — eine Toolinstanz, eine Spectrum-Kennung.
	// Ohne sie ist das Objekt ein Literal: gespeichert, aber nicht indiziert
	// (nekton, SPEC §7.2).
	ObjectID string         `json:"objectId,omitempty"`
	Object   map[string]any `json:"object,omitempty"`
	By       string         `json:"by,omitempty"`
	When     string         `json:"when,omitempty"`
	Why      string         `json:"why,omitempty"`
	Evidence []Ref          `json:"evidence,omitempty"`
}

// Ref verweist auf etwas Inhaltsadressiertes.
type Ref struct {
	Hash string `json:"hash,omitempty"`
	Name string `json:"name,omitempty"`
}
