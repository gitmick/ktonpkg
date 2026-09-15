package ktonpkg

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kton.dev/plankton/core"
)

// beispielHash steht für ein Subjekt — der Wert ist beliebig, die Form nicht.
var beispielHash = strings.Repeat("ab", 32)

func testSigner(t *testing.T) *Signer {
	t.Helper()
	s, err := GenerateSigner(filepath.Join(t.TempDir(), "test.key"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Die Wire-Form ist die der SPEC: ein in-toto-Statement mit dem
// Claim-Prädikatstyp, verpackt in einen DSSE-Umschlag.
func TestSignedClaimIsAnInTotoStatement(t *testing.T) {
	s := testSigner(t)
	signed, err := s.Sign(Claim{
		Subject:   Ref{Hash: "sha256:" + beispielHash, Name: "raute"},
		Predicate: "https://improve.scinteco.com/terms/runs-on",
		Object:    map[string]any{"tool": "jam-r"},
		By:        "michael", When: "2026-09-14T08:20:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if signed.Envelope.PayloadType != PayloadType {
		t.Fatalf("Nutzlasttyp %q", signed.Envelope.PayloadType)
	}
	st, err := signed.Envelope.Statement()
	if err != nil {
		t.Fatal(err)
	}
	if st.PredicateType != ClaimPredicateType {
		t.Fatalf("Prädikatstyp %q", st.PredicateType)
	}
	if len(st.Subject) != 1 || st.Subject[0].Name != "raute" {
		t.Fatalf("Subject %+v", st.Subject)
	}
	if st.Subject[0].Digest["sha256"] != beispielHash {
		t.Fatalf("Digest %+v", st.Subject[0].Digest)
	}
	if ok, err := signed.Envelope.Verify(s.Pub); err != nil || !ok {
		t.Fatalf("die eigene Unterschrift muss aufgehen: %v %v", ok, err)
	}
}

// Der Claim kommt unverändert wieder heraus — sonst wäre die Unterschrift
// unter etwas anderem als dem, was gelesen wird.
func TestClaimSurvivesTheRoundTrip(t *testing.T) {
	s := testSigner(t)
	c := Claim{
		Subject:   Ref{Hash: "sha256:" + beispielHash, Name: "jam-r"},
		Predicate: "https://improve.scinteco.com/terms/qualifies-as",
		Object:    map[string]any{"spectrum": "jam-r", "level": "L0"},
		By:        "toolinstall", When: "2026-09-14T08:20:00Z",
		Why:      "alle Member reproduziert",
		Evidence: []Ref{{Hash: "sha256:" + beispielHash, Name: "qualify"}},
	}
	signed, err := s.Sign(c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ClaimFromEnvelope(signed.Envelope, []ed25519.PublicKey{s.Pub})
	if err != nil {
		t.Fatal(err)
	}
	if back.KeyID != s.KeyID {
		t.Fatalf("keyid %q, erwartet %q", back.KeyID, s.KeyID)
	}
	vorher, err := ClaimID(c)
	if err != nil {
		t.Fatal(err)
	}
	nachher, err := ClaimID(back.Claim)
	if err != nil {
		t.Fatal(err)
	}
	if vorher != nachher {
		t.Fatalf("der Claim hat sich unterwegs verändert:\n%+v\n%+v", c, back.Claim)
	}
}

// Ein Claim ohne Unterschrift ist keiner (SPEC §7.2) — und wird beim Lesen
// nicht durchgewunken.
func TestUnsignedClaimIsRefused(t *testing.T) {
	b := raute(t)
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	blank, _ := json.Marshal(core.Envelope{PayloadType: PayloadType, Payload: "e30="})
	dir := filepath.Join(b.Dir, "kton", "claims")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nackt.json"), blank, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(b.Dir); err == nil {
		t.Fatal("ein Claim ohne Unterschrift muss abgelehnt werden")
	}
}

// Wird an einem Claim etwas verändert, geht die Unterschrift nicht mehr auf —
// und das ist ein Befund, kein „ungeprüft".
func TestTamperedClaimIsRefused(t *testing.T) {
	b := raute(t)
	if err := b.AddClaim(testSigner(t), Claim{
		Subject:   Ref{Hash: "sha256:" + beispielHash, Name: "raute"},
		Predicate: "https://improve.scinteco.com/terms/runs-on",
		Object:    map[string]any{"tool": "jam-r"},
		By:        "michael",
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(b.Dir, "kton", "claims")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ein Claim erwartet: %v %v", entries, err)
	}
	path := filepath.Join(dir, entries[0].Name())
	var env core.Envelope
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	st, err := env.Statement()
	if err != nil {
		t.Fatal(err)
	}
	st.Predicate = json.RawMessage(`{"predicate":"https://beispiel/etwas-anderes","by":"wer auch immer","when":"2026-09-14T08:20:00Z"}`)
	payload, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	env.Payload = base64.StdEncoding.EncodeToString(payload)
	neu, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, neu, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(b.Dir); err == nil {
		t.Fatal("ein verfälschter Claim muss auffallen")
	}
}

// Ein beigelegter Schlüssel ist kein Inhalt: er verändert die
// Paketidentität nicht.
func TestAttachedKeyDoesNotChangeThePackageID(t *testing.T) {
	b := raute(t)
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	vorher, err := b.ID()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AttachKey(testSigner(t).Pub); err != nil {
		t.Fatal(err)
	}
	nachher, err := b.ID()
	if err != nil {
		t.Fatal(err)
	}
	if vorher != nachher {
		t.Fatal("ein beigelegter Schlüssel darf die Paketidentität nicht verändern")
	}
}

// Eine Schlüsseldatei ist die einzige Kopie ihres Seeds.
func TestKeyfileIsNeverOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k.key")
	first, err := GenerateSigner(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateSigner(path); err == nil {
		t.Fatal("eine vorhandene Schlüsseldatei darf nicht überschrieben werden")
	}
	again, err := OpenOrCreateSigner(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.KeyID != first.KeyID {
		t.Fatalf("derselbe Pfad muss denselben Schlüssel liefern: %s vs %s",
			again.KeyID, first.KeyID)
	}
}

// Ein Claim spricht über eine Fassung. Ändert sich am Paket etwas, das in die
// Identität zählt, steht er noch da — und muss auffallen.
func TestClaimFromAnotherVersionIsReported(t *testing.T) {
	b := raute(t)
	if err := b.AddClaim(testSigner(t), Claim{
		Subject:   Ref{Hash: "sha256:" + beispielHash, Name: "eine andere Fassung"},
		Predicate: "https://improve.scinteco.com/terms/runs-on",
		By:        "irgendwer",
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	problems := b.Verify()
	if len(problems) == 0 {
		t.Fatal("ein Claim über eine andere Fassung muss auffallen")
	}
	if !strings.Contains(problems[0].Message, "anderen Fassung") {
		t.Fatalf("unerwarteter Befund: %v", problems)
	}
}

// Ein Claim über das Paket selbst ist kein Befund.
func TestClaimAboutThisPackageIsFine(t *testing.T) {
	b := raute(t)
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	id, err := b.ID()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddClaim(testSigner(t), Claim{
		Subject:   Ref{Hash: id, Name: "raute"},
		Predicate: "https://improve.scinteco.com/terms/runs-on",
		By:        "michael",
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}
	if problems := b.Verify(); len(problems) != 0 {
		t.Fatalf("kein Befund erwartet, bekam %v", problems)
	}
}
