package scope

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Member is one part of a revision, as an isPartOf claim names it: its role, its name (empty for a
// part without one, such as a step), and its hash, sha256:<lower-case hex>. For a ray, step or run
// the hash is the plankton record's id; every other role names bytes under blobs/.
type Member struct {
	Role string
	Name string
	Hash string
}

var hashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Members lists the revision's parts, sorted by role, then name, then hash. A part whose hash is
// not sha256:<64 hex> is an error, not something to skip.
func (r *Revision) Members() ([]Member, error) {
	claims, err := r.Claims()
	if err != nil {
		return nil, err
	}
	var out []Member
	for _, c := range claims {
		if c.Predicate != PartOf {
			continue
		}
		v := value(c)
		h := strings.ToLower(c.Subject)
		if !hashPattern.MatchString(h) {
			return nil, fmt.Errorf("revision %s: the part %q is not named by a sha256 hash", r.Label, c.Subject)
		}
		out = append(out, Member{Role: str(v["role"]), Name: str(v["name"]), Hash: h})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Role != b.Role {
			return a.Role < b.Role
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Hash < b.Hash
	})
	return out, nil
}

// MemberBytes returns the bytes of a part of the revision by its hash, checked against it. A hash
// that no isPartOf claim of the revision names is refused even when the blob is in the package.
func (r *Revision) MemberBytes(hash string) ([]byte, error) {
	ms, err := r.Members()
	if err != nil {
		return nil, err
	}
	h := strings.ToLower(hash)
	for _, m := range ms {
		if m.Hash != h {
			continue
		}
		switch m.Role {
		case RoleRay, RoleStep, RoleRun:
			return nil, fmt.Errorf("%s is a %s, a plankton record, not bytes", hash, m.Role)
		}
		return r.P.Blob(h)
	}
	return nil, fmt.Errorf("%s is not a part of %s@%s", hash, r.P.Name, r.Label)
}
