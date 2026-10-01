package scope

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	nregistry "kton.dev/nekton/registry"
	"kton.dev/plankton/core"
	pregistry "kton.dev/plankton/registry"
)

// Verify prüft die Struktur eines Pakets — und nur die. Ob eine Revision freigegeben, ein Ray
// qualifiziert, ein Dokument in Kraft ist, beantwortet das Profil der Paketart (SPARQL über die
// Claims), nicht diese Funktion.
//
// Geprüft wird:
//   - jeder Record ist von einem beiliegenden Schlüssel unterschrieben (ein Hinweis, kein Vertrauen:
//     welchen Schlüsseln man glaubt, entscheidet der Leser mit seinen Trust-Tiers);
//   - jede Revision ist ein Sub-Seed des Pakets, ihre Kette hat einen Kopf;
//   - jeder Teil, den ein isPartOf-Claim nennt, liegt im Paket: ein Record im plankton-Register
//     oder Bytes unter blobs/, die zu ihrem Hash passen;
//   - jeder Ray nennt Potentiale, die im Paket liegen;
//   - jedes Siegel nennt eine Revision des Pakets und einen Claim aus deren Kette.
func (p *Package) Verify() []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	keys, err := p.keys()
	if err != nil {
		return []string{err.Error()}
	}
	nreg, err := nregistry.Open(filepath.Join(p.Dir, NektonDir))
	if err != nil {
		return []string{err.Error()}
	}
	preg, err := pregistry.Open(filepath.Join(p.Dir, PlanktonDir))
	if err != nil {
		return []string{err.Error()}
	}
	for _, rec := range nreg.Records(0) {
		if core.VerifiedSignerKeyID(rec.Envelope, keys) == "" {
			add("der Claim %s ist von keinem beiliegenden Schlüssel unterschrieben", short(rec.ClaimID))
		}
	}
	for _, rec := range preg.Records(0) {
		if core.VerifiedSignerKeyID(rec.Envelope, keys) == "" {
			add("der Record %s ist von keinem beiliegenden Schlüssel unterschrieben", short(rec.FotonID))
		}
	}

	revs, err := p.Revisions()
	if err != nil {
		return append(out, err.Error())
	}
	inRev := map[string]map[string]bool{} // Revision → Claim-Ids ihrer Kette
	for _, r := range revs {
		if _, _, ok := nreg.Heads(r.ID); !ok {
			add("die Revision %s hat keine lesbare Kette", r.Label)
			continue
		}
		claims, err := r.Claims()
		if err != nil {
			add("Revision %s: %v", r.Label, err)
			continue
		}
		inRev[r.ID] = map[string]bool{r.ID: true}
		for _, c := range claims {
			inRev[r.ID][c.ID] = true
			if c.Predicate != PartOf {
				continue
			}
			v := value(c)
			role := str(v["role"])
			switch role {
			case RoleRay, RoleStep, RoleRun:
				f, ok := preg.Foton(c.Subject)
				if !ok {
					add("Revision %s: %s %s liegt nicht im Paket", r.Label, role, short(c.Subject))
					continue
				}
				if role == RoleRay {
					var desc rayDescriptor
					if err := fromMap(f.Protocol.Descriptor, &desc); err != nil {
						add("Revision %s: der Ray %s ist nicht lesbar: %v", r.Label, short(c.Subject), err)
						continue
					}
					for _, s := range desc.Steps {
						if _, ok := preg.Foton(s.Potential); !ok {
							add("Revision %s: der Ray nennt das Potential von %s, das nicht im Paket liegt", r.Label, s.ID)
						}
					}
				}
			default:
				if _, err := p.Blob(c.Subject); err != nil {
					add("Revision %s: %s %s: %v", r.Label, role, short(c.Subject), err)
				}
				// Eine Ausgabe, die einen Lauf nennt, muss eine Ausgabe dieses Laufs sein.
				if run := str(v["run"]); run != "" {
					f, ok := preg.Foton(run)
					if !ok {
						add("Revision %s: %s %s nennt den Lauf %s, der nicht im Paket liegt", r.Label, role, str(v["path"]), short(run))
						break
					}
					found := false
					for _, o := range f.Outputs {
						found = found || o.Hash == c.Subject
					}
					if !found {
						add("Revision %s: %s %s ist keine Ausgabe des Laufs %s", r.Label, role, str(v["path"]), short(run))
					}
				}
			}
		}
	}
	seals, err := p.Seals()
	if err != nil {
		return append(out, err.Error())
	}
	for _, s := range seals {
		chain, ok := inRev[s.Subject]
		if !ok {
			add("das Siegel %s nennt keine Revision dieses Pakets", short(s.ID))
			continue
		}
		if h := str(s.Object["hash"]); !chain[h] {
			add("das Siegel %s nennt einen Kopf, der nicht in der Kette der Revision steht", short(s.ID))
		}
	}
	// Seeds, die weder das Paket noch eine seiner Revisionen sind, gehören nicht hierher.
	seeds, _ := readSeeds(p.Dir)
	for _, s := range seeds {
		if s.ID != p.ID && s.Parent != p.ID {
			add("der Seed %s (%s) gehört nicht zu diesem Paket", short(s.ID), s.Name)
		}
	}
	sort.Strings(out)
	return out
}

func (p *Package) keys() ([]ed25519.PublicKey, error) {
	entries, err := os.ReadDir(filepath.Join(p.Dir, KeyDir))
	if err != nil {
		return nil, fmt.Errorf("%s fehlt", KeyDir)
	}
	var out []ed25519.PublicKey
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".pub") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(p.Dir, KeyDir, e.Name()))
		if err != nil {
			return nil, err
		}
		raw, err := hex.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%s ist kein öffentlicher Schlüssel", e.Name())
		}
		out = append(out, ed25519.PublicKey(raw))
	}
	return out, nil
}

func short(id string) string {
	if len(id) > 23 {
		return id[:23] + "…"
	}
	return id
}
