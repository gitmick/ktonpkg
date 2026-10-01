// Package scope ist das Paketformat als nekton-Scope (kton-cockpit ADR-009).
//
// Ein Paket hat kein eigenes Format mehr. Es ist ein Ausschnitt aus einem Register: plankton-Records
// (Potentiale, Läufe), nekton-Claims über sie, und die Bytes dahinter.
//
//	<paket>/
//	  registry/nekton/    der Seed des Pakets, je Revision ein Sub-Seed, die Ketten, die Siegel
//	  registry/plankton/  Potentiale und Läufe
//	  blobs/sha256/<hex>  die Bytes, nach Inhalt
//	  keys/<keyid>.pub    die öffentlichen Schlüssel der Unterzeichner — ein Hinweis, kein Vertrauen
//	  files/<revision>/   eine lesbare Ansicht, aus dem Rest abgeleitet; sie entscheidet nichts
//
// Der Seed des Pakets nennt das Dokument und bleibt über alle Revisionen gleich; seine Id ist die
// Paket-Id. Jede Revision ist ein Sub-Seed mit dem Paket als parent. Alles, was zu einer Revision
// gehört, hängt in ihrer Kette, in Reihenfolge. Ein Siegel trägt den Kopf einer Revision in die
// Kette des Pakets ein; es darf mehrere geben, auch Zwischensiegel.
//
// Der Seed trägt nur den Namen. Alles andere über das Dokument — Art, Zweck, Verantwortliche —
// steht als Claim in seinem Scope und ist damit geordnet, signiert und änderbar, ohne die
// Paket-Id zu berühren.
//
// Ein Paket ist das nekton der Autorin: was sie gebaut, beschrieben, begründet und versiegelt hat.
// Was beim User geschieht — Installation, Verifikation, Qualifizierung (IQ/OQ), eigene Reviews —,
// gehört in das nekton des Users und verweist von dort per Hash auf Paket, Revision und Siegel.
// Deshalb trägt ein Paket keine verifies- oder qualifies-Claims, und Verify verlangt keine: ein
// Paket ist ohne das Urteil eines Users vollständig, und kein User erbt das Urteil eines anderen.
//
// Was eine Folge von Siegeln bedeutet — freigegeben, in Kraft —, entscheidet hier nichts. Das ist
// eine Regel über die Claims, geprüft mit SPARQL (das Profil einer Paketart), nicht eine Eigenschaft
// des Formats. Verify prüft nur Struktur: Unterschriften, Ketten, Hashes, Vollständigkeit.
//
// Seeds, Ketten und Siegel schreibt der Kernel (kton.dev/nekton), Potentiale und Läufe ebenso
// (kton.dev/plankton). Hier wird keine Identität nachgerechnet.
package scope

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	nclaim "kton.dev/nekton/claim"
	nregistry "kton.dev/nekton/registry"
	"kton.dev/plankton/core"
	pregistry "kton.dev/plankton/registry"
)

// Die Orte im Paket.
const (
	NektonDir   = "registry/nekton"
	PlanktonDir = "registry/plankton"
	BlobDir     = "blobs/sha256"
	KeyDir      = "keys"
	FilesDir    = "files"
)

// Das Vokabular. Veröffentlichte Begriffe, wo es sie gibt.
const (
	// SealPredicate ist das Prädikat, mit dem das kton-cockpit Scopes versiegelt (cockpit scope seal).
	SealPredicate = "https://kton.dev/v/sealedAt"
	// PartOf macht einen Record zum Teil der Revision, in deren Kette der Claim steht.
	PartOf = "http://purl.org/dc/terms/isPartOf"
	// Requires ist eine Bedingung an die Umgebung, etwa ein Werkzeug eines Spectrums.
	Requires = "http://purl.org/dc/terms/requires"
	// Description und Rationale sind die Templates describes und rationale des Cockpits.
	Description = "http://purl.org/dc/terms/description"
	Rationale   = "https://w3id.org/ep-plan#hasRationale"
)

// Package ist ein geöffnetes Paket.
type Package struct {
	Dir  string
	ID   string // die Id des Paket-Seeds
	Name string

	key   ed25519.PrivateKey // nur zum Schreiben
	clock func() time.Time
}

// Options steuert das Anlegen.
type Options struct {
	// Clock liefert die Zeit, die in Seeds und Claims steht. Ohne Angabe: jetzt. Eine feste Uhr
	// ergibt feste Ids — für Tests und für Pakete, die reproduzierbar entstehen sollen.
	Clock func() time.Time
	// OnlyMeasured lässt beim Überführen die Referenzausgaben und Dokumente des alten Pakets weg,
	// weil sie mit den Läufen nachkommen, die sie erzeugt haben (Revision.AddRun).
	OnlyMeasured bool
}

// Create legt ein Paket an: den Seed, der das Dokument nennt.
func Create(dir, name string, key ed25519.PrivateKey, opt Options) (*Package, error) {
	if name == "" {
		return nil, fmt.Errorf("ein Paket braucht einen Namen")
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%s ist nicht leer — ein Paket entsteht neu", dir)
	}
	p := &Package{Dir: dir, Name: name, key: key, clock: opt.Clock}
	if p.clock == nil {
		p.clock = time.Now
	}
	for _, d := range []string{NektonDir, PlanktonDir, BlobDir, KeyDir} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return nil, err
		}
	}
	if err := p.writeKey(); err != nil {
		return nil, err
	}
	id, err := p.seed(name, "")
	if err != nil {
		return nil, err
	}
	p.ID = id
	return p, nil
}

// Open öffnet ein Paket. Das Paket ist der eine Seed ohne parent.
func Open(dir string) (*Package, error) {
	seeds, err := readSeeds(dir)
	if err != nil {
		return nil, err
	}
	var roots []seedInfo
	for _, s := range seeds {
		if s.Parent == "" {
			roots = append(roots, s)
		}
	}
	if len(roots) != 1 {
		return nil, fmt.Errorf("%s: genau ein Seed ohne parent erwartet, gefunden %d", dir, len(roots))
	}
	return &Package{Dir: dir, ID: roots[0].ID, Name: roots[0].Name, clock: time.Now}, nil
}

// WithSigner gibt das Paket zum Schreiben frei.
func (p *Package) WithSigner(key ed25519.PrivateKey, opt Options) *Package {
	p.key = key
	if opt.Clock != nil {
		p.clock = opt.Clock
	}
	_ = p.writeKey()
	return p
}

// Revision ist eine Revision des Pakets: ein Sub-Seed.
type Revision struct {
	P     *Package
	ID    string
	Label string
}

// NewRevision öffnet eine Revision.
func (p *Package) NewRevision(label string) (*Revision, error) {
	if label == "" {
		return nil, fmt.Errorf("eine Revision braucht eine Bezeichnung")
	}
	id, err := p.seed(p.Name+"@"+label, p.ID)
	if err != nil {
		return nil, err
	}
	return &Revision{P: p, ID: id, Label: label}, nil
}

// Revisions listet die Revisionen, nach Bezeichnung.
func (p *Package) Revisions() ([]*Revision, error) {
	seeds, err := readSeeds(p.Dir)
	if err != nil {
		return nil, err
	}
	var out []*Revision
	for _, s := range seeds {
		if s.Parent == p.ID {
			out = append(out, &Revision{P: p, ID: s.ID, Label: strings.TrimPrefix(s.Name, p.Name+"@")})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}

// Revision findet eine Revision nach Bezeichnung.
func (p *Package) Revision(label string) (*Revision, error) {
	revs, err := p.Revisions()
	if err != nil {
		return nil, err
	}
	for _, r := range revs {
		if r.Label == label {
			return r, nil
		}
	}
	return nil, fmt.Errorf("keine Revision %q", label)
}

// Claim schreibt einen Claim in die Kette der Revision und gibt seine Id zurück.
func (r *Revision) Claim(subject, predicate string, object map[string]any) (string, error) {
	return r.P.claim(r.ID, subject, predicate, object)
}

// Seal trägt den Kopf der Revision in die Kette des Pakets ein. Zwischensiegel sind erlaubt; was
// ein Siegel bedeutet, sagt das Profil der Paketart, nicht das Format.
func (r *Revision) Seal() (string, error) {
	head, err := r.P.head(r.ID)
	if err != nil {
		return "", err
	}
	return r.P.claim(r.P.ID, r.ID, SealPredicate, map[string]any{"hash": head})
}

// Claims listet die Claims einer Revision in Kettenreihenfolge.
func (r *Revision) Claims() ([]ClaimInfo, error) { return r.P.chain(r.ID) }

// Seals listet die Siegel des Pakets, in Kettenreihenfolge.
func (p *Package) Seals() ([]ClaimInfo, error) {
	cs, err := p.chain(p.ID)
	if err != nil {
		return nil, err
	}
	var out []ClaimInfo
	for _, c := range cs {
		if c.Predicate == SealPredicate {
			out = append(out, c)
		}
	}
	return out, nil
}

// ClaimInfo ist ein gelesener Claim.
type ClaimInfo struct {
	ID        string
	Subject   string
	Predicate string
	Object    map[string]any
	Prev      string
	Signer    string // keyid
}

// --- Bytes -----------------------------------------------------------------------------------------

// PutBlob legt Bytes nach ihrem Inhalt ab und gibt den Hash zurück.
func (p *Package) PutBlob(body []byte) (string, error) {
	h := core.HashBytes(body)
	path := p.blobPath(h)
	if _, err := os.Stat(path); err == nil {
		return h, nil
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	return h, nil
}

// Blob liest Bytes nach Hash und prüft sie.
func (p *Package) Blob(hash string) ([]byte, error) {
	body, err := os.ReadFile(p.blobPath(hash))
	if err != nil {
		return nil, fmt.Errorf("%s liegt nicht im Paket", hash)
	}
	if got := core.HashBytes(body); got != hash {
		return nil, fmt.Errorf("die Bytes zu %s haben den Hash %s", hash, got)
	}
	return body, nil
}

func (p *Package) blobPath(hash string) string {
	return filepath.Join(p.Dir, BlobDir, strings.TrimPrefix(hash, "sha256:"))
}

// --- Kernel ----------------------------------------------------------------------------------------

func (p *Package) keyID() (string, error) {
	if p.key == nil {
		return "", fmt.Errorf("das Paket ist nur zum Lesen geöffnet")
	}
	return core.KeyIDHex(p.key.Public().(ed25519.PublicKey)), nil
}

func (p *Package) writeKey() error {
	if p.key == nil {
		return nil
	}
	id, _ := p.keyID()
	pub := p.key.Public().(ed25519.PublicKey)
	return os.WriteFile(filepath.Join(p.Dir, KeyDir, id+".pub"), []byte(hex.EncodeToString(pub)), 0o644)
}

// seed öffnet einen Scope, so wie das Cockpit es tut (cockpit scope seed).
func (p *Package) seed(name, parent string) (string, error) {
	keyid, err := p.keyID()
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"scope":   name,
		"genesis": true,
		"by":      "key:" + keyid,
		"when":    p.clock().UTC().Format(time.RFC3339),
	}
	if parent != "" {
		body["parent"] = map[string]any{"hash": parent}
	}
	spec := nclaim.Spec{
		Subject:       []nclaim.SubjectSpec{{URI: "urn:nekton:scope:" + name}},
		PredicateType: nclaim.ScopePredicateType,
		PredicateBody: body,
	}
	return p.addClaim(spec)
}

func (p *Package) claim(scope, subject, predicate string, object map[string]any) (string, error) {
	keyid, err := p.keyID()
	if err != nil {
		return "", err
	}
	prev, err := p.head(scope)
	if err != nil {
		return "", err
	}
	spec := nclaim.Spec{
		Predicate: predicate,
		Object:    object,
		By:        "key:" + keyid,
		When:      p.clock().UTC().Format(time.RFC3339),
		Scope:     scope,
		Prev:      prev,
	}
	if strings.HasPrefix(subject, "sha256:") {
		spec.Subject = []nclaim.SubjectSpec{{Hash: subject}}
	} else {
		spec.Subject = []nclaim.SubjectSpec{{URI: subject}}
	}
	return p.addClaim(spec)
}

func (p *Package) addClaim(spec nclaim.Spec) (string, error) {
	env, id, err := nclaim.SignWith(spec, p.key)
	if err != nil {
		return "", err
	}
	reg, err := nregistry.Open(filepath.Join(p.Dir, NektonDir))
	if err != nil {
		return "", err
	}
	if _, _, err := reg.Add(env); err != nil {
		return "", fmt.Errorf("das nekton-Register hat %s abgelehnt: %w", id, err)
	}
	return id, nil
}

// AddFoton nimmt einen signierten plankton-Record auf (ein Potential oder einen Lauf).
func (p *Package) AddFoton(env core.Envelope) (string, error) {
	reg, err := pregistry.Open(filepath.Join(p.Dir, PlanktonDir))
	if err != nil {
		return "", err
	}
	id, _, err := reg.Add(env)
	return id, err
}

// head ist der Kopf eines Scopes; bei einem frischen Scope der Seed selbst.
func (p *Package) head(scope string) (string, error) {
	reg, err := nregistry.Open(filepath.Join(p.Dir, NektonDir))
	if err != nil {
		return "", err
	}
	heads, _, ok := reg.Heads(scope)
	if !ok {
		return "", fmt.Errorf("kein Scope %s im Paket", scope)
	}
	if len(heads) != 1 {
		return "", fmt.Errorf("der Scope %s hat %d Köpfe — eine Revision wird in einer Linie geschrieben", scope, len(heads))
	}
	return heads[0], nil
}

// chain liest die Claims eines Scopes in Kettenreihenfolge (vom Seed zum Kopf).
func (p *Package) chain(scope string) ([]ClaimInfo, error) {
	reg, err := nregistry.Open(filepath.Join(p.Dir, NektonDir))
	if err != nil {
		return nil, err
	}
	byPrev := map[string]ClaimInfo{}
	for _, rec := range reg.Records(0) {
		st, _, err := nclaim.ParseEnvelope(rec.Envelope)
		if err != nil || st.IsSeed() {
			continue
		}
		pr, err := st.ParsePredicate()
		if err != nil || pr.Scope != scope {
			continue
		}
		ci := ClaimInfo{ID: rec.ClaimID, Predicate: pr.Predicate.Key(), Prev: pr.Prev}
		if len(st.Subject) > 0 {
			ci.Subject = st.Subject[0].Key()
		}
		var body struct {
			Object map[string]any `json:"object"`
		}
		if json.Unmarshal(st.Predicate, &body) == nil {
			ci.Object = body.Object
		}
		if len(rec.Envelope.Signatures) > 0 {
			ci.Signer = rec.Envelope.Signatures[0].KeyID
		}
		byPrev[pr.Prev] = ci
	}
	var out []ClaimInfo
	for cur := scope; ; {
		ci, ok := byPrev[cur]
		if !ok {
			break
		}
		out = append(out, ci)
		cur = ci.ID
	}
	return out, nil
}

type seedInfo struct{ ID, Name, Parent string }

func readSeeds(dir string) ([]seedInfo, error) {
	if _, err := os.Stat(filepath.Join(dir, NektonDir)); err != nil {
		return nil, fmt.Errorf("%s ist kein Paket: %s fehlt", dir, NektonDir)
	}
	reg, err := nregistry.Open(filepath.Join(dir, NektonDir))
	if err != nil {
		return nil, err
	}
	var out []seedInfo
	for _, rec := range reg.Records(0) {
		st, _, err := nclaim.ParseEnvelope(rec.Envelope)
		if err != nil || !st.IsSeed() {
			continue
		}
		var body struct {
			Scope  string `json:"scope"`
			Parent *struct {
				Hash string `json:"hash"`
			} `json:"parent"`
		}
		if err := json.Unmarshal(st.Predicate, &body); err != nil {
			continue
		}
		s := seedInfo{ID: rec.ClaimID, Name: body.Scope}
		if body.Parent != nil {
			s.Parent = body.Parent.Hash
		}
		out = append(out, s)
	}
	return out, nil
}
