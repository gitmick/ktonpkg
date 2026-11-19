package scope

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/gitmick/ktonpkg"
)

// The ui part of the verticle profile (ktonpkg v0.9.0). Contracts:
//   - C2.1 slot entries, ui/<name>.json:      verticleformat/2026-11-18-ui-declaration.md
//   - blocks and compositions, ui/blocks/ and ui/compositions/, with vars and emits:
//     verticleformat/2026-11-19-composition-format.md, Rounds 2-6 and Naming
//
// Everything here is a declaration, checked at build: every name it uses resolves inside the
// package, every value it binds comes from a closed set, and types are equal. What a host does with
// it (render, ask, sign) is the host's. Names in other packages ({"package": …}) are checked for
// form only; compose resolves them among the admitted packages (v0.10.0).

// Roles of the ui part.
const (
	RoleUI          = "ui"          // ui/<name>.json: a C2 slot entry
	RoleBlock       = "block"       // ui/blocks/<name>.json
	RoleComposition = "composition" // ui/compositions/<name>.json
	RoleContent     = "content"     // content/<path>: text, tables, images a composition shows
	RoleViewerFile  = "viewer-file" // viewers/<viewer>/<path>: a viewer tree the package carries
)

// Control kinds (O1, MH 18.11.): a decision is signed with the person key through the sign dialog; a
// record is written with the agent key, with no dialog. A control that says no kind is a decision:
// the safe side. The intent that runs a control cannot change its kind.
const (
	ControlDecision = "decision"
	ControlRecord   = "record"
)

// Meanings are the one vocabulary of what a control's say means (C3.4, qms-evolution). rejected is
// an outcome of reviewed or approved, not a meaning; verified waits for CAPA.
var Meanings = []string{"created", "reviewed", "approved", "executed", "released", "trained-on", "delivered"}

// Kind is the control's kind, decision when it says none.
func (c Control) EffectiveKind() string {
	if c.Kind == "" {
		return ControlDecision
	}
	return c.Kind
}

// Slots are the v1 slots of C2.1.
var Slots = []string{"activity", "home-tile", "status-item", "review-view", "record-view", "run-action",
	"finding-source", "material-editor"}

// ShellValues are what a slot entry's inputs may bind from (C2.2, plus subject.iri and subject.claim
// from the composition rounds F4, F5). Nothing that carries a key or a trust decision.
var ShellValues = []string{"subject.id", "subject.iri", "subject.claim", "subject.kind", "subject.type",
	"subject.path", "subject.scope", "identity.name"}

// compositionValues are what a composition's params may bind from, besides var.<name>, row.<var>
// inside an every, and the composition's own declared params by name.
var compositionValues = append(append([]string(nil), ShellValues...),
	"subject.class", "part.id", "part.iri", "composition.id")

// Capabilities a block may name. The acts never hide a block, so they may only be optional.
var (
	DataCapabilities = []string{"query@1", "product@1", "graph@1", "content@1", "open@1"}
	ActCapabilities  = []string{"run@1", "record@1", "sign@1", "communication@1"}
)

// actIntents sign or write. They are never wirable (round 5b (c)).
var actIntents = []string{"sign", "record", "run", "post"}

// reserved names a var or param may not take.
var reserved = []string{"subject", "part", "row", "identity", "composition", "var"}

// CompositionKinds; the kind changes the presenter of parts, not the declaration.
var CompositionKinds = []string{"view", "report", "deck", "page"}

// MaxDepth is how deep compositions may nest.
const MaxDepth = 8

// When says which subjects an entry applies to.
type When struct {
	Kind string `json:"kind"`
	Type string `json:"type,omitempty"`
}

// UIEntry is one C2 slot entry.
type UIEntry struct {
	Slot        string            `json:"slot"`
	Label       string            `json:"label"`
	When        *When             `json:"when,omitempty"`
	Query       string            `json:"query,omitempty"`
	Ray         string            `json:"ray,omitempty"`
	Viewer      string            `json:"viewer,omitempty"`
	Control     string            `json:"control,omitempty"`
	Composition string            `json:"composition,omitempty"`
	Inputs      map[string]string `json:"inputs,omitempty"`
	Order       int               `json:"order,omitempty"`
	Area        *Area             `json:"area,omitempty"`
}

// Area is C3's addition to an activity entry.
//
// As agreed in C3.2: the scopes it reads, its queries (record, raci, training) with their params
// bound from the shell's values, its actions (controls), its documents and governing SOP (document
// keys the shell resolves), and the rules of another package it shows findings of.
type Area struct {
	Scopes    *AreaScopes       `json:"scopes,omitempty"`
	Record    string            `json:"record,omitempty"`
	Raci      string            `json:"raci,omitempty"`
	Training  string            `json:"training,omitempty"`
	Approvals string            `json:"approvals,omitempty"` // ?claim ?record ?outcome ?signer of a record
	History   string            `json:"history,omitempty"`   // ?claim ?replaces ?replacedBy ?when: the record's chain
	Params    map[string]string `json:"params,omitempty"`    // $VAR of the area's queries -> shell value
	Actions   []string          `json:"actions,omitempty"`
	Documents []string          `json:"documents,omitempty"`
	Governing string            `json:"governing,omitempty"`
	Findings  *Findings         `json:"findings,omitempty"`
}

// AreaScopes are the scope names an area writes records to and reads from; the shell and backend
// resolve them.
type AreaScopes struct {
	Records string   `json:"records"`
	Reads   []string `json:"reads,omitempty"`
}

// Findings names another package by name, and rule ids (the ?rule of the one finding shape, as in a
// part's rules), not query names: the shell resolves the package among admitted packages and keeps
// the findings whose ?rule is listed.
type Findings struct {
	Package string   `json:"package"`
	Rules   []string `json:"rules"`
}

// Block declares a presenter: its viewer, what it needs from the host, what subjects it accepts,
// its typed options and the events it emits with their payload types.
type Block struct {
	Block    string                     `json:"block,omitempty"` // if given, the file's name
	Viewer   json.RawMessage            `json:"viewer"`          // a viewers/ name, or an inline {viewer, at, tree}
	Requires []string                   `json:"requires,omitempty"`
	Optional []string                   `json:"optional,omitempty"`
	Accepts  []When                     `json:"accepts,omitempty"`
	Options  map[string]json.RawMessage `json:"options,omitempty"` // option -> type
	Emits    map[string]json.RawMessage `json:"emits,omitempty"`   // event -> payload type
}

// Composition is a block made of blocks.
type Composition struct {
	Composition string             `json:"composition,omitempty"` // if given, the file's name
	Kind        string             `json:"kind"`
	Class       string             `json:"class,omitempty"`
	Subject     *When              `json:"subject,omitempty"`
	Inputs      []string           `json:"inputs,omitempty"`
	Params      []string           `json:"params,omitempty"`
	Vars        map[string]VarDecl `json:"vars,omitempty"`
	Parts       []Part             `json:"parts"`
	EachPart    []Use              `json:"eachPart,omitempty"`
	Emits       map[string]string  `json:"emits,omitempty"` // event -> own var
	Render      *RenderRef         `json:"render,omitempty"`
}

// VarDecl is a composition variable.
type VarDecl struct {
	Type    json.RawMessage `json:"type"`
	Default json.RawMessage `json:"default,omitempty"`
}

// RenderRef names the ray a report or deck renders through.
type RenderRef struct {
	Package string `json:"package,omitempty"`
	Ray     string `json:"ray"`
}

// Part is a section: blocks and sub-parts, or a nested composition, or a placeholder for an input.
type Part struct {
	ID          string            `json:"id,omitempty"`
	Heading     string            `json:"heading,omitempty"`
	From        string            `json:"from,omitempty"`
	Rules       []string          `json:"rules,omitempty"`
	Blocks      []Use             `json:"blocks,omitempty"`
	Parts       []Part            `json:"parts,omitempty"`
	Composition json.RawMessage   `json:"composition,omitempty"`
	Params      map[string]string `json:"params,omitempty"`
	Emits       map[string]string `json:"emits,omitempty"`
	Every       *Every            `json:"every,omitempty"`
}

// Use places a block.
type Use struct {
	ID      string                     `json:"id,omitempty"`
	Use     json.RawMessage            `json:"use"`
	Query   json.RawMessage            `json:"query,omitempty"`
	Params  map[string]string          `json:"params,omitempty"`
	Options map[string]json.RawMessage `json:"options,omitempty"`
	Refine  map[string]Refine          `json:"refine,omitempty"`
	Emits   map[string]string          `json:"emits,omitempty"`
	Up      bool                       `json:"up,omitempty"`
	Empty   string                     `json:"empty,omitempty"`
	File    string                     `json:"file,omitempty"`
	Caption string                     `json:"caption,omitempty"`
	Scope   json.RawMessage            `json:"scope,omitempty"`
	Every   *Every                     `json:"every,omitempty"`
	Acts    []Act                      `json:"acts,omitempty"`
}

// Every repeats a use or a part over a query's rows.
type Every struct {
	Query  json.RawMessage   `json:"query"`
	As     string            `json:"as,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// Refine lets the reader vary one query param: a list of values, or a type with a default.
type Refine struct {
	Values  []json.RawMessage `json:"values,omitempty"`
	Type    string            `json:"type,omitempty"`
	Default json.RawMessage   `json:"default,omitempty"`
}

// Act is an act a use offers: sign or record through a control, run through the ray.
type Act struct {
	Intent  string            `json:"intent"`
	Label   string            `json:"label,omitempty"`
	Control string            `json:"control,omitempty"`
	Ray     string            `json:"ray,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// Ref is a name, here or in another package, or in an input of the composition.
type Ref struct {
	Package string
	Input   string
	Name    string
}

// Local says the name is this package's.
func (r Ref) Local() bool { return r.Package == "" && r.Input == "" }

func parseRef(raw json.RawMessage, field string) (Ref, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "" {
			return Ref{}, fmt.Errorf("an empty %s", field)
		}
		return Ref{Name: s}, nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return Ref{}, fmt.Errorf("%s is neither a name nor {package|input, %s}", field, field)
	}
	r := Ref{Package: m["package"], Input: m["input"], Name: m[field]}
	for k := range m {
		if k != "package" && k != "input" && k != field {
			return Ref{}, fmt.Errorf("%s: unknown field %q", field, k)
		}
	}
	if r.Name == "" || (r.Package == "") == (r.Input == "") {
		return Ref{}, fmt.Errorf("%s must be a name, or {package, %s}, or {input, %s}", field, field, field)
	}
	return r, nil
}

// VarType is iri, literal, integer, hash, or a set of one of them.
type VarType struct {
	Set bool
	Of  string
}

func (t VarType) String() string {
	if t.Set {
		return "set of " + t.Of
	}
	return t.Of
}

var scalarTypes = []string{"iri", "literal", "integer", "hash"}

func parseType(raw json.RawMessage) (VarType, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if !has(scalarTypes, s) {
			return VarType{}, fmt.Errorf("type %q is not one of %s", s, strings.Join(scalarTypes, ", "))
		}
		return VarType{Of: s}, nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || len(m) != 1 || !has(scalarTypes, m["set"]) {
		return VarType{}, fmt.Errorf("type %s is neither %s nor {\"set\": one of them}", string(raw), strings.Join(scalarTypes, "|"))
	}
	return VarType{Set: true, Of: m["set"]}, nil
}

// assignable says a value of type from may go where to is wanted: equal, or the one conversion
// hash -> iri (round 5b (a)).
func assignable(from, to VarType) bool {
	return from == to || (!from.Set && !to.Set && from.Of == "hash" && to.Of == "iri")
}

var hashForm = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// conforms says a JSON value is of type t.
func conforms(raw json.RawMessage, t VarType) bool {
	if t.Set {
		var xs []json.RawMessage
		if json.Unmarshal(raw, &xs) != nil {
			return false
		}
		for _, x := range xs {
			if !conforms(x, VarType{Of: t.Of}) {
				return false
			}
		}
		return true
	}
	switch t.Of {
	case "integer":
		var n json.Number
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.UseNumber()
		if d.Decode(&n) != nil {
			return false
		}
		_, err := n.Int64()
		return err == nil
	default:
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return false
		}
		switch t.Of {
		case "hash":
			return hashForm.MatchString(s)
		case "iri":
			return s != "" && strings.Contains(s, ":")
		}
		return true
	}
}

func has(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// AddUI, AddBlock, AddComposition and AddContent lay ui declarations down; the bytes are decoded
// strictly (no unknown fields), the references are checked by VerticleProblems.
func (r *Revision) AddUI(name string, body []byte) (string, error) {
	if _, err := decode[UIEntry](body); err != nil {
		return "", fmt.Errorf("ui %q: %w", name, err)
	}
	return r.addNamed(RoleUI, name, body)
}

func (r *Revision) AddBlock(name string, body []byte) (string, error) {
	if _, err := decode[Block](body); err != nil {
		return "", fmt.Errorf("block %q: %w", name, err)
	}
	return r.addNamed(RoleBlock, name, body)
}

func (r *Revision) AddComposition(name string, body []byte) (string, error) {
	if _, err := decodeComposition(body); err != nil {
		return "", fmt.Errorf("composition %q: %w", name, err)
	}
	return r.addNamed(RoleComposition, name, body)
}

// AddContent adds a file a composition shows, by its path in the package ("content/…").
func (r *Revision) AddContent(path string, body []byte) (string, error) {
	if !strings.HasPrefix(path, "content/") || strings.Contains(path, "..") {
		return "", fmt.Errorf("content %q: a content path is content/<path>", path)
	}
	return r.addNamed(RoleContent, path, body)
}

// AddViewerFile adds one file of a viewer tree the package carries (viewers/<viewer>/<path>). A
// carried viewer is pinned like a fetched one, {viewer, at, tree}, with at = <package>@<revision
// label>; the build checks the tree id and reads viewer.json for the block checks.
func (r *Revision) AddViewerFile(viewer, rel string, body []byte) (string, error) {
	if viewer == "" || strings.ContainsAny(viewer, "/\\") || rel == "" || strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("viewer file %q/%q: a viewer file is viewers/<viewer>/<path> inside the package", viewer, rel)
	}
	return r.addNamed(RoleViewerFile, "viewers/"+viewer+"/"+rel, body)
}

// ViewerTrees are the viewer trees the package carries: viewer -> path in the tree -> bytes.
func (r *Revision) ViewerTrees() (map[string]map[string][]byte, error) {
	files, err := r.named(RoleViewerFile)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string][]byte{}
	for name, b := range files {
		parts := strings.SplitN(strings.TrimPrefix(name, "viewers/"), "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("viewer file %q is not viewers/<viewer>/<path>", name)
		}
		if out[parts[0]] == nil {
			out[parts[0]] = map[string][]byte{}
		}
		out[parts[0]][parts[1]] = b
	}
	return out, nil
}

// decodeComposition decodes strictly, nested parts and uses included (the decoder's strictness
// reaches into nested structs).
func decodeComposition(body []byte) (Composition, error) { return decode[Composition](body) }

// UIEntries, Blocks, Compositions and Content are the revision's ui declarations by name.
func (r *Revision) UIEntries() (map[string]UIEntry, error) { return decodeAll[UIEntry](r, RoleUI) }
func (r *Revision) Blocks() (map[string]Block, error)      { return decodeAll[Block](r, RoleBlock) }
func (r *Revision) Compositions() (map[string]Composition, error) {
	return decodeAll[Composition](r, RoleComposition)
}
func (r *Revision) Content() (map[string][]byte, error) { return r.named(RoleContent) }

// uiPackage is what the ui checks look up in the revision.
type uiPackage struct {
	queries      map[string][]byte
	templates    map[string][]byte
	controls     map[string]Control
	viewers      map[string]Viewer
	blocks       map[string]Block
	compositions map[string]Composition
	content      map[string][]byte
	ray          *ktonpkg.Ray
	prefixes     map[string]string
}

// uiProblems checks the ui part. Called by VerticleProblems.
func (r *Revision) uiProblems(qs, ts map[string][]byte, cs map[string]Control, vs map[string]Viewer, ray *ktonpkg.Ray) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	ui, err := r.UIEntries()
	if err != nil {
		return []string{err.Error()}
	}
	bs, err := r.Blocks()
	if err != nil {
		return []string{err.Error()}
	}
	comps, err := r.Compositions()
	if err != nil {
		return []string{err.Error()}
	}
	content, err := r.Content()
	if err != nil {
		return []string{err.Error()}
	}
	p := &uiPackage{queries: qs, templates: ts, controls: cs, viewers: vs, blocks: bs, compositions: comps,
		content: content, ray: ray, prefixes: map[string]string{}}
	if a, err := r.Aliases(); err == nil && a != nil {
		var al struct {
			Prefixes map[string]string `json:"prefixes"`
		}
		if json.Unmarshal(a, &al) == nil && al.Prefixes != nil {
			p.prefixes = al.Prefixes
		}
	}
	for _, n := range sortedKeys(ui) {
		for _, m := range p.entryProblems(ui[n]) {
			add("ui %q: %s", n, m)
		}
	}
	for _, n := range sortedKeys(bs) {
		for _, m := range p.blockProblems(n, bs[n]) {
			add("block %q: %s", n, m)
		}
	}
	for _, n := range sortedKeys(comps) {
		for _, m := range p.compositionProblems(n, comps[n]) {
			add("composition %q: %s", n, m)
		}
	}
	for _, m := range p.cycleProblems() {
		add("%s", m)
	}
	trees, err := r.ViewerTrees()
	if err != nil {
		return append(out, err.Error())
	}
	return append(out, p.carriedProblems(r.P.Name+"@"+r.Label, trees)...)
}

// viewerManifest is what the checks read of a carried viewer's viewer.json: its events with their
// types and the state keys it accepts (round 5b (b)).
type viewerManifest struct {
	Viewer  string                                    `json:"viewer"`
	Emits   map[string]struct{ Type json.RawMessage } `json:"emits"`
	Accepts map[string]json.RawMessage                `json:"accepts"`
}

// carriedProblems checks the viewer trees the package carries: each has a viewer.json, is pinned by
// a viewer declaration or a block, and every pin of it names this revision (at) and its tree id; a
// block on a carried viewer emits and takes only what viewer.json declares, with the same types.
// For a viewer fetched from kton-web the same checks are install's (it fetches the tree).
func (p *uiPackage) carriedProblems(here string, trees map[string]map[string][]byte) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	ids := map[string]string{}
	manifests := map[string]*viewerManifest{}
	for _, v := range sortedKeys(trees) {
		id, err := ktonpkg.TreeIDOf(trees[v])
		if err != nil {
			add("viewer tree %q: %v", v, err)
			continue
		}
		ids[v] = id
		body, ok := trees[v]["viewer.json"]
		if !ok {
			add("viewer tree %q has no viewer.json", v)
			continue
		}
		var m viewerManifest
		if err := json.Unmarshal(body, &m); err != nil {
			add("viewer tree %q: viewer.json: %v", v, err)
			continue
		}
		if m.Viewer != v {
			add("viewer tree %q: viewer.json says viewer %q; it must name its folder (kton-web's key is viewer)", v, m.Viewer)
		}
		manifests[v] = &m
	}
	pinned := map[string]bool{}
	checkPin := func(what string, v Viewer) {
		id, carried := ids[v.Viewer]
		if !carried {
			return
		}
		pinned[v.Viewer] = true
		if v.Tree != id {
			add("%s pins the carried viewer %q at tree %s; its files have the tree %s", what, v.Viewer, v.Tree, id)
		}
		if v.At != here {
			add("%s: a carried viewer's at is %q (package@revision), not %q", what, here, v.At)
		}
	}
	for _, n := range sortedKeys(p.viewers) {
		checkPin(fmt.Sprintf("viewer %q", n), p.viewers[n])
	}
	for _, n := range sortedKeys(p.blocks) {
		b := p.blocks[n]
		var pin Viewer
		var s string
		if json.Unmarshal(b.Viewer, &s) == nil {
			pin = p.viewers[s]
		} else if v, err := decode[Viewer](b.Viewer); err == nil {
			pin = v
			checkPin(fmt.Sprintf("block %q", n), v)
		}
		m := manifests[pin.Viewer]
		if m == nil {
			continue
		}
		for _, e := range sortedKeys(b.Emits) {
			ve, ok := m.Emits[e]
			if !ok {
				add("block %q emits %s, which the viewer %q does not report", n, e, pin.Viewer)
				continue
			}
			bt, err1 := parseType(b.Emits[e])
			vt, err2 := parseType(ve.Type)
			if err1 == nil && (err2 != nil || bt != vt) {
				add("block %q: event %s is a %s, the viewer %q reports %s", n, e, bt, pin.Viewer, string(ve.Type))
			}
		}
		for _, o := range sortedKeys(b.Options) {
			va, ok := m.Accepts[o]
			if !ok {
				add("block %q has the option %s, which the viewer %q does not accept", n, o, pin.Viewer)
				continue
			}
			bt, err1 := parseType(b.Options[o])
			vt, err2 := parseType(va)
			if err1 == nil && (err2 != nil || bt != vt) {
				add("block %q: option %s is a %s, the viewer %q accepts %s", n, o, bt, pin.Viewer, string(va))
			}
		}
	}
	for _, v := range sortedKeys(trees) {
		if !pinned[v] {
			add("the package carries the viewer tree %q, but no viewer or block pins it", v)
		}
	}
	return out
}

// whenProblems checks a when or a subject: the kind is one of claim, foton, scope; a type is a full
// IRI or a CURIE whose prefix the package's aliases.json declares.
func (p *uiPackage) whenProblems(w *When, what string) []string {
	var out []string
	if !has([]string{"claim", "foton", "scope"}, w.Kind) {
		out = append(out, fmt.Sprintf("%s kind %q is not one of claim, foton, scope", what, w.Kind))
	}
	if w.Type != "" {
		if msg := p.typeProblem(w.Type); msg != "" {
			out = append(out, fmt.Sprintf("%s type %s", what, msg))
		}
	}
	return out
}

func (p *uiPackage) typeProblem(t string) string {
	i := strings.Index(t, ":")
	if i <= 0 {
		return fmt.Sprintf("%q is neither an IRI nor a CURIE", t)
	}
	prefix, rest := t[:i], t[i+1:]
	if strings.HasPrefix(rest, "//") || prefix == "urn" {
		return ""
	}
	if _, ok := p.prefixes[prefix]; !ok {
		return fmt.Sprintf("%q: the prefix %q is not in the package's aliases.json", t, prefix)
	}
	return ""
}

// queryVars are the $VARs a query uses.
func queryVars(q []byte) []string {
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`).FindAllSubmatch(withoutComments(q), -1) {
		seen[string(m[1])] = true
	}
	return sortedKeys(seen)
}

func (p *uiPackage) entryProblems(e UIEntry) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if !has(Slots, e.Slot) {
		add("slot %q is not one of %s", e.Slot, strings.Join(Slots, ", "))
	}
	if strings.TrimSpace(e.Label) == "" {
		add("has no label")
	}
	if e.When != nil {
		out = append(out, p.whenProblems(e.When, "when")...)
	}
	if e.Query != "" {
		if _, ok := p.queries[e.Query]; !ok {
			add("the query %q is not in the package", e.Query)
		}
	}
	if e.Ray != "" && (p.ray == nil || p.ray.Name != e.Ray) {
		add("the ray %q is not in the package", e.Ray)
	}
	if e.Viewer != "" {
		if _, ok := p.viewers[e.Viewer]; !ok {
			add("the viewer %q is not in the package", e.Viewer)
		}
	}
	if e.Control != "" {
		if _, ok := p.controls[e.Control]; !ok {
			add("the control %q is not in the package", e.Control)
		}
	}
	if e.Composition != "" {
		if _, ok := p.compositions[e.Composition]; !ok {
			add("the composition %q is not in the package", e.Composition)
		}
	}
	// What each slot needs.
	view := e.Viewer != "" || e.Composition != ""
	switch e.Slot {
	case "activity":
		if e.Query == "" && e.Composition == "" {
			add("slot activity needs a query or a composition")
		}
	case "home-tile", "status-item", "finding-source":
		if e.Query == "" {
			add("slot %s needs a query", e.Slot)
		}
	case "review-view":
		if !view || e.When == nil {
			add("slot review-view needs a viewer or composition, and when")
		}
	case "record-view":
		if (!view && e.Query == "") || e.When == nil || e.When.Type == "" {
			add("slot record-view needs a viewer, composition or query, and when with a type")
		}
	case "run-action":
		if e.Ray == "" {
			add("slot run-action needs a ray")
		}
	case "material-editor":
		if !view {
			add("slot material-editor needs a viewer or composition")
		}
	}
	// inputs: a query $VAR or a ray hole, from the shell's closed set.
	var qvars []string
	if q, ok := p.queries[e.Query]; ok {
		qvars = queryVars(q)
	}
	for _, k := range sortedKeys(e.Inputs) {
		if !has(ShellValues, e.Inputs[k]) {
			add("input %s binds %q, which is not a shell value (%s)", k, e.Inputs[k], strings.Join(ShellValues, ", "))
		}
		hole := e.Ray != "" && p.ray != nil && p.ray.Name == e.Ray && p.ray.Hole(k) != nil
		if !has(qvars, k) && !hole {
			add("input %s is neither a $VAR of the query nor a hole of the ray", k)
		}
	}
	for _, v := range qvars {
		if _, ok := e.Inputs[v]; !ok {
			add("the query %q uses $%s, which inputs does not bind", e.Query, v)
		}
	}
	if e.Area != nil {
		out = append(out, prefix("area: ", p.areaProblems(e.Slot, e.Area))...)
	}
	return out
}

// The result shapes of an area's approvals and history queries. Who signed is a key (?signer); its
// name, tier and whether it is an agent come from the host's re-verified answer (C1.3), never from
// the query, so an agent's approval is not counted by the shell, not filtered by the data.
var (
	ApprovalsShape = []string{"claim", "record", "outcome", "signer"}
	HistoryShape   = []string{"claim", "replaces", "replacedBy", "when"}
)

// missingProjection are the variables of shape a SELECT query does not project. SELECT * projects
// all; a query that is not a SELECT projects none.
func missingProjection(q []byte, shape []string) []string {
	if len(shape) == 0 {
		return nil
	}
	text := string(withoutComments(q))
	m := regexp.MustCompile(`(?is)\bSELECT\b(.*?)(\bWHERE\b|\{)`).FindStringSubmatch(text)
	if m == nil {
		return shape
	}
	if strings.Contains(m[1], "*") {
		return nil
	}
	var out []string
	for _, v := range shape {
		if !regexp.MustCompile(`\?` + regexp.QuoteMeta(v) + `\b`).MatchString(m[1]) {
			out = append(out, v)
		}
	}
	return out
}

var ruleID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (p *uiPackage) areaProblems(slot string, a *Area) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if slot != "activity" {
		add("belongs to an activity entry")
	}
	if a.Scopes == nil || strings.TrimSpace(a.Scopes.Records) == "" {
		add("scopes.records names no scope")
	} else {
		for _, r := range a.Scopes.Reads {
			if strings.TrimSpace(r) == "" {
				add("scopes.reads has an empty name")
			}
		}
	}
	used := map[string]bool{}
	for _, q := range []struct {
		field, name string
		shape       []string
	}{{"record", a.Record, nil}, {"raci", a.Raci, nil}, {"training", a.Training, nil},
		{"approvals", a.Approvals, ApprovalsShape}, {"history", a.History, HistoryShape}} {
		if q.name == "" {
			continue
		}
		body, ok := p.queries[q.name]
		if !ok {
			add("%s: the query %q is not in the package", q.field, q.name)
			continue
		}
		for _, v := range missingProjection(body, q.shape) {
			add("%s: the query %q does not select ?%s (%s)", q.field, q.name, v, "?"+strings.Join(q.shape, " ?"))
		}
		for _, v := range queryVars(body) {
			used[v] = true
			if _, ok := a.Params[v]; !ok {
				add("%s: the query %q uses $%s, which params does not bind", q.field, q.name, v)
			}
		}
	}
	for _, k := range sortedKeys(a.Params) {
		if !has(ShellValues, a.Params[k]) {
			add("param %s binds %q, which is not a shell value (%s)", k, a.Params[k], strings.Join(ShellValues, ", "))
		}
		if !used[k] {
			add("param %s is not a $VAR of an area query", k)
		}
	}
	for _, c := range a.Actions {
		if _, ok := p.controls[c]; !ok {
			add("action: the control %q is not in the package", c)
		}
	}
	for _, d := range a.Documents {
		if strings.TrimSpace(d) == "" {
			add("documents has an empty key")
		}
	}
	if f := a.Findings; f != nil {
		if f.Package == "" || len(f.Rules) == 0 {
			add("findings needs a package and rules")
		}
		seen := map[string]bool{}
		for _, r := range f.Rules {
			if !ruleID.MatchString(r) {
				add("findings rule %q is not a rule id", r)
			}
			if seen[r] {
				add("findings names the rule %q twice", r)
			}
			seen[r] = true
		}
	}
	return out
}

func (p *uiPackage) blockProblems(name string, b Block) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if b.Block != "" && b.Block != name {
		add("says it is %q", b.Block)
	}
	var s string
	if len(b.Viewer) == 0 {
		add("names no viewer")
	} else if json.Unmarshal(b.Viewer, &s) == nil {
		if _, ok := p.viewers[s]; !ok {
			add("the viewer %q is not in the package", s)
		}
	} else if v, err := decode[Viewer](b.Viewer); err != nil {
		add("viewer: %v", err)
	} else {
		if v.Viewer == "" || v.At == "" {
			add("an inline viewer needs viewer and at")
		}
		if !ktonpkg.IsContentHash(v.Tree) {
			add("the viewer's tree %q is not a content hash: an inline viewer is pinned by its tree", v.Tree)
		}
		if len(v.Inputs) > 0 || len(v.Intents) > 0 {
			add("an inline viewer pins a tree only; inputs and intents belong to a viewers/ declaration")
		}
	}
	for _, c := range b.Requires {
		switch {
		case has(ActCapabilities, c):
			add("requires %s, an act: an act never hides a block, so it can only be optional", c)
		case !has(DataCapabilities, c):
			add("requires %q, which is not a capability", c)
		}
	}
	for _, c := range b.Optional {
		if !has(DataCapabilities, c) && !has(ActCapabilities, c) {
			add("optional %q is not a capability", c)
		}
	}
	for i := range b.Accepts {
		out = append(out, p.whenProblems(&b.Accepts[i], fmt.Sprintf("accepts[%d]", i))...)
	}
	for _, o := range sortedKeys(b.Options) {
		if _, err := parseType(b.Options[o]); err != nil {
			add("option %s: %v", o, err)
		}
	}
	for _, e := range sortedKeys(b.Emits) {
		if has(actIntents, e) {
			add("emits %s, an act: acts that sign or write are never wired", e)
		}
		if _, err := parseType(b.Emits[e]); err != nil {
			add("event %s: %v", e, err)
		}
	}
	return out
}

// compCtx is what a source may name at one place in a composition.
type compCtx struct {
	c      *Composition
	vars   map[string]VarType
	inRow  bool
	inputs []string
}

// sourceType checks a source and gives its type when it has one ("" type: unknown, any scalar).
func (x *compCtx) sourceType(src string) (VarType, string) {
	switch {
	case strings.HasPrefix(src, "var."):
		t, ok := x.vars[strings.TrimPrefix(src, "var.")]
		if !ok {
			return VarType{}, fmt.Sprintf("%q reads a var the composition does not declare", src)
		}
		return t, ""
	case strings.HasPrefix(src, "row."):
		if !x.inRow || len(src) == 4 {
			return VarType{}, fmt.Sprintf("%q is only a source inside an every", src)
		}
		return VarType{}, ""
	case has(compositionValues, src):
		switch src {
		case "subject.iri", "part.iri":
			return VarType{Of: "iri"}, ""
		case "subject.id", "subject.claim", "composition.id":
			return VarType{Of: "hash"}, ""
		}
		return VarType{Of: "literal"}, ""
	case has(x.c.Params, src):
		return VarType{}, ""
	}
	return VarType{}, fmt.Sprintf("%q is not a source: not in the closed set, not var.<name>, row.<var> or a param of the composition", src)
}

func (p *uiPackage) compositionProblems(name string, c Composition) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if c.Composition != "" && c.Composition != name {
		add("says it is %q", c.Composition)
	}
	if !has(CompositionKinds, c.Kind) {
		add("kind %q is not one of %s", c.Kind, strings.Join(CompositionKinds, ", "))
	}
	static := c.Kind == "report" || c.Kind == "deck"
	if c.Subject != nil {
		out = append(out, p.whenProblems(c.Subject, "subject")...)
	}
	x := &compCtx{c: &c, vars: map[string]VarType{}, inputs: c.Inputs}
	for _, n := range c.Params {
		if has(reserved, n) {
			add("param %q is a reserved name", n)
		}
	}
	for _, n := range sortedKeys(c.Vars) {
		v := c.Vars[n]
		if has(reserved, n) || has(c.Params, n) {
			add("var %q shadows a reserved name or a param", n)
		}
		t, err := parseType(v.Type)
		if err != nil {
			add("var %s: %v", n, err)
			continue
		}
		x.vars[n] = t
		if len(v.Default) > 0 && !conforms(v.Default, t) {
			add("var %s: the default %s is not a %s", n, string(v.Default), t)
		}
		if static && len(v.Default) == 0 {
			add("var %s has no default: in a %s nothing interacts, so every var needs one", n, c.Kind)
		}
	}
	for _, e := range sortedKeys(c.Emits) {
		if has(actIntents, e) {
			add("emits %s, an act: acts are never wired", e)
		}
		if _, ok := x.vars[c.Emits[e]]; !ok {
			add("emits %s from the var %q, which it does not declare", e, c.Emits[e])
		}
	}
	if c.Render != nil {
		if c.Render.Ray == "" {
			add("render names no ray")
		} else if c.Render.Package == "" && (p.ray == nil || p.ray.Name != c.Render.Ray) {
			add("render: the ray %q is not in the package", c.Render.Ray)
		}
	}
	ids := map[string]bool{}
	placeholders := map[string]int{}
	var walk func(parts []Part, depth int)
	walk = func(parts []Part, depth int) {
		for i, pt := range parts {
			where := pt.ID
			if where == "" {
				where = fmt.Sprintf("#%d", i+1)
			}
			out = append(out, prefix("part "+where+": ", p.partProblems(x, pt, ids, placeholders))...)
			walk(pt.Parts, depth+1)
		}
	}
	walk(c.Parts, 1)
	for _, in := range sortedKeys(placeholders) {
		if placeholders[in] > 1 {
			add("the input %q is placed %d times; one placeholder per input", in, placeholders[in])
		}
	}
	useIDs := map[string]bool{}
	for i, u := range c.EachPart {
		out = append(out, prefix(fmt.Sprintf("eachPart[%d]: ", i), p.useProblems(x, u, useIDs))...)
	}
	return out
}

func prefix(p string, msgs []string) []string {
	for i := range msgs {
		msgs[i] = p + msgs[i]
	}
	return msgs
}

func (p *uiPackage) partProblems(x *compCtx, pt Part, ids map[string]bool, placeholders map[string]int) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if pt.ID != "" {
		if ids[pt.ID] {
			add("the id %q is used twice in the composition", pt.ID)
		}
		ids[pt.ID] = true
	}
	nested := len(pt.Composition) > 0
	kinds := 0
	for _, b := range []bool{pt.From != "", nested, len(pt.Blocks) > 0 || len(pt.Parts) > 0} {
		if b {
			kinds++
		}
	}
	if kinds != 1 {
		add("must hold exactly one of: blocks and parts, a composition, or from")
	}
	if pt.ID == "" && pt.From == "" && (pt.Heading != "" || len(pt.Blocks) > 0 || nested) {
		add("has no id: only an unnamed part that holds parts may go without one")
	}
	if pt.From != "" {
		placeholders[pt.From]++
		if !has(x.inputs, pt.From) {
			add("from %q is not a declared input", pt.From)
		}
		if pt.ID != "" || pt.Heading != "" || len(pt.Params) > 0 || len(pt.Emits) > 0 || pt.Every != nil {
			add("a placeholder holds only from")
		}
	}
	inner := *x
	if pt.Every != nil {
		out = append(out, p.everyProblems(x, pt.Every)...)
		inner.inRow = true
	}
	if !nested && (len(pt.Params) > 0 || len(pt.Emits) > 0) {
		add("params and emits belong to a part that nests a composition")
	}
	if nested {
		out = append(out, p.nestedProblems(&inner, pt)...)
	}
	useIDs := map[string]bool{}
	for i, u := range pt.Blocks {
		where := u.ID
		if where == "" {
			where = fmt.Sprintf("#%d", i+1)
		}
		out = append(out, prefix("use "+where+": ", p.useProblems(&inner, u, useIDs))...)
	}
	return out
}

func (p *uiPackage) everyProblems(x *compCtx, e *Every) []string {
	var out []string
	if e.As != "" && e.As != "row" {
		out = append(out, fmt.Sprintf("every as %q: rows are row.<var>", e.As))
	}
	q, msg := p.queryRef(x, e.Query)
	if msg != "" {
		return append(out, "every: "+msg)
	}
	out = append(out, prefix("every: ", p.paramProblems(x, q, e.Params, nil))...)
	return out
}

// queryRef reads a query reference; for a local query it gives its bytes.
func (p *uiPackage) queryRef(x *compCtx, raw json.RawMessage) ([]byte, string) {
	r, err := parseRef(raw, "query")
	if err != nil {
		return nil, err.Error()
	}
	switch {
	case r.Input != "":
		if !has(x.inputs, r.Input) {
			return nil, fmt.Sprintf("the query reads the input %q, which is not declared", r.Input)
		}
		return nil, ""
	case r.Package != "":
		return nil, ""
	}
	q, ok := p.queries[r.Name]
	if !ok {
		return nil, fmt.Sprintf("the query %q is not in the package", r.Name)
	}
	return q, ""
}

// paramProblems checks a use's params (and refine) against a local query: each param is a $VAR the
// query uses, every used $VAR is bound, and no set goes into a query (v0.9.0).
func (p *uiPackage) paramProblems(x *compCtx, q []byte, params map[string]string, refine map[string]Refine) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	for _, k := range sortedKeys(params) {
		t, msg := x.sourceType(params[k])
		if msg != "" {
			add("param %s: %s", k, msg)
		} else if t.Set {
			add("param %s binds %s, a set: a set does not go into a query in v0.9.0", k, params[k])
		}
	}
	if q == nil {
		return out
	}
	vars := queryVars(q)
	for _, k := range sortedKeys(params) {
		if !has(vars, k) {
			add("param %s is not a $VAR of the query", k)
		}
	}
	for _, k := range sortedKeys(refine) {
		if !has(vars, k) {
			add("refine %s is not a $VAR of the query", k)
		}
	}
	for _, v := range vars {
		_, bound := params[v]
		_, refined := refine[v]
		if !bound && !refined {
			add("the query uses $%s, which neither params nor refine binds", v)
		}
	}
	return out
}

func (p *uiPackage) useProblems(x *compCtx, u Use, ids map[string]bool) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if u.ID != "" {
		if ids[u.ID] {
			add("the id %q is used twice in the part", u.ID)
		}
		ids[u.ID] = true
	}
	inner := *x
	if u.Every != nil {
		out = append(out, p.everyProblems(x, u.Every)...)
		inner.inRow = true
	}
	var block *Block
	if len(u.Use) == 0 {
		add("uses no block")
	} else if r, err := parseRef(u.Use, "block"); err != nil {
		add("use: %v", err)
	} else if r.Input != "" {
		add("use: a block comes from a package, not an input")
	} else if r.Local() {
		b, ok := p.blocks[r.Name]
		if !ok {
			add("the block %q is not in the package", r.Name)
		} else {
			block = &b
		}
	}
	if len(u.Query) > 0 {
		q, msg := p.queryRef(&inner, u.Query)
		if msg != "" {
			add("%s", msg)
		} else {
			out = append(out, p.paramProblems(&inner, q, u.Params, u.Refine)...)
		}
	} else if len(u.Params) > 0 || len(u.Refine) > 0 {
		add("params and refine need a query")
	}
	for _, k := range sortedKeys(u.Refine) {
		rf := u.Refine[k]
		if _, bound := u.Params[k]; bound {
			add("refine %s is also in params", k)
		}
		switch {
		case len(rf.Values) > 0 && rf.Type == "":
			if len(rf.Default) > 0 {
				found := false
				for _, v := range rf.Values {
					found = found || string(v) == string(rf.Default)
				}
				if !found {
					add("refine %s: the default is not one of the values", k)
				}
			}
		case len(rf.Values) == 0 && rf.Type != "":
			if !has([]string{"iri", "literal", "integer"}, rf.Type) {
				add("refine %s: type %q is not iri, literal or integer", k, rf.Type)
			} else if len(rf.Default) == 0 {
				add("refine %s: a typed refine needs a default", k)
			} else if !conforms(rf.Default, VarType{Of: rf.Type}) {
				add("refine %s: the default is not a %s", k, rf.Type)
			}
		default:
			add("refine %s needs values or a type, not both", k)
		}
	}
	for _, o := range sortedKeys(u.Options) {
		val := u.Options[o]
		var want *VarType
		if block != nil {
			raw, ok := block.Options[o]
			if !ok {
				add("option %s is not an option of the block", o)
				continue
			}
			if t, err := parseType(raw); err == nil {
				want = &t
			}
		}
		var s string
		if json.Unmarshal(val, &s) == nil && strings.HasPrefix(s, "var.") {
			t, msg := x.sourceType(s)
			if msg != "" {
				add("option %s: %s", o, msg)
			} else if want != nil && !assignable(t, *want) {
				add("option %s wants a %s, %s is a %s", o, *want, s, t)
			}
		} else if want != nil && !conforms(val, *want) {
			add("option %s wants a %s, %s is not one", o, *want, string(val))
		}
	}
	for _, e := range sortedKeys(u.Emits) {
		if has(actIntents, e) {
			add("wires %s, an act: acts that sign or write are never wired", e)
			continue
		}
		vt, ok := x.vars[u.Emits[e]]
		if !ok {
			add("emits %s into the var %q, which the composition does not declare", e, u.Emits[e])
			continue
		}
		if block == nil {
			continue
		}
		raw, ok := block.Emits[e]
		if !ok {
			add("the block does not emit %s", e)
			continue
		}
		if et, err := parseType(raw); err == nil && et != vt {
			add("emits %s (%s) into the var %s (%s)", e, et, u.Emits[e], vt)
		}
	}
	if u.Up && len(u.Emits) == 0 {
		add("up without emits: an unwired intent goes up anyway")
	}
	switch u.Empty {
	case "", "omit", "show":
	default:
		add("empty %q is not omit or show", u.Empty)
	}
	if u.File != "" {
		if _, ok := p.content[u.File]; !ok {
			add("the file %q is not content of the package", u.File)
		}
	}
	if len(u.Scope) > 0 {
		var s string
		if json.Unmarshal(u.Scope, &s) != nil {
			var m map[string]string
			if err := json.Unmarshal(u.Scope, &m); err != nil || len(m) != 2 || m["from"] == "" || m["slot"] == "" {
				add("scope is a scope name or {from, slot}")
			} else if !has(x.inputs, m["from"]) {
				add("scope from %q is not a declared input", m["from"])
			}
		} else if s == "" {
			add("scope is empty")
		}
	}
	for i, a := range u.Acts {
		out = append(out, prefix(fmt.Sprintf("act %d: ", i+1), p.actProblems(&inner, a))...)
	}
	return out
}

func (p *uiPackage) actProblems(x *compCtx, a Act) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	var fields []string
	switch a.Intent {
	case "sign", "record":
		if a.Ray != "" {
			add("a %s act names a control, not a ray", a.Intent)
		}
		c, ok := p.controls[a.Control]
		if !ok {
			add("the control %q is not in the package", a.Control)
			break
		}
		want := map[string]string{"sign": ControlDecision, "record": ControlRecord}[a.Intent]
		if c.EffectiveKind() != want {
			add("a %s act needs a %s control; %q is a %s", a.Intent, want, a.Control, c.EffectiveKind())
		}
		for _, t := range append([]string{c.Say.Template}, c.Say.Templates...) {
			var tpl struct {
				Fields map[string]json.RawMessage `json:"fields"`
			}
			if body, ok := p.templates[t]; ok && json.Unmarshal(body, &tpl) == nil {
				fields = append(fields, sortedKeys(tpl.Fields)...)
			}
		}
	case "run":
		if a.Control != "" {
			add("a run act names a ray, not a control")
		}
		if p.ray == nil || p.ray.Name != a.Ray {
			add("the ray %q is not in the package", a.Ray)
			break
		}
		for _, h := range p.ray.Holes {
			fields = append(fields, h.Name)
		}
	default:
		add("intent %q is not sign, record or run", a.Intent)
	}
	for _, k := range sortedKeys(a.Fields) {
		if _, msg := x.sourceType(a.Fields[k]); msg != "" {
			add("field %s: %s", k, msg)
		}
		if fields != nil && !has(fields, k) {
			add("field %s is not a field of the control's template or a hole of the ray", k)
		}
	}
	return out
}

// nestedProblems checks a part that nests a composition: its params bind the child's declared
// params (all of them) and subject; its emits wire events the child declares, type to type.
func (p *uiPackage) nestedProblems(x *compCtx, pt Part) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	r, err := parseRef(pt.Composition, "composition")
	if err != nil {
		return []string{err.Error()}
	}
	if r.Input != "" {
		return []string{"a composition comes from a package, not an input"}
	}
	var child *Composition
	if r.Local() {
		c, ok := p.compositions[r.Name]
		if !ok {
			return []string{fmt.Sprintf("the composition %q is not in the package", r.Name)}
		}
		child = &c
	}
	for _, k := range sortedKeys(pt.Params) {
		t, msg := x.sourceType(pt.Params[k])
		if msg != "" {
			add("param %s: %s", k, msg)
			continue
		}
		if k == "subject" {
			if strings.HasPrefix(pt.Params[k], "var.") && !assignable(t, VarType{Of: "iri"}) {
				add("subject binds %s (%s): a subject comes from an iri or hash var", pt.Params[k], t)
			}
			continue
		}
		if child != nil && !has(child.Params, k) {
			add("param %s is not a param of the composition %q", k, r.Name)
		}
	}
	if child != nil {
		for _, n := range child.Params {
			if _, ok := pt.Params[n]; !ok {
				add("the composition %q needs the param %s, which is not bound", r.Name, n)
			}
		}
	}
	for _, e := range sortedKeys(pt.Emits) {
		if has(actIntents, e) {
			add("wires %s, an act: acts are never wired", e)
			continue
		}
		vt, ok := x.vars[pt.Emits[e]]
		if !ok {
			add("emits %s into the var %q, which the composition does not declare", e, pt.Emits[e])
			continue
		}
		if child == nil {
			continue
		}
		cv, ok := child.Emits[e]
		if !ok {
			add("the composition %q does not emit %s", r.Name, e)
			continue
		}
		if raw, ok := child.Vars[cv]; ok {
			if ct, err := parseType(raw.Type); err == nil && ct != vt {
				add("emits %s (%s) into the var %s (%s)", e, ct, pt.Emits[e], vt)
			}
		}
	}
	return out
}

// cycleProblems refuses compositions that nest themselves, and nesting deeper than MaxDepth, inside
// the package. Across packages compose checks the same.
func (p *uiPackage) cycleProblems() []string {
	edges := map[string][]string{}
	for n, c := range p.compositions {
		var walk func([]Part)
		walk = func(parts []Part) {
			for _, pt := range parts {
				if len(pt.Composition) > 0 {
					if r, err := parseRef(pt.Composition, "composition"); err == nil && r.Local() {
						edges[n] = append(edges[n], r.Name)
					}
				}
				walk(pt.Parts)
			}
		}
		walk(c.Parts)
	}
	var out []string
	reported := map[string]bool{}
	var depth func(n string, path []string) int
	depth = func(n string, path []string) int {
		for i, m := range path {
			if m == n {
				members := append([]string(nil), path[i:]...)
				key := append([]string(nil), members...)
				sort.Strings(key)
				if k := strings.Join(key, ","); !reported[k] {
					reported[k] = true
					out = append(out, "compositions nest in a cycle: "+strings.Join(append(members, n), " -> "))
				}
				return 0
			}
		}
		d := 1
		for _, m := range edges[n] {
			if _, ok := p.compositions[m]; ok {
				if k := 1 + depth(m, append(path, n)); k > d {
					d = k
				}
			}
		}
		return d
	}
	names := sortedKeys(p.compositions)
	for _, n := range names {
		if d := depth(n, nil); d > MaxDepth && len(out) == 0 {
			out = append(out, fmt.Sprintf("composition %q nests %d deep; at most %d", n, d, MaxDepth))
		}
	}
	sort.Strings(out)
	return out
}
