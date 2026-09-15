package ktonpkg

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kton.dev/plankton/core"
)

// Claims signieren.
//
// SPEC §7.2 ist an dieser Stelle unmissverständlich: *„A claim is constituted
// by its signature: a claim carrying no signature is not a nekton claim and a
// conforming kernel MUST reject it on ingest."* Bis hierher trugen unsere
// Pakete deshalb **Vormerkungen**, keine Claims. Das ändert sich hier.
//
// Die Wire-Form ist die der SPEC (§7.3): ein in-toto-Statement mit
// `predicateType = https://kton.dev/claim/v0`, dessen Subject der Subject-Ref
// des Claims ist und dessen Predicate den Rest trägt — verpackt in einen
// DSSE-Umschlag (§8). Maßgeblich ist dabei nicht unsere Lesart der SPEC,
// sondern der Kernel: was hier entsteht, nimmt `nekton add` auf und
// `nekton verify` bestätigt es. Wo beides auseinanderging, hat der Kernel
// recht behalten — siehe `claimPredicate`.
//
// Zwei Dinge, die dabei ausdrücklich **nicht** gelten:
//
//   - `by` ist eine Selbstauskunft. Maßgeblich ist die **keyid** (§7.2) —
//     deshalb führt jeder gelesene Claim seine geprüfte keyid neben `by`.
//   - Eine geprüfte Signatur heißt „unverändert seit dem Signieren", nicht
//     „von jemandem, dem zu trauen ist". Wem ein Schlüssel gehört, steht
//     nicht im Paket.

// PayloadType ist der DSSE-Nutzlasttyp für in-toto-Statements.
const PayloadType = "application/vnd.in-toto+json"

// ClaimPredicateType ist der Statement-Typ eines nekton-Claims (SPEC §7.3).
const ClaimPredicateType = "https://kton.dev/claim/v0"

// KeyDir ist der Ort der beigelegten öffentlichen Schlüssel im Paket.
const KeyDir = "kton/keys"

// Signer ist eine Unterschrift leistende Kennung.
type Signer struct {
	priv  ed25519.PrivateKey
	Pub   ed25519.PublicKey
	KeyID string
}

// LoadSigner liest eine Schlüsseldatei: 64 Hexzeichen, der Ed25519-Seed —
// dieselbe Form, die `plankton keygen` schreibt.
func LoadSigner(path string) (*Signer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("%s: kein Hex-Seed: %w", path, err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s: %d Bytes Seed, erwartet %d", path, len(seed), ed25519.SeedSize)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return &Signer{priv: priv, Pub: pub, KeyID: core.KeyIDHex(pub)}, nil
}

// GenerateSigner legt ein Schlüsselpaar an — und **überschreibt nie**: eine
// vorhandene Schlüsseldatei ist die einzige Kopie ihres Seeds.
func GenerateSigner(path string) (*Signer, error) {
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("%s gibt es schon — eine Schlüsseldatei wird nicht überschrieben", path)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	seed := hex.EncodeToString(priv.Seed())
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		return nil, err
	}
	pubPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".pub"
	if err := os.WriteFile(pubPath, []byte(hex.EncodeToString(pub)), 0o644); err != nil {
		return nil, err
	}
	return &Signer{priv: priv, Pub: pub, KeyID: core.KeyIDHex(pub)}, nil
}

// SignedClaim ist ein Claim samt seinem Umschlag und der geprüften keyid.
type SignedClaim struct {
	Claim Claim
	// KeyID ist die **geprüfte** Unterschrift — leer, solange nicht geprüft
	// werden konnte.
	KeyID    string
	Envelope core.Envelope
}

// claimPredicate ist der Teil, der im Statement unter `predicate` steht —
// in **der Form des nekton-Kernels** (SPEC §7.1/§7.2), nicht in unserer
// bequemen: `predicate` ist ein Term (Hash und/oder IRI), `object` ist
// entweder ein Verweis oder ein Literal. Das ist der Preis dafür, dass
// `nekton add` unsere Claims annimmt — und der ist es wert: sonst hätten wir
// eine zweite, private Claim-Sprache.
type claimPredicate struct {
	Predicate termRef   `json:"predicate"`
	Object    *objOrLit `json:"object,omitempty"`
	By        string    `json:"by"`
	When      string    `json:"when"`
	Why       string    `json:"why,omitempty"`
	Evidence  []wireRef `json:"evidence,omitempty"`
}

type termRef struct {
	Hash string `json:"hash,omitempty"`
	URI  string `json:"uri,omitempty"`
}

type objOrLit struct {
	// Hash trägt ein **inhaltsadressiertes** Objekt. Der Kernel indiziert
	// `hash` und `uri` vor `id` (SPEC §7.2) — wer einen Hash in `id`
	// schreibt, macht ihn unauffindbar für die Abfrage, die ihn sucht.
	// Nachgesehen: `nekton by object sha256:…` fand nichts, solange der
	// Spectrum-Hash in `value` steckte.
	Hash  string `json:"hash,omitempty"`
	ID    string `json:"id,omitempty"`
	Value any    `json:"value,omitempty"`
}

// wireRef ist ein Verweis, wie nekton ihn liest: Hash und/oder IRI (SPEC
// §7.1). `name` steht daneben, weil ein Name keine Adresse ist — ihn in
// `uri` zu schreiben wäre bequem und falsch. Der Kernel indiziert über den
// Hash und übergeht den Rest; ein Leser sieht, woran die Evidenz hängt.
type wireRef struct {
	Hash string `json:"hash,omitempty"`
	URI  string `json:"uri,omitempty"`
	Name string `json:"name,omitempty"`
}

// term macht aus unserem Prädikat einen Term: ein Inhaltshash bleibt ein
// Hash, alles andere ist eine IRI.
func term(s string) termRef {
	if strings.HasPrefix(s, "sha256:") {
		return termRef{Hash: s}
	}
	return termRef{URI: s}
}

func (t termRef) String() string {
	if t.Hash != "" {
		return t.Hash
	}
	return t.URI
}

// Sign macht aus einem Claim einen signierten Claim.
func (s *Signer) Sign(c Claim) (*SignedClaim, error) {
	if c.Subject.Hash == "" {
		return nil, fmt.Errorf("ein Claim ohne Subjekt ist keiner")
	}
	if c.Predicate == "" {
		return nil, fmt.Errorf("ein Claim ohne Prädikat ist keiner")
	}
	if c.When == "" {
		c.When = time.Now().UTC().Format(time.RFC3339)
	}
	wire := claimPredicate{
		Predicate: term(c.Predicate), By: c.By, When: c.When, Why: c.Why,
	}
	if c.ObjectID != "" || len(c.Object) > 0 {
		wire.Object = &objOrLit{}
		if strings.HasPrefix(c.ObjectID, "sha256:") {
			wire.Object.Hash = c.ObjectID
		} else {
			wire.Object.ID = c.ObjectID
		}
		if len(c.Object) > 0 {
			wire.Object.Value = c.Object
		}
	}
	for _, e := range c.Evidence {
		wire.Evidence = append(wire.Evidence, wireRef{Hash: e.Hash, Name: e.Name})
	}
	pred, err := core.CanonValue(wire)
	if err != nil {
		return nil, err
	}
	st := core.Statement{
		Type:          "https://in-toto.io/Statement/v1",
		Subject:       []core.Subject{{Name: c.Subject.Name, Digest: digestOf(c.Subject.Hash)}},
		PredicateType: ClaimPredicateType,
		Predicate:     pred,
	}
	// **Kanonisch signieren.** Die Claim-Kennung ist der Hash der
	// kanonischen Nutzlast (nekton, SPEC §7.2) — signiert man etwas anderes,
	// hat derselbe Claim hier und dort zwei Kennungen.
	payload, err := core.CanonValue(st)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(s.priv, core.PAE(PayloadType, payload))
	env := core.Envelope{
		PayloadType: PayloadType,
		Payload:     base64.StdEncoding.EncodeToString(payload),
	}
	env.Signatures = append(env.Signatures, struct {
		KeyID string `json:"keyid"`
		Sig   string `json:"sig"`
	}{KeyID: s.KeyID, Sig: base64.StdEncoding.EncodeToString(sig)})
	return &SignedClaim{Claim: c, KeyID: s.KeyID, Envelope: env}, nil
}

// SignStatement unterschreibt ein beliebiges in-toto-Statement.
//
// Gebraucht wird das für **Fotonen**: sie sind keine Claims, tragen also
// einen anderen `predicateType` und eine andere Nutzlast — aber dasselbe
// Verfahren. Signiert werden die **kanonischen** Bytes, sonst hätte dieselbe
// Aussage zwei Kennungen.
func (s *Signer) SignStatement(st core.Statement) (*core.Envelope, error) {
	payload, err := core.CanonValue(st)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(s.priv, core.PAE(PayloadType, payload))
	env := core.Envelope{
		PayloadType: PayloadType,
		Payload:     base64.StdEncoding.EncodeToString(payload),
	}
	env.Signatures = append(env.Signatures, struct {
		KeyID string `json:"keyid"`
		Sig   string `json:"sig"`
	}{KeyID: s.KeyID, Sig: base64.StdEncoding.EncodeToString(sig)})
	return &env, nil
}

func digestOf(hash string) map[string]string {
	return map[string]string{"sha256": strings.TrimPrefix(hash, "sha256:")}
}

// ID ist die Kennung des Claims: der Hash der signierten Nutzlast — also
// **ohne** das Unterschriftenmaterial (SPEC §7.2). Dieselbe Rechnung wie im
// nekton-Kernel, damit ein Claim hier und dort dieselbe Kennung hat.
func (s SignedClaim) ID() (string, error) {
	payload, err := s.Envelope.PayloadBytes()
	if err != nil {
		return "", err
	}
	canon, err := core.CanonJSON(payload)
	if err != nil {
		return "", err
	}
	return core.HashBytes(canon), nil
}

// ClaimFromEnvelope liest einen Claim aus einem Umschlag und prüft seine
// Unterschrift gegen die mitgegebenen Schlüssel.
//
// Findet sich kein passender Schlüssel, ist das **kein** Fehler des Claims —
// er ist dann bloß ungeprüft, und `KeyID` bleibt leer. Diese Unterscheidung
// ist wichtig genug, um sie nicht zu verwischen: „nicht prüfbar" und
// „verfälscht" sind zwei verschiedene Befunde.
func ClaimFromEnvelope(env core.Envelope, keys []ed25519.PublicKey) (*SignedClaim, error) {
	st, err := env.Statement()
	if err != nil {
		return nil, err
	}
	if st.PredicateType != ClaimPredicateType {
		return nil, fmt.Errorf("kein Claim, sondern %q", st.PredicateType)
	}
	var pred claimPredicate
	if err := json.Unmarshal(st.Predicate, &pred); err != nil {
		return nil, err
	}
	c := Claim{
		Predicate: pred.Predicate.String(), By: pred.By,
		When: pred.When, Why: pred.Why,
	}
	if pred.Object != nil {
		c.ObjectID = pred.Object.ID
		if pred.Object.Hash != "" {
			c.ObjectID = pred.Object.Hash
		}
		if m, ok := pred.Object.Value.(map[string]any); ok {
			c.Object = m
		}
	}
	for _, e := range pred.Evidence {
		c.Evidence = append(c.Evidence, Ref{Hash: e.Hash, Name: e.Name})
	}
	if len(st.Subject) > 0 {
		c.Subject = Ref{Name: st.Subject[0].Name}
		if d, has := st.Subject[0].Digest["sha256"]; has {
			c.Subject.Hash = "sha256:" + d
		}
	}
	out := &SignedClaim{Claim: c, Envelope: env}
	if keyid := core.VerifiedSignerKeyID(env, keys); keyid != "" {
		out.KeyID = keyid
		return out, nil
	}
	// Kein Schlüssel passt. Zwei Fälle, und sie sind nicht dasselbe: liegt
	// der Schlüssel bei, auf den sich der Umschlag beruft, und die
	// Unterschrift geht trotzdem nicht auf, ist etwas verfälscht worden.
	// Liegt er nicht bei, ist der Claim bloß **ungeprüft**.
	for _, sig := range env.Signatures {
		for _, k := range keys {
			if sig.KeyID != "" && sig.KeyID == core.KeyIDHex(k) {
				return nil, fmt.Errorf("die Unterschrift von %s geht nicht auf", sig.KeyID)
			}
		}
	}
	return out, nil
}

// AttachKey legt einen öffentlichen Schlüssel ins Paket, damit ein Leser die
// Unterschriften **prüfen** kann, ohne ihn anderswoher zu holen.
//
// Das macht aus dem Paket keine Vertrauensquelle: dass eine Unterschrift zum
// Schlüssel passt, sagt „unverändert seit dem Signieren" — wem der Schlüssel
// gehört, steht nicht darin.
func (b *Bundle) AttachKey(pub ed25519.PublicKey) error {
	keyid := core.KeyIDHex(pub)
	path := filepath.Join(b.Dir, filepath.FromSlash(KeyDir), keyid+".pub")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(hex.EncodeToString(pub)), 0o644)
}

// AttachedKeys liest die beigelegten öffentlichen Schlüssel.
func (b *Bundle) AttachedKeys() ([]ed25519.PublicKey, error) {
	dir := filepath.Join(b.Dir, filepath.FromSlash(KeyDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // keine beigelegten Schlüssel ist kein Fehler
	}
	var out []ed25519.PublicKey
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, pub)
	}
	return out, nil
}

// AddClaim signiert einen Claim und legt ihn ins Paket — samt dem
// öffentlichen Schlüssel, mit dem er zu prüfen ist.
func (b *Bundle) AddClaim(s *Signer, c Claim) error {
	signed, err := s.Sign(c)
	if err != nil {
		return err
	}
	b.Claims = append(b.Claims, *signed)
	return b.AttachKey(s.Pub)
}

// OpenOrCreateSigner lädt eine Schlüsseldatei oder legt sie an, wenn es noch
// keine gibt.
func OpenOrCreateSigner(path string) (*Signer, error) {
	s, err := LoadSigner(path)
	if err == nil {
		return s, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	return GenerateSigner(path)
}
