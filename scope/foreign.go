package scope

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// PackagePin names another package's sealed revision: its name, the revision's label (at), and the
// head its seal names (head). The head fixes the whole revision, so the control and the templates
// it says cannot change under a view that pins them; a new owner revision means a new pin.
type PackagePin struct {
	Package string `json:"package"`
	At      string `json:"at"`
	Head    string `json:"head"`
}

var revisionLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (pin *PackagePin) problems(self string) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	switch {
	case !scopeLabel.MatchString(pin.Package):
		add("from.package %q is not a package name", pin.Package)
	case pin.Package == self:
		add("from names this package; a control of its own is named without from")
	}
	if !revisionLabel.MatchString(pin.At) {
		add("from.at %q is not a revision label", pin.At)
	}
	if !hashPattern.MatchString(pin.Head) {
		add("from.head %q is not sha256:<64 lower-case hex>", pin.Head)
	}
	return out
}

// controlFields are the fields of the templates a control says.
func controlFields(c Control, templates map[string][]byte) []string {
	var out []string
	for _, t := range append([]string{c.Say.Template}, c.Say.Templates...) {
		var tpl struct {
			Fields map[string]json.RawMessage `json:"fields"`
		}
		if body, ok := templates[t]; ok && json.Unmarshal(body, &tpl) == nil {
			out = append(out, sortedKeys(tpl.Fields)...)
		}
	}
	return out
}

// ForeignControl is an act of this revision that says another package's control.
type ForeignControl struct {
	Where   string // composition "<name>", then the part ids and the act's place
	Intent  string // sign or record
	Control string
	From    PackagePin
	Fields  []string // the act's field names
}

// ForeignControls lists the acts of the revision's compositions that say another package's control.
// The host resolves each among the installed packages and checks it with ForeignControlProblems.
func (r *Revision) ForeignControls() ([]ForeignControl, error) {
	comps, err := r.Compositions()
	if err != nil {
		return nil, err
	}
	var out []ForeignControl
	uses := func(where string, us []Use) {
		for i, u := range us {
			for j, a := range u.Acts {
				if a.From == nil {
					continue
				}
				out = append(out, ForeignControl{Where: fmt.Sprintf("%s/use %d/act %d", where, i+1, j+1),
					Intent: a.Intent, Control: a.Control, From: *a.From, Fields: sortedKeys(a.Fields)})
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
			uses(w, pt.Blocks)
			parts(w, pt.Parts)
		}
	}
	for _, n := range sortedKeys(comps) {
		where := fmt.Sprintf("composition %q", n)
		parts(where, comps[n].Parts)
		uses(where+"/eachPart", comps[n].EachPart)
	}
	return out, nil
}

// ForeignControlProblems checks a foreign control against the owner package as the host installed
// it: owner is its sealed revision and seal the seal Package.Sealed returned with it. The pin must
// name that revision (package, label, the seal's head); the control must be in it, of the kind the
// intent needs (sign: decision, record: record); and every field the act fills must be a field of a
// template the control says. Same rules as for a control of the act's own package.
func ForeignControlProblems(fc ForeignControl, owner *Revision, seal ClaimInfo) []string {
	var out []string
	add := func(format string, args ...any) {
		out = append(out, fmt.Sprintf("%s: ", fc.Where)+fmt.Sprintf(format, args...))
	}
	if owner == nil {
		return []string{fmt.Sprintf("%s: the package %q is not installed", fc.Where, fc.From.Package)}
	}
	if owner.P.Name != fc.From.Package {
		add("from names the package %q, the owner given is %q", fc.From.Package, owner.P.Name)
	}
	if owner.Label != fc.From.At {
		add("from pins %s@%s; the sealed revision is %s", fc.From.Package, fc.From.At, owner.Label)
	}
	if seal.Subject != owner.ID {
		add("the seal given does not seal %s@%s", owner.P.Name, owner.Label)
	} else if head := strings.ToLower(str(seal.Object["hash"])); head != fc.From.Head {
		add("from pins the head %s; the seal names %s", fc.From.Head, head)
	}
	cs, err := owner.Controls()
	if err != nil {
		return append(out, fmt.Sprintf("%s: %v", fc.Where, err))
	}
	c, ok := cs[fc.Control]
	if !ok {
		add("the control %q is not in %s@%s", fc.Control, fc.From.Package, fc.From.At)
		return out
	}
	want := map[string]string{"sign": ControlDecision, "record": ControlRecord}[fc.Intent]
	if want == "" {
		add("intent %q is not sign or record", fc.Intent)
	} else if c.EffectiveKind() != want {
		add("a %s act needs a %s control; %q is a %s", fc.Intent, want, fc.Control, c.EffectiveKind())
	}
	ts, err := owner.Templates()
	if err != nil {
		return append(out, fmt.Sprintf("%s: %v", fc.Where, err))
	}
	fields := controlFields(c, ts)
	for _, k := range fc.Fields {
		if !has(fields, k) {
			add("field %s is not a field of the control's template", k)
		}
	}
	return out
}
