package ktonpkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"kton.dev/plankton/core"
)

// Bundle ist ein geöffnetes Paket: sein Ordner, sein Kopf und sein Potential.
type Bundle struct {
	Dir     string
	Package Package
	Ray     *Ray
	// Claims sind signiert — unsigniert ist ein Claim keiner (SPEC §7.2).
	Claims []SignedClaim
	// Spectrum steht nur bei einem Werkzeugpaket: es sagt, was dieses Paket
	// **anbietet**.
	Spectrum *Spectrum
	// Requires sagt, was es **braucht**.
	Requires []Requirement
}

// SpectrumEntry ist der Ort des Spectrums im Paket.
const SpectrumEntry = "kton/spectrum.json"

// RequiresEntry ist der Ort der Bedingungen im Paket.
const RequiresEntry = "kton/requires.json"

// ReferenceDir hält die Referenzausgaben eines Werkzeugpakets — die Bytes,
// auf die sich die Member beziehen. Ohne sie wäre ein Spectrum eine Liste von
// Hashes, die niemand nachrechnen kann.
const ReferenceDir = "reference"

// New legt ein leeres Paket an.
func New(dir, kind, name string) *Bundle {
	return &Bundle{
		Dir: dir,
		Package: Package{
			FormatVersion: FormatVersion, Kind: kind, Name: name,
			Entry: "kton/ray.json", Created: time.Now().UTC().Format(time.RFC3339),
		},
		Ray: &Ray{Name: name},
	}
}

// Open liest ein Paket aus einem Ordner.
func Open(dir string) (*Bundle, error) {
	b := &Bundle{Dir: dir}
	if err := readJSON(filepath.Join(dir, "kton", "package.json"), &b.Package); err != nil {
		return nil, fmt.Errorf("kton/package.json: %w", err)
	}
	if b.Package.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("unbekannte Paketfassung %q — erwartet %q",
			b.Package.FormatVersion, FormatVersion)
	}
	entry := b.Package.Entry
	if entry == "" {
		entry = "kton/ray.json"
	}
	var ray Ray
	if err := readJSON(filepath.Join(dir, filepath.FromSlash(entry)), &ray); err != nil {
		return nil, fmt.Errorf("%s: %w", entry, err)
	}
	b.Ray = &ray

	var spec Spectrum
	if err := readJSON(filepath.Join(dir, filepath.FromSlash(SpectrumEntry)), &spec); err == nil {
		b.Spectrum = &spec
	}
	var req []Requirement
	if err := readJSON(filepath.Join(dir, filepath.FromSlash(RequiresEntry)), &req); err == nil {
		b.Requires = req
	}

	keys, err := b.AttachedKeys()
	if err != nil {
		return nil, err
	}
	claimDir := filepath.Join(dir, "kton", "claims")
	entries, err := os.ReadDir(claimDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			var env core.Envelope
			if err := readJSON(filepath.Join(claimDir, e.Name()), &env); err != nil {
				return nil, fmt.Errorf("kton/claims/%s: %w", e.Name(), err)
			}
			if !env.HasSignature() {
				return nil, fmt.Errorf("kton/claims/%s: ohne Unterschrift kein Claim", e.Name())
			}
			c, err := ClaimFromEnvelope(env, keys)
			if err != nil {
				return nil, fmt.Errorf("kton/claims/%s: %w", e.Name(), err)
			}
			b.Claims = append(b.Claims, *c)
		}
	}
	return b, nil
}

// Write schreibt Kopf, Potential und Claims. Die Nutzlast liegt schon da —
// sie wird nicht angefasst.
func (b *Bundle) Write() error {
	if b.Package.FormatVersion == "" {
		b.Package.FormatVersion = FormatVersion
	}
	if b.Package.Entry == "" {
		b.Package.Entry = "kton/ray.json"
	}
	if err := os.MkdirAll(filepath.Join(b.Dir, "kton"), 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(b.Dir, "kton", "package.json"), b.Package); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(b.Dir, filepath.FromSlash(b.Package.Entry)), b.Ray); err != nil {
		return err
	}
	if b.Spectrum != nil {
		if err := writeJSON(filepath.Join(b.Dir, filepath.FromSlash(SpectrumEntry)),
			b.Spectrum); err != nil {
			return err
		}
	}
	if len(b.Requires) > 0 {
		if err := writeJSON(filepath.Join(b.Dir, filepath.FromSlash(RequiresEntry)),
			b.Requires); err != nil {
			return err
		}
	}
	// Was in `kton/claims/` liegt, ist der Stand von `b.Claims` — nichts
	// Älteres bleibt daneben liegen und behauptet weiter etwas.
	behalten := map[string]bool{}
	for i, c := range b.Claims {
		if !c.Envelope.HasSignature() {
			return fmt.Errorf("Claim %d (%s): ohne Unterschrift kein Claim", i, c.Claim.Predicate)
		}
		// Der Name ist die Claim-ID — der Hash der signierten Nutzlast,
		// **ohne** das Unterschriftenmaterial (SPEC §7.2). Zwei
		// Unterschriften unter derselben Aussage sind derselbe Claim, und
		// die Kennung ist dieselbe, die der nekton-Kernel vergibt.
		id, err := c.ID()
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(id, "sha256:")[:16] + ".json"
		if err := writeJSON(filepath.Join(b.Dir, "kton", "claims", name), c.Envelope); err != nil {
			return fmt.Errorf("Claim %d: %w", i, err)
		}
		behalten[name] = true
	}
	if alt, err := os.ReadDir(filepath.Join(b.Dir, "kton", "claims")); err == nil {
		for _, e := range alt {
			if e.IsDir() || behalten[e.Name()] || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if err := os.Remove(filepath.Join(b.Dir, "kton", "claims", e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// AddFile legt eine Nutzlastdatei ab und liefert ihren Hash.
func (b *Bundle) AddFile(rel string, content []byte) (string, error) {
	rel = filepath.ToSlash(strings.TrimPrefix(rel, "./"))
	if strings.HasPrefix(rel, "kton/") {
		return "", fmt.Errorf("%q gehört nicht in die Nutzlast — kton/ ist für die Records", rel)
	}
	path := filepath.Join(b.Dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", err
	}
	return core.HashBytes(content), nil
}

// ReadFile liest eine Nutzlastdatei.
func (b *Bundle) ReadFile(rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(b.Dir, filepath.FromSlash(rel)))
}

// Step sucht einen Step.
func (r *Ray) Step(id string) *Step {
	for i := range r.Steps {
		if r.Steps[i].ID == id {
			return &r.Steps[i]
		}
	}
	return nil
}

// Hole sucht ein Loch.
func (r *Ray) Hole(name string) *Hole {
	for i := range r.Holes {
		if r.Holes[i].Name == name {
			return &r.Holes[i]
		}
	}
	return nil
}

// --- Prüfen -------------------------------------------------------------------

// Problem ist ein Befund.
type Problem struct {
	Step    string `json:"step,omitempty"`
	Slot    string `json:"slot,omitempty"`
	Message string `json:"message"`
}

func (p Problem) String() string {
	switch {
	case p.Step != "" && p.Slot != "":
		return fmt.Sprintf("%s/%s: %s", p.Step, p.Slot, p.Message)
	case p.Step != "":
		return fmt.Sprintf("%s: %s", p.Step, p.Message)
	}
	return p.Message
}

// Verify prüft ein Paket, ohne irgendetwas auszuführen: liegt jede feste
// Eingabe da, passt ihr Inhalt zu ihrem Hash, zeigt jede Kante auf einen
// vorhandenen Step, hat jedes Loch seine Stelle.
//
// Das ist die Prüfung, die ein Paket aus fremder Hand verdient — und sie
// braucht weder Repository noch Netz.
func (b *Bundle) Verify() []Problem {
	var out []Problem
	add := func(step, slot, msg string) { out = append(out, Problem{step, slot, msg}) }

	if b.Ray == nil || len(b.Ray.Steps) == 0 {
		add("", "", "das Paket enthält kein Potential")
		return out
	}
	seen := map[string]bool{}
	for _, s := range b.Ray.Steps {
		if s.ID == "" {
			add("", "", "ein Step ohne Kennung")
			continue
		}
		if seen[s.ID] {
			add(s.ID, "", "die Kennung kommt mehrfach vor")
		}
		seen[s.ID] = true
	}

	// Ein Claim, dessen Unterschrift hier niemand nachrechnen kann, ist kein
	// Fehler des Pakets — aber ein Befund: er sagt dann nur, was dasteht.
	for _, c := range b.Claims {
		if c.KeyID == "" {
			add("", "", fmt.Sprintf("der Claim %q ist nicht geprüft — "+
				"der Schlüssel liegt nicht bei", c.Claim.Predicate))
		}
	}
	b.checkStaleClaims(add)
	b.checkTestdaten(add)

	holeNames := map[string]bool{}
	for _, h := range b.Ray.Holes {
		holeNames[h.Name] = true
		step := b.Ray.Step(h.Step)
		if step == nil {
			add(h.Step, "", fmt.Sprintf("das Loch %q zeigt auf einen unbekannten Step", h.Name))
			continue
		}
		switch h.Type {
		case HoleParam:
			if h.Param == "" {
				add(h.Step, "", fmt.Sprintf("das Loch %q nennt keinen Parameter", h.Name))
			} else if _, has := step.Params[h.Param]; !has {
				add(h.Step, h.Param, fmt.Sprintf("der Parameter des Lochs %q fehlt am Step", h.Name))
			}
		case HoleFile, "":
			if h.Slot == "" {
				add(h.Step, "", fmt.Sprintf("das Loch %q nennt keinen Slot", h.Name))
			}
		default:
			add(h.Step, "", fmt.Sprintf("unbekannte Lochart %q", h.Type))
		}
	}

	for _, s := range b.Ray.Steps {
		slots := map[string]bool{}
		for _, in := range s.Inputs {
			if in.Slot == "" {
				add(s.ID, "", "eine Eingabe ohne Slot")
				continue
			}
			if slots[in.Slot] {
				// Zwei Eingaben an derselben Stelle sind mehrdeutig — der
				// Kernel weist das später ohnehin ab.
				add(s.ID, in.Slot, "der Slot ist doppelt belegt")
			}
			slots[in.Slot] = true

			switch {
			case in.Pinned():
				if in.Path == "" || in.Hash == "" {
					add(s.ID, in.Slot, "eine feste Eingabe braucht Pfad und Hash")
					continue
				}
				body, err := b.ReadFile(in.Path)
				if err != nil {
					add(s.ID, in.Slot, fmt.Sprintf("%s liegt nicht im Paket", in.Path))
					continue
				}
				if got := core.HashBytes(body); got != in.Hash {
					add(s.ID, in.Slot, fmt.Sprintf("%s passt nicht zu seinem Hash", in.Path))
				}
			case strings.HasPrefix(in.From, FromHole):
				name := strings.TrimPrefix(in.From, FromHole)
				if !holeNames[name] {
					add(s.ID, in.Slot, fmt.Sprintf("das Loch %q ist nicht erklärt", name))
				}
			case strings.HasPrefix(in.From, FromUpstream):
				up, _, ok := ParseUpstream(in.From)
				if !ok || b.Ray.Step(up) == nil {
					add(s.ID, in.Slot, fmt.Sprintf("die Kante zeigt auf den unbekannten Step %q", up))
				}
			default:
				add(s.ID, in.Slot, fmt.Sprintf("unbekannte Verdrahtung %q", in.From))
			}
		}
		for name, value := range s.Params {
			if strings.HasPrefix(value, FromHole) {
				hole := strings.TrimPrefix(value, FromHole)
				if !holeNames[hole] {
					add(s.ID, name, fmt.Sprintf("das Loch %q ist nicht erklärt", hole))
				}
			}
		}
	}

	if b.Spectrum != nil {
		out = append(out, b.verifySpectrum()...)
	}

	if _, err := b.Ray.Order(); err != nil {
		add("", "", err.Error())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// ParseUpstream zerlegt "upstream:<step>:<slot>".
func ParseUpstream(from string) (step, slot string, ok bool) {
	if !strings.HasPrefix(from, FromUpstream) {
		return "", "", false
	}
	rest := strings.TrimPrefix(from, FromUpstream)
	i := strings.LastIndex(rest, ":")
	if i < 0 {
		return rest, "", true
	}
	return rest[:i], rest[i+1:], true
}

// Order liefert die Steps in Abhängigkeitsreihenfolge. Ein Zyklus ist ein
// Fehler, kein Sonderfall.
func (r *Ray) Order() ([]string, error) {
	deps := map[string][]string{}
	known := map[string]bool{}
	var names []string
	for _, s := range r.Steps {
		known[s.ID] = true
		names = append(names, s.ID)
	}
	for _, s := range r.Steps {
		for _, in := range s.Inputs {
			if up, _, ok := ParseUpstream(in.From); ok && known[up] {
				deps[s.ID] = append(deps[s.ID], up)
			}
		}
		if s.Parent != "" && known[s.Parent] {
			deps[s.ID] = append(deps[s.ID], s.Parent)
		}
	}
	state := map[string]int{}
	var order []string
	var visit func(string, []string) error
	visit = func(id string, path []string) error {
		switch state[id] {
		case 2:
			return nil
		case 1:
			return fmt.Errorf("Zyklus im Potential: %s", strings.Join(append(path, id), " → "))
		}
		state[id] = 1
		for _, d := range deps[id] {
			if err := visit(d, append(path, id)); err != nil {
				return err
			}
		}
		state[id] = 2
		order = append(order, id)
		return nil
	}
	for _, n := range names {
		if err := visit(n, nil); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// --- kleine Helfer ---------------------------------------------------------

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// verifySpectrum prüft das Spectrum gegen das Paket.
//
// Zwei Zusagen, und beide lassen sich ohne Register und ohne Netz einlösen:
// zu jedem Member liegt seine **Referenzausgabe** im Paket und hat genau den
// genannten Hash, und der genannte Normalisierer ist ein **Step dieses Rays**.
// Ein Spectrum, dessen Referenzen niemand nachrechnen kann, ist eine Liste
// von Behauptungen.
func (b *Bundle) verifySpectrum() []Problem {
	var out []Problem
	s := b.Spectrum
	if s.Spectrum == "" {
		out = append(out, Problem{Message: "das Spectrum hat keine Kennung"})
	}
	if len(s.Members) == 0 {
		// SPEC §10: ein Werkzeug ohne Referenzmenge qualifiziert nichts.
		out = append(out, Problem{Message: "das Spectrum hat keine Member"})
	}
	for _, m := range s.Members {
		if !IsContentHash(m.Output) {
			out = append(out, Problem{Step: m.Name,
				Message: "die Referenzausgabe ist kein Inhaltshash"})
			continue
		}
		if _, err := b.Reference(m.Output); err != nil {
			out = append(out, Problem{Step: m.Name, Message: err.Error()})
		}
	}
	if s.Normalizer != "" {
		gefunden := false
		for _, step := range b.Ray.Steps {
			f, err := b.Ray.Foton(step.ID, nil, nil)
			if err != nil {
				continue
			}
			if f.Protocol.Ref == s.Normalizer {
				gefunden = true
				break
			}
		}
		if !gefunden {
			out = append(out, Problem{
				Message: "der genannte Normalisierer ist kein Step dieses Rays"})
		}
	}
	return out
}

// IsContentHash sagt, ob eine Zeichenkette ein "sha256:<64 hex>" ist.
func IsContentHash(s string) bool {
	const p = "sha256:"
	if !strings.HasPrefix(s, p) || len(s) != len(p)+64 {
		return false
	}
	for _, c := range s[len(p):] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// ID ist die Identität des **Pakets**: sein Potential, sein Spectrum und
// jede Datei der Nutzlast.
//
// Warum nicht nur das Potential: Anforderungen und Spezifikation gehören zum
// Paket, sind aber keine Eingabe einer Berechnung. Blieben sie außerhalb der
// Identität, könnte man eine Anforderung ändern, ohne dass es dem Paket
// anzusehen wäre — und die V-Struktur stünde auf einer Behauptung. Deckt die
// Identität die ganze Nutzlast, gilt: **wer eine Anforderung ändert, hat ein
// anderes Paket.**
//
// Ausgenommen sind `kton/claims/` und `kton/package.json`: ein Claim darf sein
// Subjekt nie verändern, und der Kopf trägt einen Zeitstempel. Beides gehört
// nicht in eine Identität.
func (b *Bundle) ID() (string, error) {
	rayID, err := b.Ray.ID()
	if err != nil {
		return "", err
	}
	files, err := b.payloadRefs()
	if err != nil {
		return "", err
	}
	m := map[string]any{"ray": rayID, "files": files}
	if len(b.Requires) > 0 {
		// Bedingungen zählen zur Identität — sonst ließen sie sich
		// nachträglich lockern, ohne dass es auffiele.
		rb, err := core.CanonValue(b.Requires)
		if err != nil {
			return "", err
		}
		m["requires"] = core.HashBytes(rb)
	}
	if b.Spectrum != nil {
		sb, err := core.CanonValue(b.Spectrum)
		if err != nil {
			return "", err
		}
		m["spectrum"] = core.HashBytes(sb)
	}
	raw, err := core.CanonValue(m)
	if err != nil {
		return "", err
	}
	return core.HashBytes(raw), nil
}

// payloadRefs listet jede Nutzlastdatei mit Pfad und Hash, in fester Ordnung.
func (b *Bundle) payloadRefs() ([]any, error) {
	var out []any
	err := filepath.Walk(b.Dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(b.Dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "kton/") {
			// Die Records: das Potential steckt über seine Identität drin,
			// Claims dürfen nicht, der Kopf trägt einen Zeitstempel.
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out = append(out, map[string]any{"path": rel, "hash": core.HashBytes(body)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].(map[string]any)["path"].(string) <
			out[j].(map[string]any)["path"].(string)
	})
	return out, nil
}

// checkStaleClaims meldet Claims, die von einer **anderen Fassung** sprechen.
//
// Ein Claim verändert sein Subjekt nie — das ist seine Stärke und zugleich
// die Stelle, an der er still falsch wird: ändert sich am Paket etwas, das in
// die Identität zählt (eine Anforderung, ein Skript, eine Referenzausgabe),
// dann steht der Claim noch da und spricht über etwas, das es hier nicht mehr
// gibt. Kein Fehler — das Paket ist stimmig, der Claim ist echt — aber
// niemand sollte ihn für einen Befund über *diese* Bytes halten.
//
// Als Subjekt gelten: das Paket, sein Potential und jedes seiner Fotonen.
func (b *Bundle) checkStaleClaims(add func(step, slot, msg string)) {
	if len(b.Claims) == 0 {
		return
	}
	gültig := map[string]bool{}
	if id, err := b.ID(); err == nil {
		gültig[id] = true
	}
	if b.Ray != nil {
		if id, err := b.Ray.ID(); err == nil {
			gültig[id] = true
		}
		for _, s := range b.Ray.Steps {
			if id, err := b.Ray.FotonID(s.ID, nil, nil); err == nil {
				gültig[id] = true
			}
		}
	}
	for _, c := range b.Claims {
		if !gültig[c.Claim.Subject.Hash] {
			add("", "", fmt.Sprintf("der Claim %q spricht über %s — das ist "+
				"weder dieses Paket noch eines seiner Fotonen; er stammt aus "+
				"einer anderen Fassung", c.Claim.Predicate, c.Claim.Subject.Hash))
		}
	}
}

// Reference liefert die Referenzausgabe zu einem Spectrum-Hash.
//
// Gesucht wird **über den Inhalt**, nicht über einen Dateinamen. Vorher stand
// hier `reference/<member>.out` — eine Konvention, die genau so lange trug,
// wie alle Member Textdateien waren. Ein Renderwerkzeug erzeugt `.html` und
// `.pdf`, und schon ist der Name Raterei. Der Hash ist ohnehin das, was das
// Spectrum nennt (SPEC §10): er bleibt die einzige Zusage, und die Datei
// darf heißen, wie sie will.
func (b *Bundle) Reference(hash string) ([]byte, error) {
	dir := filepath.Join(b.Dir, ReferenceDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("das Paket hat keinen %s/-Ordner", ReferenceDir)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if core.HashBytes(body) == hash {
			return body, nil
		}
	}
	return nil, fmt.Errorf("zu %s liegt keine Referenzausgabe im Paket (%s/ hat %d Dateien, keine passt)",
		kurzHash(hash), ReferenceDir, len(entries))
}

func kurzHash(h string) string {
	h = strings.TrimPrefix(h, "sha256:")
	if len(h) > 12 {
		return h[:12] + "…"
	}
	return h
}

// List nennt die Nutzlastdateien unter einem Verzeichnis, relativ zum Paket.
func (b *Bundle) List(dir string) ([]string, error) {
	root := filepath.Join(b.Dir, filepath.FromSlash(dir))
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, strings.TrimSuffix(dir, "/")+"/"+e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// HashOf ist der Inhaltshash in der Form, die überall sonst gilt.
func HashOf(body []byte) string { return core.HashBytes(body) }

// TestdatenDir hält die Daten, mit denen ein Paket sich selbst prüfen lässt.
const TestdatenDir = "testdaten"

// checkTestdaten hält die Regel fest: **ein Workflow bringt seinen
// Testdatensatz mit.**
//
// Der Grund ist nicht Ordnungsliebe. OQ heißt „rechnet es hier, was die
// Spezifikation sagt" — das ist nur zu beantworten, indem man es laufen
// lässt, und laufen lassen kann man es nur mit Daten. Ein Workflow, der seine
// Daten erst beim Anwender bekommt, ist bei ihm nicht prüfbar; er müsste
// glauben, was anderswo gemessen wurde.
//
// Darum: jedes Pflicht-Dateiloch nennt einen Vorgabewert, und der zeigt auf
// eine Datei **im Paket**. Damit läuft das Paket aus sich heraus, und der
// Anwender kann nachrechnen, bevor er seine eigenen Daten hineingibt.
//
// Geprüft wird das für `kind: ray` — ein Werkzeugpaket bringt seine Member
// ohnehin mit, ein Verticle bekommt seine Anfrage erst zur Laufzeit.
func (b *Bundle) checkTestdaten(add func(step, slot, msg string)) {
	if b.Package.Kind != KindRay || b.Ray == nil {
		return
	}
	for _, h := range b.Ray.Holes {
		if !h.Required || h.Type != HoleFile {
			continue
		}
		if h.Default == "" {
			add(h.Step, h.Slot, fmt.Sprintf(
				"das Loch %q hat keinen Testdatensatz — ohne Daten läuft der Workflow "+
					"nicht, und ohne Lauf gibt es kein OQ", h.Name))
			continue
		}
		if _, err := b.ReadFile(h.Default); err != nil {
			add(h.Step, h.Slot, fmt.Sprintf(
				"der Testdatensatz %q des Lochs %q liegt nicht im Paket", h.Default, h.Name))
		}
	}
}
