package scope

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BuildTemplates legt ein Paket des Template-Profils an: die Claim-Templates aus templateDir
// (<name>.json) und die SPARQL-Abfragen aus queryDir (<name>.rq), als Revision label, versiegelt.
// Ein solches Paket ist das Vokabular, das ein Cockpit für eine Art von Arbeit braucht — die Wörter,
// mit denen es Aussagen macht, und die Fragen, mit denen es sucht.
func BuildTemplates(dir, name, label, templateDir, queryDir string, key ed25519.PrivateKey, opt Options) (*Package, *Revision, error) {
	p, err := Create(dir, name, key, opt)
	if err != nil {
		return nil, nil, err
	}
	r, err := p.NewRevision(label)
	if err != nil {
		return nil, nil, err
	}
	if err := r.SetProfile(ProfileTemplates); err != nil {
		return nil, nil, err
	}
	add := func(src, ext string, put func(string, []byte) (string, error)) (int, error) {
		if src == "" {
			return 0, nil
		}
		files, err := filepath.Glob(filepath.Join(src, "*"+ext))
		if err != nil {
			return 0, err
		}
		for _, f := range files {
			body, err := os.ReadFile(f)
			if err != nil {
				return 0, err
			}
			if _, err := put(strings.TrimSuffix(filepath.Base(f), ext), body); err != nil {
				return 0, err
			}
		}
		return len(files), nil
	}
	nt, err := add(templateDir, ".json", r.AddTemplate)
	if err != nil {
		return nil, nil, err
	}
	nq, err := add(queryDir, ".rq", r.AddQuery)
	if err != nil {
		return nil, nil, err
	}
	if nt+nq == 0 {
		return nil, nil, fmt.Errorf("weder Templates in %s noch Abfragen in %s", templateDir, queryDir)
	}
	if _, err := r.Seal(); err != nil {
		return nil, nil, err
	}
	return p, r, nil
}
