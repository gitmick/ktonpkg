package scope

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// Compose is the first of the three steps compose → query → fill (§6, F6, Naming). It is pure and
// deterministic: from a composition, its subject, its bound inputs, the admitted packages, the
// answers known so far and the reader's state it builds the composed tree: sections in order, and
// per use its block, content, bound query and the key the host asks it by. An every repeats over a
// query's rows; when those rows are not in Results yet, Compose returns them as needs and no tree.
// The host answers the needs (QueryKey) and calls Compose again; the rounds are bounded by the every
// nesting depth.
//
// Each admitted package has passed its own build and install checks (VerticleProblems), so Compose
// checks again only what one package cannot see: names across packages, the inputs, the class, the
// params a parent binds, cycles and depth across packages, and the $VARs of an input's query.

// ComposeRequest is everything Compose reads.
type ComposeRequest struct {
	Package     string               // the root composition's package
	Composition string               // the root composition
	Params      map[string]Term      // the root composition's declared params
	Subject     Subject              // the subject the root composition is about
	Inputs      map[string]*Input    // bound inputs by name (chapters)
	Packages    map[string]*Revision // the admitted packages' sealed revisions, by package name
	Results     map[string]Answer    // answers by QueryKey: for every, and for fill
	State       State                // the reader's state, validated here (ValidateState)
	Identity    string               // identity.name
}

// Subject is what a composition is about. IRI is required; the rest is given when the host knows it.
type Subject struct {
	IRI        string `json:"iri"`
	ID         string `json:"id,omitempty"`    // its content id
	Claim      string `json:"claim,omitempty"` // its registration claim: what sign and record acts are about
	Class      string `json:"class,omitempty"` // from the input manifest when one is bound
	Kind       string `json:"kind,omitempty"`
	Type       string `json:"type,omitempty"`
	Identifier string `json:"identifier,omitempty"` // a report's document id (report-v2 document.id)
	Title      string `json:"title,omitempty"`
}

// Need is a query Compose needs answered before it can expand an every.
type Need struct {
	Key    string          `json:"key"`
	Query  QueryRef        `json:"query"`
	Params map[string]Term `json:"params"`
}

// QueryRef names a query by where it is (a package or an input) and its hash.
type QueryRef struct {
	Package string `json:"package,omitempty"`
	Input   string `json:"input,omitempty"`
	Name    string `json:"name"`
	Hash    string `json:"hash"`
	Text    []byte `json:"-"`
}

// Composed is the composed tree.
type Composed struct {
	Schema       string                   `json:"schema"` // ComposedSchema
	Root         ComposedComposition      `json:"root"`
	Subject      Subject                  `json:"subject"`
	Inputs       map[string]ComposedInput `json:"inputs,omitempty"`
	Compositions []ComposedComposition    `json:"compositions"`
	Sections     []Section                `json:"sections"`
	Uses         []ComposedUse            `json:"uses"`
	Publishes    []Publish                `json:"publishes,omitempty"`
	StateErrors  []StateError             `json:"stateErrors,omitempty"`
}

// ComposedSchema names the composed tree's form.
const ComposedSchema = "kton.dev/composed/v0"

// ComposedInput is what the tree records of a bound input: its manifest hash and class.
type ComposedInput struct {
	Manifest string `json:"manifest"`
	Class    string `json:"class,omitempty"`
}

// ComposedComposition is one composition in the tree: Key is the part path of the part that nests
// it ("" for the root).
type ComposedComposition struct {
	Key     string                     `json:"key"`
	Package string                     `json:"package"`
	Name    string                     `json:"name"`
	Hash    string                     `json:"hash"`
	Kind    string                     `json:"kind"`
	Vars    map[string]json.RawMessage `json:"vars,omitempty"` // the values in use
}

// Section is one part in reading order. Level is its depth, counting unnamed parts.
type Section struct {
	Key         string   `json:"key"`
	ID          string   `json:"id"`
	Heading     string   `json:"heading,omitempty"`
	Level       int      `json:"level"`
	Parent      string   `json:"parent,omitempty"`
	Composition string   `json:"composition"` // key of the composition it belongs to
	Rules       []string `json:"rules,omitempty"`
	Uses        []string `json:"uses,omitempty"`
	Unnamed     bool     `json:"unnamed,omitempty"`
	Input       string   `json:"input,omitempty"`   // the input its parts came from
	Waiting     []string `json:"waiting,omitempty"` // a nested composition whose subject var is unset
}

// ComposedUse is one placed block.
type ComposedUse struct {
	Key      string                     `json:"key"` // <section key>#<use id>
	ID       string                     `json:"id"`
	Section  string                     `json:"section"`
	Origin   string                     `json:"origin"` // part or eachPart
	Block    string                     `json:"block"`  // <package>/<block>
	Viewer   string                     `json:"viewer"`
	Form     string                     `json:"form"` // the viewer's last name segment: text, table, image, ...
	Options  map[string]json.RawMessage `json:"options,omitempty"`
	Empty    string                     `json:"empty"`
	Scope    string                     `json:"scope,omitempty"`
	Caption  string                     `json:"caption,omitempty"`
	Content  *ContentRef                `json:"content,omitempty"`
	Query    *QueryRef                  `json:"query,omitempty"`
	Params   map[string]Term            `json:"params,omitempty"`
	Refine   map[string]Refine          `json:"refine,omitempty"`
	Requires []string                   `json:"requires,omitempty"`
	Acts     []ComposedAct              `json:"acts,omitempty"`
	Emits    map[string]Wire            `json:"emits,omitempty"`
	Up       bool                       `json:"up,omitempty"`
	Waiting  []string                   `json:"waiting,omitempty"` // vars without a value it reads
	Ask      string                     `json:"ask,omitempty"`     // QueryKey, when no query param waits
}

// ContentRef is a content file a use shows, by where it is and its hash.
type ContentRef struct {
	Source string `json:"source"` // package or input name
	Path   string `json:"path"`
	Hash   string `json:"hash"`
	Bytes  []byte `json:"-"`
}

// ComposedAct is an act with its fields bound.
type ComposedAct struct {
	Intent  string            `json:"intent"`
	Label   string            `json:"label"`
	Control string            `json:"control,omitempty"`
	From    *PackagePin       `json:"from,omitempty"`
	Package string            `json:"package,omitempty"` // the control's package when it is the use's own
	Ray     string            `json:"ray,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
	About   string            `json:"about,omitempty"` // subject.claim, for sign and record
}

// Wire sends an event to a var of a composition (by its key).
type Wire struct {
	Composition string `json:"composition"`
	Var         string `json:"var"`
}

// Publish sends a nested composition's var, as its declared event, to a var of its parent.
type Publish struct {
	Event string `json:"event"`
	From  Wire   `json:"from"`
	To    Wire   `json:"to"`
}

// ComposeError is a refusal by name: the composition does not compose, whatever the answers.
type ComposeError struct {
	Code    string
	Message string
}

func (e *ComposeError) Error() string { return e.Code + ": " + e.Message }

func refuse(code, format string, args ...any) error {
	return &ComposeError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Compose builds the composed tree, or says which queries it needs answered first.
func Compose(req ComposeRequest) (*Composed, []Need, error) {
	if req.Subject.IRI == "" {
		return nil, nil, refuse("subject", "the subject needs an iri")
	}
	x := &composer{req: req, pkgs: map[string]*pkgView{}, needKeys: map[string]bool{}, out: &Composed{
		Schema: ComposedSchema, Inputs: map[string]ComposedInput{}}}
	sub := req.Subject
	for _, n := range sortedKeys(req.Inputs) {
		in := req.Inputs[n]
		x.out.Inputs[n] = ComposedInput{Manifest: in.Hash, Class: in.Manifest.Class}
		if in.Manifest.Class != "" {
			if sub.Class != "" && sub.Class != in.Manifest.Class {
				return nil, nil, refuse("class", "the subject says class %s; the input %s is for %s", sub.Class, n, in.Manifest.Class)
			}
			sub.Class = in.Manifest.Class
		}
	}
	x.out.Subject = sub
	root := &subjectRef{iri: &Term{Type: "uri", Value: sub.IRI}, s: sub}
	err := x.composition(req.Package, Ref{Name: req.Composition}, req.Params, frameAt{
		depth: 1, level: 1, subject: root, state: req.State})
	if err != nil {
		return nil, nil, err
	}
	if len(x.needs) > 0 {
		return nil, x.needs, nil
	}
	x.out.Root = x.out.Compositions[0]
	return x.out, nil, nil
}

type composer struct {
	req      ComposeRequest
	pkgs     map[string]*pkgView
	out      *Composed
	needs    []Need
	needKeys map[string]bool
}

// subjectRef is a composition's subject as far as it is known: a subject bound from a var gives the
// iri only (round 5, 5), and an unset var gives nothing yet.
type subjectRef struct {
	iri     *Term // nil: the var is unset
	fromVar string
	s       Subject
}

type frameAt struct {
	parentSection string // the nesting part's key, "" for the root
	depth, level  int
	chain         []string
	subject       *subjectRef
	state         State
}

// frame is one composition being expanded.
type frame struct {
	pkg     *pkgView
	id      string // <package>/<name>
	c       Composition
	key     string
	hash    string
	params  map[string]Term
	chain   []string
	depth   int
	subject *subjectRef
	vars    map[string]json.RawMessage // validated values
}

// --- packages -------------------------------------------------------------------------------------

type pkgView struct {
	name     string
	comps    map[string]Composition
	blocks   map[string]Block
	viewers  map[string]Viewer
	controls map[string]Control
	queries  map[string][]byte
	content  map[string][]byte
	hashes   map[string]string // role/name -> hash
}

func (x *composer) pkg(name string) (*pkgView, error) {
	if v, ok := x.pkgs[name]; ok {
		return v, nil
	}
	r, ok := x.req.Packages[name]
	if !ok {
		return nil, refuse("unknown-ref", "the package %s is not admitted", name)
	}
	v := &pkgView{name: name, hashes: map[string]string{}}
	var err error
	if v.comps, err = r.Compositions(); err != nil {
		return nil, err
	}
	if v.blocks, err = r.Blocks(); err != nil {
		return nil, err
	}
	if v.viewers, err = r.Viewers(); err != nil {
		return nil, err
	}
	if v.controls, err = r.Controls(); err != nil {
		return nil, err
	}
	if v.queries, err = r.Queries(); err != nil {
		return nil, err
	}
	if v.content, err = r.Content(); err != nil {
		return nil, err
	}
	ms, err := r.Members()
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		v.hashes[m.Role+"/"+m.Name] = m.Hash
	}
	x.pkgs[name] = v
	return v, nil
}

// --- compositions ---------------------------------------------------------------------------------

func (x *composer) composition(fromPkg string, ref Ref, bound map[string]Term, at frameAt) error {
	pkgName := fromPkg
	if ref.Package != "" {
		pkgName = ref.Package
	}
	p, err := x.pkg(pkgName)
	if err != nil {
		return err
	}
	c, ok := p.comps[ref.Name]
	if !ok {
		return refuse("unknown-ref", "%s has no composition %s", pkgName, ref.Name)
	}
	id := pkgName + "/" + ref.Name
	for _, prev := range at.chain {
		if prev == id {
			return refuse("cycle", "composition cycle: %s -> %s", strings.Join(at.chain, " -> "), id)
		}
	}
	if at.depth > MaxDepth {
		return refuse("depth", "compositions nest deeper than %d: %s -> %s", MaxDepth, strings.Join(at.chain, " -> "), id)
	}
	for _, k := range c.Inputs {
		if x.req.Inputs[k] == nil {
			return refuse("input", "%s needs the input %s", id, k)
		}
	}
	if c.Class != "" {
		if at.subject.fromVar != "" {
			return refuse("subject-from-var", "%s is for class %s, but its subject comes from var.%s: only its iri is known", id, c.Class, at.subject.fromVar)
		}
		if c.Class != at.subject.s.Class {
			return refuse("class", "%s is for class %s; the subject is %s", id, c.Class, orNone(at.subject.s.Class))
		}
	}
	for _, k := range sortedKeys(bound) {
		if !has(c.Params, k) {
			return refuse("param", "%s declares no param %s", id, k)
		}
	}
	for _, k := range c.Params {
		if _, ok := bound[k]; !ok {
			return refuse("param", "%s: the param %s is not bound", id, k)
		}
	}
	state, errs := ValidateState(c, at.state)
	for _, e := range errs {
		if at.parentSection != "" {
			e.Key = at.parentSection + ":" + e.Key
		}
		x.out.StateErrors = append(x.out.StateErrors, e)
	}
	f := &frame{pkg: p, id: id, c: c, key: at.parentSection, hash: p.hashes[RoleComposition+"/"+ref.Name], params: bound,
		chain: append(append([]string(nil), at.chain...), id), depth: at.depth, subject: at.subject, vars: state.Vars}
	x.out.Compositions = append(x.out.Compositions, ComposedComposition{Key: f.key, Package: pkgName, Name: ref.Name,
		Hash: f.hash, Kind: c.Kind, Vars: state.Vars})

	parts, err := x.fromInputs(f, c.Parts)
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	var walk func(ps []Part) error
	walk = func(ps []Part) error {
		for _, pt := range ps {
			if pt.ID == "" {
				if len(pt.Parts) == 0 {
					return refuse("part", "%s: a part has no id; only a part that groups parts may go without one", id)
				}
				if err := walk(pt.Parts); err != nil {
					return err
				}
				continue
			}
			if ids[pt.ID] {
				return refuse("part", "%s: the part id %s is not unique in its composition", id, pt.ID)
			}
			ids[pt.ID] = true
			if err := walk(pt.Parts); err != nil {
				return err
			}
		}
		return nil
	}
	for _, ip := range parts {
		if err := walk([]Part{ip.Part}); err != nil {
			return err
		}
	}
	for i, pt := range parts {
		if err := x.part(f, pt.input, pt.Part, i, at.level, at.parentSection, nil, state); err != nil {
			return err
		}
	}
	return nil
}

// inputPart is a part and the input it came from ("" for the composition's own).
type inputPart struct {
	Part
	input string
}

// fromInputs replaces {"from": "<input>"} with that input's parts.
func (x *composer) fromInputs(f *frame, ps []Part) ([]inputPart, error) {
	var out []inputPart
	for _, pt := range ps {
		if pt.From == "" {
			out = append(out, inputPart{Part: pt})
			continue
		}
		in := x.req.Inputs[pt.From]
		if !has(f.c.Inputs, pt.From) || in == nil {
			return nil, refuse("input", "%s: parts from %s, which is not a bound input it declares", f.id, pt.From)
		}
		if in.Chapters == nil {
			return nil, refuse("input", "%s: the input %s has no %s", f.id, pt.From, ChaptersFile)
		}
		for _, cp := range in.Chapters.Parts {
			out = append(out, inputPart{Part: cp, input: pt.From})
		}
	}
	return out, nil
}

func orNone(s string) string {
	if s == "" {
		return "of no class"
	}
	return s
}

// scope is where a source is read: the part, and inside an every the row.
type scope struct {
	partID string
	row    map[string]Term
}

func (x *composer) part(f *frame, input string, pt Part, i, level int, parent string, row map[string]Term, state State) error {
	if pt.ID == "" {
		// a part that only groups parts: keyed by its place (PartPath), no section of its own in a report
		pt.ID = fmt.Sprintf("#%d", i+1)
		pt.Unnamed = true
	}
	key := pt.ID
	if parent != "" {
		key = parent + "/" + pt.ID
	}
	if pt.Every != nil {
		rows, err := x.everyRows(f, input, pt.Every, scope{partID: pt.ID, row: row})
		if err != nil || rows == nil {
			return err
		}
		for j, r := range rows {
			rp := pt
			rp.Every = nil
			rp.ID = fmt.Sprintf("%s.%d", pt.ID, j+1)
			if err := x.part(f, input, rp, i, level, parent, r, state); err != nil {
				return err
			}
		}
		return nil
	}
	sec := Section{Key: key, ID: pt.ID, Heading: pt.Heading, Level: level, Parent: parent, Composition: f.key,
		Rules: pt.Rules, Unnamed: pt.Unnamed, Input: input}
	if pt.Unnamed {
		sec.Heading = ""
	}
	x.out.Sections = append(x.out.Sections, sec)
	si := len(x.out.Sections) - 1
	sc := scope{partID: pt.ID, row: row}
	for j, u := range pt.Blocks {
		if err := x.use(f, input, u, si, sc, fmt.Sprint(j+1), "part"); err != nil {
			return err
		}
	}
	if !pt.Unnamed {
		for j, u := range f.c.EachPart {
			if err := x.use(f, "", u, si, sc, fmt.Sprintf("each%d", j+1), "eachPart"); err != nil {
				return err
			}
		}
	}
	if len(pt.Composition) > 0 {
		if err := x.nested(f, pt, key, level, sc, state); err != nil {
			return err
		}
	}
	for j, cp := range pt.Parts {
		if err := x.part(f, input, cp, j, level+1, key, row, state); err != nil {
			return err
		}
	}
	return nil
}

func (x *composer) nested(f *frame, pt Part, key string, level int, sc scope, state State) error {
	ref, err := parseRef(pt.Composition, "composition")
	if err != nil {
		return refuse("reference", "%s/%s: %v", f.id, pt.ID, err)
	}
	if ref.Input != "" {
		return refuse("reference", "%s/%s: a composition comes from a package, not an input", f.id, pt.ID)
	}
	bound := map[string]Term{}
	sub := f.subject
	for _, k := range sortedKeys(pt.Params) {
		src := pt.Params[k]
		if k == "subject" {
			name := strings.TrimPrefix(src, "var.")
			if name == src {
				t, _, err := x.source(f, src, sc)
				if err != nil {
					return err
				}
				sub = &subjectRef{iri: &t, s: Subject{IRI: t.Value}}
				continue
			}
			v, ok := f.vars[name]
			if !ok {
				// W9: the nested composition waits as a whole until the var has a value.
				s := &x.out.Sections[len(x.out.Sections)-1]
				s.Waiting = append(s.Waiting, "var."+name)
				return nil
			}
			t, err := varTerm(f.c.Vars[name], v)
			if err != nil {
				return refuse("var", "%s/%s: params.subject: %v", f.id, pt.ID, err)
			}
			sub = &subjectRef{iri: &t, fromVar: name, s: Subject{IRI: t.Value}}
			continue
		}
		t, waiting, err := x.source(f, src, sc)
		if err != nil {
			return err
		}
		if waiting != "" {
			s := &x.out.Sections[len(x.out.Sections)-1]
			s.Waiting = append(s.Waiting, waiting)
			return nil
		}
		bound[k] = t
	}
	before := len(x.out.Compositions)
	err = x.composition(f.pkg.name, ref, bound, frameAt{parentSection: key, depth: f.depth + 1,
		level: level + 1, chain: f.chain, subject: sub, state: state.Nested[key]})
	if err != nil {
		return err
	}
	child := x.out.Compositions[before]
	cp, _ := x.pkg(child.Package)
	cdecl := cp.comps[child.Name]
	for _, ev := range sortedKeys(pt.Emits) {
		cv, ok := cdecl.Emits[ev]
		if !ok {
			return refuse("emits", "%s/%s: %s/%s declares no event %s", f.id, pt.ID, child.Package, child.Name, ev)
		}
		pv := pt.Emits[ev]
		pd, ok := f.c.Vars[pv]
		if !ok {
			return refuse("emits", "%s/%s: emits %s to var %s, which %s does not declare", f.id, pt.ID, ev, pv, f.id)
		}
		if f.c.Kind == "report" || f.c.Kind == "deck" {
			continue // inert
		}
		ct, err1 := parseType(cdecl.Vars[cv].Type)
		pty, err2 := parseType(pd.Type)
		if err1 != nil || err2 != nil || ct != pty {
			return refuse("var-type", "%s/%s: the event %s carries %s; the var %s is %s", f.id, pt.ID, ev, ct, pv, pty)
		}
		x.out.Publishes = append(x.out.Publishes, Publish{Event: ev, From: Wire{Composition: child.Key, Var: cv},
			To: Wire{Composition: f.key, Var: pv}})
	}
	return nil
}

// everyRows are the rows an every repeats over, or nil after recording its query as a need.
func (x *composer) everyRows(f *frame, input string, e *Every, sc scope) ([]map[string]Term, error) {
	q, err := x.query(f, input, e.Query)
	if err != nil {
		return nil, err
	}
	params := map[string]Term{}
	for _, k := range sortedKeys(e.Params) {
		t, waiting, err := x.source(f, e.Params[k], sc)
		if err != nil {
			return nil, err
		}
		if waiting != "" {
			return nil, refuse("var", "%s: an every's query cannot read a var (%s): its parts are fixed by compose", f.id, waiting)
		}
		params[k] = t
	}
	if err := checkVars(f, q, params, nil); err != nil {
		return nil, err
	}
	key, err := QueryKey(q.Hash, params)
	if err != nil {
		return nil, refuse("param", "%s: %v", f.id, err)
	}
	ans, ok := x.req.Results[key]
	if !ok {
		if !x.needKeys[key] {
			x.needKeys[key] = true
			x.needs = append(x.needs, Need{Key: key, Query: *q, Params: params})
		}
		return nil, nil
	}
	return ans.Results.Bindings, nil
}

// query resolves a query reference: a name in the composition's package (or, for a part from an
// input, in that input), {package, query} or {input, query}.
func (x *composer) query(f *frame, input string, raw json.RawMessage) (*QueryRef, error) {
	r, err := parseRef(raw, "query")
	if err != nil {
		return nil, refuse("reference", "%s: %v", f.id, err)
	}
	if r.Local() && input != "" {
		r.Input = input
	}
	switch {
	case r.Input != "":
		in := x.req.Inputs[r.Input]
		if in == nil || !has(f.c.Inputs, r.Input) {
			return nil, refuse("input", "%s: the query %s is in the input %s, which is not a bound input it declares", f.id, r.Name, r.Input)
		}
		p := "queries/" + r.Name + ".rq"
		b, ok := in.Files[p]
		if !ok {
			return nil, refuse("unknown-ref", "the input %s has no query %s", r.Input, r.Name)
		}
		return &QueryRef{Input: r.Input, Name: r.Name, Hash: in.Manifest.Files[p], Text: b}, nil
	case r.Package != "":
		p, err := x.pkg(r.Package)
		if err != nil {
			return nil, err
		}
		b, ok := p.queries[r.Name]
		if !ok {
			return nil, refuse("unknown-ref", "%s has no query %s", r.Package, r.Name)
		}
		return &QueryRef{Package: r.Package, Name: r.Name, Hash: p.hashes[RoleQuery+"/"+r.Name], Text: b}, nil
	}
	b, ok := f.pkg.queries[r.Name]
	if !ok {
		return nil, refuse("unknown-ref", "%s has no query %s", f.pkg.name, r.Name)
	}
	return &QueryRef{Package: f.pkg.name, Name: r.Name, Hash: f.pkg.hashes[RoleQuery+"/"+r.Name], Text: b}, nil
}

// checkVars: every $VAR the query uses is bound, refined or optional, and every param is a $VAR of
// it. The build checks this for a package's own queries; compose checks it again because a query of
// another package or of an input is first seen here.
func checkVars(f *frame, q *QueryRef, params map[string]Term, optional []string) error {
	vars := queryVars(q.Text)
	for _, v := range vars {
		if _, ok := params[v]; !ok && !has(optional, v) {
			return refuse("unbound-var", "%s: the query %s uses $%s, which is not bound", f.id, q.Name, v)
		}
	}
	for _, k := range sortedKeys(params) {
		if !has(vars, k) {
			return refuse("param", "%s: the query %s has no $%s", f.id, q.Name, k)
		}
	}
	return nil
}

// --- uses -----------------------------------------------------------------------------------------

func (x *composer) use(f *frame, input string, u Use, si int, sc scope, idx, origin string) error {
	sec := &x.out.Sections[si]
	id := u.ID
	if id == "" {
		id = idx
	}
	if u.Every != nil {
		rows, err := x.everyRows(f, input, u.Every, sc)
		if err != nil || rows == nil {
			return err
		}
		for j, r := range rows {
			ru := u
			ru.Every = nil
			ru.ID = fmt.Sprintf("%s.%d", id, j+1)
			if err := x.use(f, input, ru, si, scope{partID: sc.partID, row: r}, idx, origin); err != nil {
				return err
			}
		}
		return nil
	}
	bref, err := parseRef(u.Use, "block")
	if err != nil {
		return refuse("reference", "%s: %v", sec.Key, err)
	}
	bp := f.pkg
	if bref.Package != "" {
		if bp, err = x.pkg(bref.Package); err != nil {
			return err
		}
	}
	b, ok := bp.blocks[bref.Name]
	if !ok {
		return refuse("unknown-ref", "%s has no block %s", bp.name, bref.Name)
	}
	viewer := blockViewer(b, bp.viewers)
	key := sec.Key + "#" + id
	for _, k := range sec.Uses {
		if k == key {
			return refuse("duplicate-use", "the use id %s appears twice in %s", id, sec.Key)
		}
	}
	cu := ComposedUse{Key: key, ID: id, Section: sec.Key, Origin: origin, Block: bp.name + "/" + bref.Name,
		Viewer: viewer, Form: path.Base(viewer), Empty: u.Empty, Caption: u.Caption, Up: u.Up}
	if cu.Empty == "" {
		cu.Empty = "show"
		if f.c.Kind == "report" {
			cu.Empty = "omit"
		}
	}
	requires := map[string]bool{}
	for _, r := range b.Requires {
		requires[r] = true
	}
	waiting := map[string]bool{}      // every var without a value the use reads
	paramWaiting := map[string]bool{} // those its query needs: no ask until they have one

	// options: a literal value, or var.<name> (its value now; unset: the option waits, the query not)
	for _, k := range sortedKeys(u.Options) {
		raw := u.Options[k]
		var s string
		if json.Unmarshal(raw, &s) == nil && strings.HasPrefix(s, "var.") {
			v, ok := f.vars[strings.TrimPrefix(s, "var.")]
			if !ok {
				waiting[s] = true
				continue
			}
			raw = v
		}
		if cu.Options == nil {
			cu.Options = map[string]json.RawMessage{}
		}
		cu.Options[k] = raw
	}
	if len(u.Scope) > 0 {
		var s string
		var from struct{ From, Slot string }
		switch {
		case json.Unmarshal(u.Scope, &s) == nil:
			cu.Scope = s
		case json.Unmarshal(u.Scope, &from) == nil && from.From != "":
			in := x.req.Inputs[from.From]
			if in == nil || in.Chapters == nil || in.Chapters.Scopes[from.Slot] == "" {
				return refuse("unknown-ref", "%s: no scope %s in the input %s", key, from.Slot, from.From)
			}
			cu.Scope = in.Chapters.Scopes[from.Slot]
		default:
			return refuse("reference", "%s: scope is a scope label or {from, slot}", key)
		}
	}
	if u.File != "" {
		c, err := x.content(f, input, u.File)
		if err != nil {
			return refuse("unknown-ref", "%s: %v", key, err)
		}
		cu.Content = c
		if !isText(u.File) {
			requires["content@1"] = true
		}
	}
	if len(u.Query) > 0 {
		q, err := x.query(f, input, u.Query)
		if err != nil {
			return err
		}
		params := map[string]Term{}
		for _, k := range sortedKeys(u.Params) {
			t, w, err := x.source(f, u.Params[k], sc)
			if err != nil {
				return refuse("param", "%s: param %s: %v", key, k, err)
			}
			if w != "" {
				waiting[w] = true
				if !has(u.Optional, k) {
					paramWaiting[w] = true
				}
				continue
			}
			params[k] = t
		}
		for _, k := range sortedKeys(u.Refine) {
			t, err := refineDefault(u.Refine[k])
			if err != nil {
				return refuse("refine", "%s: refine %s: %v", key, k, err)
			}
			params[k] = t
		}
		optional := append(append([]string(nil), u.Optional...), keysOfWaiting(u.Params, waiting)...)
		if err := checkVars(f, q, params, optional); err != nil {
			return err
		}
		cu.Query, cu.Params, cu.Refine = q, params, u.Refine
		requires["query@1"] = true
	}
	for _, a := range u.Acts {
		ca, err := x.act(f, a, sc, key)
		if err != nil {
			return err
		}
		cu.Acts = append(cu.Acts, ca)
	}
	if f.c.Kind != "report" && f.c.Kind != "deck" {
		for _, ev := range sortedKeys(u.Emits) {
			if cu.Emits == nil {
				cu.Emits = map[string]Wire{}
			}
			cu.Emits[ev] = Wire{Composition: f.key, Var: u.Emits[ev]}
		}
	}
	cu.Requires = sortedKeys(requires)
	cu.Waiting = sortedKeys(waiting)
	if cu.Query != nil && len(paramWaiting) == 0 {
		if cu.Ask, err = QueryKey(cu.Query.Hash, cu.Params); err != nil {
			return refuse("param", "%s: %v", key, err)
		}
	}
	x.out.Uses = append(x.out.Uses, cu)
	sec.Uses = append(sec.Uses, key)
	return nil
}

// keysOfWaiting are the params whose source waits: their $VARs are not bound yet, which is not a
// refusal (the use waits, W9).
func keysOfWaiting(params map[string]string, waiting map[string]bool) []string {
	var out []string
	for k, src := range params {
		if waiting[src] {
			out = append(out, k)
		}
	}
	return out
}

func blockViewer(b Block, viewers map[string]Viewer) string {
	var s string
	if json.Unmarshal(b.Viewer, &s) == nil {
		if v, ok := viewers[s]; ok {
			return v.Viewer
		}
		return s
	}
	var v Viewer
	if json.Unmarshal(b.Viewer, &v) == nil {
		return v.Viewer
	}
	return ""
}

func isText(p string) bool {
	switch path.Ext(p) {
	case ".md", ".json", ".mmd", ".txt", ".csv", ".R", ".rq":
		return true
	}
	return false
}

func (x *composer) content(f *frame, input, file string) (*ContentRef, error) {
	if input != "" {
		in := x.req.Inputs[input]
		b, ok := in.Files[file]
		if !ok {
			return nil, fmt.Errorf("the input %s has no content file %s", input, file)
		}
		return &ContentRef{Source: input, Path: file, Hash: in.Manifest.Files[file], Bytes: b}, nil
	}
	b, ok := f.pkg.content[file]
	if !ok {
		return nil, fmt.Errorf("%s has no content file %s", f.pkg.name, file)
	}
	return &ContentRef{Source: f.pkg.name, Path: file, Hash: f.pkg.hashes[RoleContent+"/"+file], Bytes: b}, nil
}

func refineDefault(r Refine) (Term, error) {
	var def json.RawMessage = r.Default
	if len(r.Values) > 0 && len(def) == 0 {
		def = r.Values[0]
	}
	var s string
	if err := json.Unmarshal(def, &s); err != nil {
		var n json.Number
		if json.Unmarshal(def, &n) == nil {
			return Term{Type: "literal", Value: n.String(), Datatype: xsdInteger}, nil
		}
		return Term{}, fmt.Errorf("the default %s is not a value", string(def))
	}
	switch r.Type {
	case "iri":
		return Term{Type: "uri", Value: s}, nil
	case "integer":
		return Term{Type: "literal", Value: s, Datatype: xsdInteger}, nil
	}
	return Term{Type: "literal", Value: s}, nil
}

func (x *composer) act(f *frame, a Act, sc scope, key string) (ComposedAct, error) {
	ca := ComposedAct{Intent: a.Intent, Label: a.Label, Control: a.Control, From: a.From, Ray: a.Ray}
	if ca.Label == "" {
		ca.Label = a.Intent
	}
	if a.Control != "" && a.From == nil {
		ca.Package = f.pkg.name
	}
	for _, k := range sortedKeys(a.Fields) {
		t, w, err := x.source(f, a.Fields[k], sc)
		if err != nil {
			return ca, refuse("param", "%s: act field %s: %v", key, k, err)
		}
		if w != "" {
			continue // the host fills it when the var has a value
		}
		if ca.Fields == nil {
			ca.Fields = map[string]string{}
		}
		ca.Fields[k] = t.Value
	}
	if a.Intent == "sign" || a.Intent == "record" {
		if f.subject.fromVar != "" {
			return ca, refuse("subject-from-var", "%s: a %s act is about subject.claim, but the subject comes from var.%s", key, a.Intent, f.subject.fromVar)
		}
		if f.subject.s.Claim == "" {
			return ca, refuse("unbound-var", "%s: a %s act is about subject.claim, which this subject lacks", key, a.Intent)
		}
		ca.About = f.subject.s.Claim
	}
	return ca, nil
}

// --- sources --------------------------------------------------------------------------------------

// source binds a param source to a term. A var without a value, or a subject that waits for one,
// gives waiting = "var.<name>" instead of a term.
func (x *composer) source(f *frame, src string, sc scope) (Term, string, error) {
	lit := func(v string) Term { return Term{Type: "literal", Value: v} }
	s := f.subject
	subjectIRI := func() (Term, string, error) {
		if s.iri == nil {
			return Term{}, "var." + s.fromVar, nil
		}
		return *s.iri, "", nil
	}
	known := func(name, v string) (Term, string, error) {
		if s.fromVar != "" {
			return Term{}, "", fmt.Errorf("%s reads %s, but its subject comes from var.%s: only its iri is known", f.id, name, s.fromVar)
		}
		if v == "" {
			return Term{}, "", fmt.Errorf("%s is not known for this subject", name)
		}
		return lit(v), "", nil
	}
	switch src {
	case "subject.iri":
		return subjectIRI()
	case "subject.id":
		return known(src, s.s.ID)
	case "subject.claim":
		return known(src, s.s.Claim)
	case "subject.class":
		return known(src, s.s.Class)
	case "subject.kind":
		return known(src, s.s.Kind)
	case "subject.type":
		return known(src, s.s.Type)
	case "part.id":
		if sc.partID == "" {
			return Term{}, "", fmt.Errorf("part.id outside a part")
		}
		return lit(sc.partID), "", nil
	case "part.iri":
		if sc.partID == "" {
			return Term{}, "", fmt.Errorf("part.iri outside a part")
		}
		t, w, err := subjectIRI()
		if w != "" || err != nil {
			return t, w, err
		}
		return Term{Type: "uri", Value: t.Value + "/" + sc.partID}, "", nil
	case "composition.id":
		return lit(f.hash), "", nil
	case "identity.name":
		if x.req.Identity == "" {
			return Term{}, "", fmt.Errorf("identity.name is not known (no identity given)")
		}
		return lit(x.req.Identity), "", nil
	}
	if name, ok := strings.CutPrefix(src, "var."); ok {
		d, ok := f.c.Vars[name]
		if !ok {
			return Term{}, "", fmt.Errorf("%s declares no var %s", f.id, name)
		}
		v, ok := f.vars[name]
		if !ok {
			return Term{}, src, nil
		}
		t, err := varTerm(d, v)
		return t, "", err
	}
	if name, ok := strings.CutPrefix(src, "row."); ok {
		if sc.row == nil {
			return Term{}, "", fmt.Errorf("%s outside an every", src)
		}
		t, ok := sc.row[name]
		if !ok {
			return Term{}, "", fmt.Errorf("%s: the row has no %s", src, name)
		}
		return t, "", nil
	}
	if has(f.c.Params, src) {
		return f.params[src], "", nil
	}
	return Term{}, "", fmt.Errorf("%s is not a source (the closed set, var.<name>, row.<var> or a param of %s)", src, f.id)
}

// varTerm is a var's value as a term, by its declared type; a hash becomes its content IRI.
func varTerm(d VarDecl, v json.RawMessage) (Term, error) {
	t, err := parseType(d.Type)
	if err != nil {
		return Term{}, err
	}
	if t.Set {
		return Term{}, fmt.Errorf("a set does not bind into a query")
	}
	if t.Of == "integer" {
		var n json.Number
		if err := json.Unmarshal(v, &n); err != nil {
			return Term{}, err
		}
		return Term{Type: "literal", Value: n.String(), Datatype: xsdInteger}, nil
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return Term{}, err
	}
	switch t.Of {
	case "iri":
		return Term{Type: "uri", Value: s}, nil
	case "hash":
		return Term{Type: "uri", Value: contentIRI(s)}, nil
	}
	return Term{Type: "literal", Value: s}, nil
}
