package scope

import (
	"fmt"
	"sort"
	"strings"
)

// Profile ist die Art eines Pakets: was es enthält und welche Regeln für es gelten. Sie steht als
// Claim in der Revision, nicht im Seed (der trägt nur den Namen) — eine Revision sagt, welchem
// Profil sie entspricht (dcterms:conformsTo), und ein SPARQL-Profil prüft, ob das stimmt.
const (
	ConformsTo = "http://purl.org/dc/terms/conformsTo"

	ProfileWorkflow  = "https://kton.dev/profile/workflow"
	ProfileTool      = "https://kton.dev/profile/tool"
	ProfileTemplates = "https://kton.dev/profile/templates"
)

// Rollen des Template-Profils.
const (
	RoleTemplate = "template" // ein nekton-Claim-Template (JSON), Name im Objekt
	RoleQuery    = "query"    // eine SPARQL-Abfrage (.rq), Name im Objekt
)

// SetProfile hält fest, welchem Profil die Revision entspricht.
func (r *Revision) SetProfile(profile string) error {
	_, err := r.Claim(r.ID, ConformsTo, map[string]any{"id": profile})
	return err
}

// Profile ist das Profil der Revision ("" ohne Angabe; bei mehreren gilt die letzte).
func (r *Revision) Profile() (string, error) {
	claims, err := r.Claims()
	if err != nil {
		return "", err
	}
	p := ""
	for _, c := range claims {
		if c.Predicate == ConformsTo && c.Subject == r.ID {
			p = str(c.Object["id"])
		}
	}
	return p, nil
}

// AddTemplate legt ein Claim-Template als Teil der Revision ab.
func (r *Revision) AddTemplate(name string, body []byte) (string, error) {
	return r.addNamed(RoleTemplate, name, body)
}

// AddQuery legt eine SPARQL-Abfrage als Teil der Revision ab.
func (r *Revision) AddQuery(name string, body []byte) (string, error) {
	return r.addNamed(RoleQuery, name, body)
}

func (r *Revision) addNamed(role, name string, body []byte) (string, error) {
	if name == "" {
		return "", fmt.Errorf("a %s needs a name", role)
	}
	if msg := memberNameProblem(name); msg != "" {
		return "", fmt.Errorf("a %s %s", role, msg)
	}
	h, err := r.P.PutBlob(body)
	if err != nil {
		return "", err
	}
	if _, err := r.Claim(h, PartOf, member(role, map[string]any{"name": name})); err != nil {
		return "", err
	}
	return h, nil
}

// Templates sind die Claim-Templates der Revision nach Name, die Bytes gegen ihren Hash geprüft.
func (r *Revision) Templates() (map[string][]byte, error) { return r.named(RoleTemplate) }

// Queries sind die SPARQL-Abfragen der Revision nach Name.
func (r *Revision) Queries() (map[string][]byte, error) { return r.named(RoleQuery) }

func (r *Revision) named(role string) (map[string][]byte, error) {
	claims, err := r.Claims()
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, c := range claims {
		if c.Predicate != PartOf {
			continue
		}
		v := value(c)
		if str(v["role"]) != role {
			continue
		}
		if msg := memberNameProblem(str(v["name"])); msg != "" {
			return nil, fmt.Errorf("revision %s: a %s %s", r.Label, role, msg)
		}
		body, err := r.P.Blob(c.Subject)
		if err != nil {
			return nil, err
		}
		out[str(v["name"])] = body
	}
	return out, nil
}

// Sealed ist die zuletzt versiegelte Revision des Pakets, mit Siegel und Kopf. Was gilt, ist das
// Versiegelte: eine Revision, an der noch geschrieben wird, liefert keine Templates und keine
// Abfragen an Dritte.
func (p *Package) Sealed() (*Revision, ClaimInfo, error) {
	seals, err := p.Seals()
	if err != nil {
		return nil, ClaimInfo{}, err
	}
	revs, err := p.Revisions()
	if err != nil {
		return nil, ClaimInfo{}, err
	}
	byID := map[string]*Revision{}
	for _, r := range revs {
		byID[r.ID] = r
	}
	for i := len(seals) - 1; i >= 0; i-- {
		if r, ok := byID[seals[i].Subject]; ok {
			return r, seals[i], nil
		}
	}
	return nil, ClaimInfo{}, fmt.Errorf("package %s has no sealed revision", p.Name)
}

// Names gibt die Schlüssel einer Map sortiert zurück.
func Names(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// memberNameProblem says what is wrong with a member's name or path, or "" when nothing is. A name
// is a relative path with "/" between non-empty segments: no ".", "..", leading "/", backslash or
// colon, so no consumer that writes members to disk can be led outside its folder (kton-review F1).
// The colon is refused anywhere, not only as a drive prefix: on NTFS "a:b" writes the alternate
// data stream b of a, and the cockpit and the shell run on Windows hosts too. One rule, checked
// on write (Revision.Claim) and on read (Verify, the named readers, Members).
func memberNameProblem(name string) string {
	if strings.ContainsAny(name, "\\:") {
		return fmt.Sprintf("name %q has a backslash or a colon", name)
	}
	for _, seg := range strings.Split(name, "/") {
		switch seg {
		case "":
			return fmt.Sprintf("name %q has an empty segment or a leading or trailing /", name)
		case ".", "..":
			return fmt.Sprintf("name %q has a %s segment", name, seg)
		}
	}
	return ""
}
