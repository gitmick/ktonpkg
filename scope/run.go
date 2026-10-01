package scope

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"kton.dev/plankton/core"
	pregistry "kton.dev/plankton/registry"
)

// Läufe im Paket. Eine Referenzausgabe ist nicht eine Datei, die jemand beigelegt hat, sondern die
// Ausgabe eines Laufs: der Ray, mit den Testdaten realisiert. Ebenso ist ein gerendertes Dokument
// die Ausgabe eines Renderlaufs über seine Quellen. Der Lauf kommt als signierter plankton-Record
// ins Paket, seine Ausgaben als Bytes, und ein Claim sagt, wozu er da ist.

// Arten eines Laufs, im Objekt seines isPartOf-Claims.
const (
	RunReference = "reference" // der Ray mit den Testdaten realisiert; seine Ausgaben sind die Referenz
	RunRender    = "render"    // Dokumentquellen zu einem Dokument gerendert
)

// RoleReference ist eine Referenzausgabe: Bytes, die ein Lauf der Art RunReference erzeugt hat.
const RoleReference = "reference"

// AddRun nimmt einen Lauf in die Revision auf. env ist der signierte Record, pub der Schlüssel, der
// ihn unterschrieben hat (er kommt nach keys/, damit Verify ihn ohne Netz prüfen kann), outputs die
// Bytes seiner Ausgaben nach Pfad. extra steht neben Rolle und Art im Claim, etwa der Step.
//
// Jede Ausgabe wird gegen den Hash im Record geprüft: ein Paket, dessen Referenz nicht die Ausgabe
// des Laufs ist, der sie belegen soll, wird nicht gebaut.
func (r *Revision) AddRun(env core.Envelope, pub ed25519.PublicKey, kind string, extra map[string]any,
	outputs map[string][]byte) (string, error) {
	p := r.P
	if core.VerifiedSignerKeyID(env, []ed25519.PublicKey{pub}) == "" {
		return "", fmt.Errorf("der Lauf ist nicht mit dem angegebenen Schlüssel unterschrieben")
	}
	if err := p.AddKey(pub); err != nil {
		return "", err
	}
	id, err := p.AddFoton(env)
	if err != nil {
		return "", err
	}
	reg, err := pregistry.Open(filepath.Join(p.Dir, PlanktonDir))
	if err != nil {
		return "", err
	}
	f, ok := reg.Foton(id)
	if !ok {
		return "", fmt.Errorf("der Lauf %s ist nach dem Aufnehmen nicht lesbar", short(id))
	}
	for _, o := range f.Outputs {
		if o.Hash == "" {
			return "", fmt.Errorf("der Record %s hat eine Ausgabe ohne Hash (%s): ein Potential, kein Lauf", short(id), o.Path)
		}
	}
	obj := map[string]any{"kind": kind}
	for k, v := range extra {
		obj[k] = v
	}
	if _, err := r.Claim(id, PartOf, member(RoleRun, obj)); err != nil {
		return "", err
	}
	role := map[string]string{RunReference: RoleReference, RunRender: RoleDocument}[kind]
	for _, o := range f.Outputs {
		body, ok := outputs[o.Path]
		if !ok {
			return "", fmt.Errorf("die Bytes der Ausgabe %s des Laufs fehlen", o.Path)
		}
		h, err := p.PutBlob(body)
		if err != nil {
			return "", err
		}
		if h != o.Hash {
			return "", fmt.Errorf("die Ausgabe %s hat den Hash %s, der Lauf nennt %s", o.Path, short(h), short(o.Hash))
		}
		if role == "" {
			continue
		}
		rel := path.Base(o.Path)
		if s, ok := extra["step"].(string); ok && role == RoleReference {
			rel = s + "-" + rel
		}
		dir := map[string]string{RoleReference: "reference", RoleDocument: "dokumentation"}[role]
		if _, err := r.Claim(h, PartOf, member(role, map[string]any{"path": path.Join(dir, rel), "run": id,
			"output": o.Path})); err != nil {
			return "", err
		}
	}
	return id, nil
}

// AddKey legt einen weiteren öffentlichen Schlüssel nach keys/ — den eines Laufs, der nicht mit dem
// Paketschlüssel unterschrieben ist. Ein Hinweis zum Prüfen, kein Vertrauen.
func (p *Package) AddKey(pub ed25519.PublicKey) error {
	path := filepath.Join(p.Dir, KeyDir, core.KeyIDHex(pub)+".pub")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(hex.EncodeToString(pub)), 0o644)
}
