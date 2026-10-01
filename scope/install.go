package scope

import (
	"fmt"
	"path/filepath"

	nregistry "kton.dev/nekton/registry"
	pregistry "kton.dev/plankton/registry"
)

// Installieren ist die Grenze zwischen den zwei nektons. Das Paket ist das nekton der Autorin; was
// der User damit tut — einspielen, nachprüfen, qualifizieren, eigene Läufe —, schreibt er in sein
// eigenes. Install trägt die Records des Pakets in die Register des Users (Föderation: Vereinigung
// nach Hash, nichts wird umgeschrieben) und nennt, worauf sich der User von dort aus bezieht: die
// Revision und das Siegel, das ihren Kopf festhält.
//
// Install schreibt keinen Claim des Users. Dass er installiert hat, sagt er selbst, in seinem Scope
// und mit seinem Schlüssel — im Cockpit ein say mit dem Template installed.

// Installation ist, worauf sich der User nach dem Installieren bezieht.
type Installation struct {
	Package  string `json:"package"`
	Name     string `json:"name"`
	Revision string `json:"revision"`
	Label    string `json:"label"`
	Seal     string `json:"seal"`
	Head     string `json:"head"`
	Ray      string `json:"ray,omitempty"`
	Fotons   int    `json:"fotons"`
	Claims   int    `json:"claims"`
}

// Install prüft das Paket und trägt seine Records in die Register des Users ein. Ein Paket mit
// Strukturbefund oder eine Revision ohne Siegel wird nicht installiert: ohne Siegel gibt es keinen
// festen Stand, auf den sich ein Urteil des Users beziehen könnte.
func (p *Package) Install(label, planktonDir, nektonDir string) (*Installation, error) {
	if problems := p.Verify(); len(problems) > 0 {
		return nil, fmt.Errorf("das Paket hat %d Strukturbefund(e), erster: %s", len(problems), problems[0])
	}
	r, err := p.Revision(label)
	if err != nil {
		return nil, err
	}
	seals, err := p.Seals()
	if err != nil {
		return nil, err
	}
	in := &Installation{Package: p.ID, Name: p.Name, Revision: r.ID, Label: r.Label}
	for _, s := range seals { // in Kettenfolge: das letzte Siegel der Revision gilt
		if s.Subject == r.ID {
			in.Seal, in.Head = s.ID, str(s.Object["hash"])
		}
	}
	if in.Seal == "" {
		return nil, fmt.Errorf("die Revision %s ist nicht versiegelt", label)
	}
	in.Ray, _ = r.RayID()

	src, err := pregistry.Open(filepath.Join(p.Dir, PlanktonDir))
	if err != nil {
		return nil, err
	}
	dst, err := pregistry.Open(planktonDir)
	if err != nil {
		return nil, err
	}
	for _, rec := range src.Records(0) {
		if _, _, err := dst.Add(rec.Envelope); err != nil {
			return nil, fmt.Errorf("plankton %s: %w", short(rec.FotonID), err)
		}
		in.Fotons++
	}
	nsrc, err := nregistry.Open(filepath.Join(p.Dir, NektonDir))
	if err != nil {
		return nil, err
	}
	ndst, err := nregistry.Open(nektonDir)
	if err != nil {
		return nil, err
	}
	for _, rec := range nsrc.Records(0) {
		if _, _, err := ndst.Add(rec.Envelope); err != nil {
			return nil, fmt.Errorf("nekton %s: %w", short(rec.ClaimID), err)
		}
		in.Claims++
	}
	return in, nil
}
