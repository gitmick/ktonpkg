package scope

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// RequiresFile is the source file that declares the packages a verticle needs:
//
//	{"packages": [{"package": "qm-core", "at": "r7", "head": "sha256:…", "why": "…"}]}
//
// package is required; at pins a revision label, head (only with at) the seal's head; why says
// what for. Without at, any installed revision of the package will do.
const RequiresFile = "requires.json"

// PackageRequire is one package a revision declares it needs. It stands as a dct:requires claim
// on the revision (a tool requirement of a ray is one on the ray, with a spectrum id).
type PackageRequire struct {
	Package string `json:"package"`
	At      string `json:"at,omitempty"`
	Head    string `json:"head,omitempty"`
	Why     string `json:"why,omitempty"`
}

type requiresDecl struct {
	Packages []PackageRequire `json:"packages"`
}

// ParseRequires reads requires.json strictly: an unknown field is an error, not something to skip.
func ParseRequires(body []byte) ([]PackageRequire, error) {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	var d requiresDecl
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("%s: %v", RequiresFile, err)
	}
	return d.Packages, nil
}

func (q PackageRequire) problems(self string) []string {
	var out []string
	add := func(format string, args ...any) {
		out = append(out, fmt.Sprintf("requires %q: ", q.Package)+fmt.Sprintf(format, args...))
	}
	switch {
	case !scopeLabel.MatchString(q.Package):
		add("not a package name")
	case q.Package == self:
		add("a package does not require itself")
	}
	if q.At != "" && !revisionLabel.MatchString(q.At) {
		add("at %q is not a revision label", q.At)
	}
	if q.Head != "" {
		if q.At == "" {
			add("a head pins a sealed revision, so it needs at")
		}
		if !hashPattern.MatchString(q.Head) {
			add("head %q is not sha256:<64 lower-case hex>", q.Head)
		}
	}
	return out
}

// RequirePackages records the packages the revision needs. Each package once; checked as
// PackageRequireProblems checks the claims.
func (r *Revision) RequirePackages(qs []PackageRequire) error {
	seen := map[string]bool{}
	var probs []string
	for _, q := range qs {
		probs = append(probs, q.problems(r.P.Name)...)
		if seen[q.Package] {
			probs = append(probs, fmt.Sprintf("requires %q twice", q.Package))
		}
		seen[q.Package] = true
	}
	if old, err := r.PackageRequires(); err != nil {
		return err
	} else {
		for _, q := range old {
			if seen[q.Package] {
				probs = append(probs, fmt.Sprintf("requires %q twice", q.Package))
			}
		}
	}
	if len(probs) > 0 {
		return fmt.Errorf("%s", strings.Join(probs, "; "))
	}
	for _, q := range qs {
		if _, err := r.Claim(r.ID, Requires, map[string]any{"value": toMap(q)}); err != nil {
			return err
		}
	}
	return nil
}

// PackageRequires are the packages the revision declares it needs, sorted by name.
func (r *Revision) PackageRequires() ([]PackageRequire, error) {
	claims, err := r.Claims()
	if err != nil {
		return nil, err
	}
	var out []PackageRequire
	for _, c := range claims {
		if c.Predicate != Requires || c.Subject != r.ID {
			continue
		}
		var q PackageRequire
		b, _ := json.Marshal(c.Object["value"])
		if err := json.Unmarshal(b, &q); err != nil || q.Package == "" {
			return nil, fmt.Errorf("the requires claim %s does not name a package", short(c.ID))
		}
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Package < out[j].Package })
	return out, nil
}

// PackageReference is a place in the revision that names another package.
type PackageReference struct {
	Package string
	Where   string // composition "<name>", then the part ids, as ForeignControl.Where
	What    string // block, query, composition, every, render, control
	Name    string // the block, query, composition, ray or control named
	At      string // a control's pin: the revision label
	Head    string // and the seal's head
}

// PackageReferences lists every place the revision's compositions name another package: a block,
// query, nested composition, every query or render ray taken {package, …}, and a control an act
// says from another package. In the order the compositions read, compositions sorted by name.
func (r *Revision) PackageReferences() ([]PackageReference, error) {
	comps, err := r.Compositions()
	if err != nil {
		return nil, err
	}
	var out []PackageReference
	ref := func(where, what string, raw json.RawMessage) {
		if len(raw) == 0 {
			return
		}
		if x, err := parseRef(raw, what); err == nil && x.Package != "" {
			out = append(out, PackageReference{Package: x.Package, Where: where, What: what, Name: x.Name})
		}
	}
	uses := func(where string, us []Use) {
		for i, u := range us {
			w := fmt.Sprintf("%s/use %d", where, i+1)
			if u.ID != "" {
				w = where + "/" + u.ID
			}
			ref(w, "block", u.Use)
			ref(w, "query", u.Query)
			if u.Every != nil {
				ref(w, "query", u.Every.Query)
			}
			for j, a := range u.Acts {
				if a.From != nil {
					out = append(out, PackageReference{Package: a.From.Package, Where: fmt.Sprintf("%s/act %d", w, j+1),
						What: "control", Name: a.Control, At: a.From.At, Head: a.From.Head})
				}
			}
		}
	}
	var parts func(where string, ps []Part)
	parts = func(where string, ps []Part) {
		for i, pt := range ps {
			w := fmt.Sprintf("%s/part %d", where, i+1)
			if pt.ID != "" {
				w = where + "/" + pt.ID
			}
			ref(w, "composition", pt.Composition)
			if pt.Every != nil {
				ref(w, "query", pt.Every.Query)
			}
			uses(w, pt.Blocks)
			parts(w, pt.Parts)
		}
	}
	for _, n := range sortedKeys(comps) {
		c := comps[n]
		where := fmt.Sprintf("composition %q", n)
		if c.Render != nil && c.Render.Package != "" {
			out = append(out, PackageReference{Package: c.Render.Package, Where: where, What: "render", Name: c.Render.Ray})
		}
		parts(where, c.Parts)
		uses(where+"/eachPart", c.EachPart)
	}
	return out, nil
}

// packageRequireProblems checks the declared requires: their form, each package once, and that a
// control pinned from a package does not contradict the revision the requires pin for it.
func (r *Revision) packageRequireProblems() []string {
	qs, err := r.PackageRequires()
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	by := map[string]PackageRequire{}
	for _, q := range qs {
		out = append(out, q.problems(r.P.Name)...)
		if _, dup := by[q.Package]; dup {
			out = append(out, fmt.Sprintf("requires %q twice", q.Package))
		}
		by[q.Package] = q
	}
	refs, err := r.PackageReferences()
	if err != nil {
		return append(out, err.Error())
	}
	for _, x := range refs {
		q, ok := by[x.Package]
		if !ok || x.What != "control" {
			continue
		}
		if q.At != "" && x.At != q.At {
			out = append(out, fmt.Sprintf("%s: the control is pinned at %s@%s; requires pins %s@%s",
				x.Where, x.Package, x.At, x.Package, q.At))
		} else if q.Head != "" && x.Head != q.Head {
			out = append(out, fmt.Sprintf("%s: the control pins the head %s; requires pins %s",
				x.Where, x.Head, q.Head))
		}
	}
	return out
}

// PackageRequireProblems checks a declared require against the package the host installed for it:
// installed is its sealed revision (nil when the package is not installed) and seal the seal
// Package.Sealed returned with it.
func PackageRequireProblems(q PackageRequire, installed *Revision, seal ClaimInfo) []string {
	if installed == nil {
		return []string{fmt.Sprintf("requires %q, which is not installed", q.Package)}
	}
	var out []string
	if installed.P.Name != q.Package {
		out = append(out, fmt.Sprintf("requires %q; the package given is %q", q.Package, installed.P.Name))
	}
	if q.At != "" && installed.Label != q.At {
		out = append(out, fmt.Sprintf("requires %s@%s; the installed revision is %s", q.Package, q.At, installed.Label))
	}
	if q.Head != "" {
		if seal.Subject != installed.ID {
			out = append(out, fmt.Sprintf("the seal given does not seal %s@%s", installed.P.Name, installed.Label))
		} else if head := strings.ToLower(str(seal.Object["hash"])); head != q.Head {
			out = append(out, fmt.Sprintf("requires the head %s; the seal names %s", q.Head, head))
		}
	}
	return out
}
