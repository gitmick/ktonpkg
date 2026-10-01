package scope

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gitmick/ktonpkg"
)

// WriteView schreibt die lesbare Ansicht einer Revision nach files/<revision>/.
//
// Die Ansicht ist abgeleitet: alles in ihr steht schon in den Registern und unter blobs/. Sie
// entscheidet nichts und wird nicht unterschrieben. Ihre Form ist die gewohnte — kton/ray.json,
// steps/<step>/<slot>, testdaten/<loch>/<slot> —, damit ein Mensch, ein Prüfer und ein Werkzeug,
// das noch das alte Ordnerformat liest, sie ohne weiteres öffnen können.
func (r *Revision) WriteView() (string, error) {
	dir := filepath.Join(r.P.Dir, FilesDir, r.Label)
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	ray, err := r.Ray()
	if err != nil {
		return "", err
	}
	claims, err := r.Claims()
	if err != nil {
		return "", err
	}
	b := ktonpkg.New(dir, ktonpkg.KindRay, ray.Name)
	b.Ray = ray
	b.Package.Created = time.Unix(0, 0).UTC().Format(time.RFC3339) // abgeleitet, also ohne eigene Zeit
	var requires []ktonpkg.Requirement
	var lines []string
	for _, c := range claims {
		lines = append(lines, fmt.Sprintf("| `%s` | %s | `%s` | %s |", short(c.ID), predicateName(c.Predicate),
			short(c.Subject), summary(c)))
		switch c.Predicate {
		case Requires:
			v := value(c)
			requires = append(requires, ktonpkg.Requirement{Kind: str(v["kind"]), Spectrum: str(c.Object["id"]),
				Name: str(v["name"])})
		case PartOf:
			v := value(c)
			var rel string
			switch str(v["role"]) {
			case RoleCode:
				rel = filepath.Join("steps", str(v["step"]), str(v["slot"]))
			case RoleTestData:
				if slot := str(v["slot"]); slot != "" {
					rel = filepath.Join("testdaten", str(v["hole"]), slot)
				}
			case RoleSpectrum:
				body, err := r.P.Blob(c.Subject)
				if err != nil {
					return "", err
				}
				var s ktonpkg.Spectrum
				if err := json.Unmarshal(body, &s); err != nil {
					return "", fmt.Errorf("das Spectrum %s: %w", short(c.Subject), err)
				}
				b.Spectrum = &s
			case RoleDocument, RoleReference:
				rel = str(v["path"])
			}
			if rel == "" {
				continue
			}
			body, err := r.P.Blob(c.Subject)
			if err != nil {
				return "", err
			}
			if _, err := b.AddFile(rel, body); err != nil {
				return "", err
			}
		}
	}
	b.Requires = requires
	if err := b.Write(); err != nil {
		return "", err
	}
	readme := []string{
		"# " + ray.Name + " — Revision " + r.Label,
		"",
		"Abgeleitete Ansicht. Maßgeblich sind die Register (`registry/`) und die Bytes (`blobs/`) des",
		"Pakets; diese Dateien werden daraus erzeugt und nicht unterschrieben.",
		"",
		"Paket `" + r.P.ID + "`  ",
		"Revision `" + r.ID + "`",
		"",
		"| Claim | Prädikat | Subjekt | Inhalt |",
		"|---|---|---|---|",
	}
	readme = append(readme, lines...)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(strings.Join(readme, "\n")+"\n"), 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

func predicateName(p string) string {
	switch p {
	case PartOf:
		return "isPartOf"
	case Requires:
		return "requires"
	case Description:
		return "description"
	case Rationale:
		return "hasRationale"
	case SealPredicate:
		return "sealedAt"
	}
	return p
}

func summary(c ClaimInfo) string {
	if v := value(c); v != nil {
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			s := fmt.Sprint(v[k])
			if strings.HasPrefix(s, "sha256:") {
				s = short(s)
			}
			parts = append(parts, k+"="+s)
		}
		return strings.Join(parts, ", ")
	}
	if s, ok := c.Object["value"].(string); ok {
		return s
	}
	return ""
}
