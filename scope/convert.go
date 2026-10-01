package scope

import (
	"crypto/ed25519"
	"fmt"
	"path"

	"github.com/gitmick/ktonpkg"
)

// FromBundle überführt ein Paket im alten Ordnerformat (kton-package/1, Art ray) in ein Paket als
// Scope: Seed für das Paket, Sub-Seed für die Revision, der Ray als Potentiale, alles Übrige als
// Teile der Revision.
//
// Was das alte Format nicht hatte, kann die Überführung nicht erfinden. Die Referenzausgaben kommen
// als Bytes mit, ohne den Messlauf, der sie erzeugt hat; die Dokumente als Quellen, ohne
// Renderlauf. Beides steht so in den Claims (Rolle "reference" und "document"), damit ein Profil
// es vom Ziel unterscheiden kann.
//
// verifies-Claims und die übrigen Claims des alten Pakets kommen nicht mit: sie sind Urteile eines
// Users über die Berechnung und gehören in dessen nekton, nicht in das Paket der Autorin.
func FromBundle(b *ktonpkg.Bundle, dir, label string, key ed25519.PrivateKey, opt Options) (*Package, *Revision, error) {
	if b.Ray == nil || b.Package.Kind != ktonpkg.KindRay {
		return nil, nil, fmt.Errorf("%s ist kein Workflow-Paket", b.Dir)
	}
	p, err := Create(dir, b.Package.Name, key, opt)
	if err != nil {
		return nil, nil, err
	}
	r, err := p.NewRevision(label)
	if err != nil {
		return nil, nil, err
	}
	if err := r.SetProfile(ProfileWorkflow); err != nil {
		return nil, nil, err
	}
	// Die Bytes fester Eingaben liegen im alten Paket unter ihrem Pfad.
	byHash := map[string]string{}
	for _, s := range b.Ray.Steps {
		for _, in := range s.Inputs {
			if in.Pinned() {
				byHash[in.Hash] = in.Path
			}
		}
	}
	fetch := func(h string) ([]byte, error) {
		rel, ok := byHash[h]
		if !ok {
			return nil, fmt.Errorf("keine Datei zu %s im Paket", h)
		}
		return b.ReadFile(rel)
	}
	testData := map[string][]byte{}
	for _, h := range b.Ray.Holes {
		if h.Default == "" {
			continue
		}
		if h.Type == ktonpkg.HoleFile {
			body, err := b.ReadFile(h.Default)
			if err != nil {
				return nil, nil, fmt.Errorf("Testdaten von %s: %w", h.Name, err)
			}
			testData[h.Name] = body
		} else {
			testData[h.Name] = []byte(h.Default)
		}
	}
	rayID, err := r.AddRay(b.Ray, fetch, testData)
	if err != nil {
		return nil, nil, err
	}
	if b.Spectrum != nil {
		raw, err := b.ReadFile(ktonpkg.SpectrumEntry)
		if err != nil {
			return nil, nil, err
		}
		h, err := p.PutBlob(raw)
		if err != nil {
			return nil, nil, err
		}
		sid, err := b.Spectrum.ID()
		if err != nil {
			return nil, nil, err
		}
		if _, err := r.Claim(h, PartOf, member(RoleSpectrum, map[string]any{"spectrum": sid})); err != nil {
			return nil, nil, err
		}
		refs, _ := b.List(ktonpkg.ReferenceDir)
		if opt.OnlyMeasured {
			refs = nil
		}
		for _, rel := range refs {
			body, err := b.ReadFile(rel)
			if err != nil {
				return nil, nil, err
			}
			h, err := p.PutBlob(body)
			if err != nil {
				return nil, nil, err
			}
			if _, err := r.Claim(h, PartOf, member(RoleReference, map[string]any{"path": rel,
				"note": "aus kton-package/1 übernommen: Bytes ohne Messlauf"})); err != nil {
				return nil, nil, err
			}
		}
	}
	for _, q := range b.Requires {
		obj := map[string]any{"id": q.Spectrum, "value": map[string]any{"kind": q.Kind, "name": q.Name}}
		if _, err := r.Claim(rayID, Requires, obj); err != nil {
			return nil, nil, err
		}
	}
	for _, d := range []string{"requirements", "specification"} {
		files, _ := b.List(d)
		if opt.OnlyMeasured {
			files = nil
		}
		for _, rel := range files {
			body, err := b.ReadFile(rel)
			if err != nil {
				return nil, nil, err
			}
			h, err := p.PutBlob(body)
			if err != nil {
				return nil, nil, err
			}
			if _, err := r.Claim(h, PartOf, member(RoleDocument, map[string]any{"path": path.Clean(rel),
				"note": "Quelle; der Renderlauf fehlt im alten Format"})); err != nil {
				return nil, nil, err
			}
		}
	}
	return p, r, nil
}
