package scope

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gitmick/ktonpkg"
	"kton.dev/plankton/core"
)

// A verticle is a package that brings, besides templates and queries, the parts a host needs to
// offer a kind of work: viewers, controls and MCP tools — and, where the work is a computation, its
// ray. Design: workstreams/active/cockpit/verticleformat/2026-11-02-verticle-profile.md.
//
// All three new parts are declarations, not code. A control is a say of one of the package's own
// templates, signed with the caller's key; an MCP tool binds an input schema to one of the package's
// queries, controls or its ray; a viewer names a kton-web viewer and wires its inputs to queries and
// its intents to controls. What runs them — the cockpit, its MCP server, kton-web — stays where it
// is, so admitting a verticle (claims.allowedPackages) lets a host do nothing its templates, queries
// and recorded steps do not already allow.
const ProfileVerticle = "https://kton.dev/profile/verticle"

// Roles of the verticle profile.
const (
	RoleViewer  = "viewer"
	RoleControl = "control"
	RoleMCP     = "mcp"
)

// Viewer wires a kton-web viewer to the package (kton-web viewers/<name>/viewer.json is the
// viewer's own contract; whether the input names match it is the host's check when it loads it).
type Viewer struct {
	Viewer  string            `json:"viewer"`            // e.g. "kton-web/feed"
	At      string            `json:"at,omitempty"`      // the human handle: a kton-web commit
	Tree    string            `json:"tree,omitempty"`    // the viewer's bytes: ktonpkg TreeID of its folder
	Inputs  map[string]string `json:"inputs,omitempty"`  // viewer input → query in this package
	Intents map[string]string `json:"intents,omitempty"` // viewer intent → control in this package
}

// Control is a say of one of the package's templates. Signer, scope and about say how the host
// picks them from the caller's input; the key is always the caller's own.
type Control struct {
	Title  string          `json:"title,omitempty"`
	Say    Say             `json:"say"`
	Signer json.RawMessage `json:"signer,omitempty"`
	Scope  json.RawMessage `json:"scope,omitempty"`
	About  json.RawMessage `json:"about,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"` // JSON Schema, an object at the top
}

// Say names the template a control says, or the templates it may choose from.
type Say struct {
	Template  string   `json:"template,omitempty"`
	Templates []string `json:"templates,omitempty"`
}

// MCPTool is one tool the host's MCP server offers for this package: exactly one of Query, Control
// or Ray, with Bind mapping the query's $VARs or the ray's holes to fields of Input.
type MCPTool struct {
	Description string            `json:"description,omitempty"`
	Query       string            `json:"query,omitempty"`
	Control     string            `json:"control,omitempty"`
	Ray         string            `json:"ray,omitempty"`
	Bind        map[string]string `json:"bind,omitempty"`
	Input       json.RawMessage   `json:"input"`
}

// AddViewer, AddControl and AddMCPTool lay a declaration down as part of the revision. The bytes
// are checked to be a declaration of that kind (no unknown fields), not yet against the package.
func (r *Revision) AddViewer(name string, body []byte) (string, error) {
	if _, err := decode[Viewer](body); err != nil {
		return "", fmt.Errorf("viewer %q: %w", name, err)
	}
	return r.addNamed(RoleViewer, name, body)
}

func (r *Revision) AddControl(name string, body []byte) (string, error) {
	if _, err := decode[Control](body); err != nil {
		return "", fmt.Errorf("control %q: %w", name, err)
	}
	return r.addNamed(RoleControl, name, body)
}

func (r *Revision) AddMCPTool(name string, body []byte) (string, error) {
	if _, err := decode[MCPTool](body); err != nil {
		return "", fmt.Errorf("mcp tool %q: %w", name, err)
	}
	return r.addNamed(RoleMCP, name, body)
}

// Viewers, Controls and MCPTools are the revision's declarations by name.
func (r *Revision) Viewers() (map[string]Viewer, error)   { return decodeAll[Viewer](r, RoleViewer) }
func (r *Revision) Controls() (map[string]Control, error) { return decodeAll[Control](r, RoleControl) }
func (r *Revision) MCPTools() (map[string]MCPTool, error) { return decodeAll[MCPTool](r, RoleMCP) }

func decode[T any](body []byte) (T, error) {
	var v T
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	err := d.Decode(&v)
	return v, err
}

func decodeAll[T any](r *Revision, role string) (map[string]T, error) {
	raw, err := r.named(role)
	if err != nil {
		return nil, err
	}
	out := make(map[string]T, len(raw))
	for name, body := range raw {
		v, err := decode[T](body)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", role, name, err)
		}
		out[name] = v
	}
	return out, nil
}

// VerticleProblems checks that the revision's declarations hold together: every name they use is a
// query, control, template, ray or hole of this package. References reach only inside the package
// (v1). Verify runs it for every revision of profile verticle; BuildVerticle refuses to seal on it.
func (r *Revision) VerticleProblems() []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	ts, err := r.Templates()
	if err != nil {
		return []string{err.Error()}
	}
	qs, err := r.Queries()
	if err != nil {
		return []string{err.Error()}
	}
	vs, err := r.Viewers()
	if err != nil {
		return []string{err.Error()}
	}
	cs, err := r.Controls()
	if err != nil {
		return []string{err.Error()}
	}
	ms, err := r.MCPTools()
	if err != nil {
		return []string{err.Error()}
	}
	var ray *ktonpkg.Ray
	if _, err := r.RayID(); err == nil {
		if ray, err = r.Ray(); err != nil {
			add("the ray is not readable: %v", err)
		}
	}

	for _, n := range sortedKeys(vs) {
		v := vs[n]
		if v.Viewer == "" {
			add("viewer %q names no viewer", n)
		}
		if v.Tree != "" && !ktonpkg.IsContentHash(v.Tree) {
			add("viewer %q: tree %q is not a content hash (sha256:<64 hex>)", n, v.Tree)
		}
		for _, in := range sortedKeys(v.Inputs) {
			if _, ok := qs[v.Inputs[in]]; !ok {
				add("viewer %q: input %s reads the query %q, which is not in the package", n, in, v.Inputs[in])
			}
		}
		for _, it := range sortedKeys(v.Intents) {
			if _, ok := cs[v.Intents[it]]; !ok {
				add("viewer %q: intent %s runs the control %q, which is not in the package", n, it, v.Intents[it])
			}
		}
	}
	for _, n := range sortedKeys(cs) {
		c := cs[n]
		says := append([]string(nil), c.Say.Templates...)
		if c.Say.Template != "" {
			says = append(says, c.Say.Template)
		}
		if len(says) == 0 {
			add("control %q says no template", n)
		}
		for _, t := range says {
			if _, ok := ts[t]; !ok {
				add("control %q says the template %q, which is not in the package", n, t)
			}
		}
		if len(c.Input) > 0 {
			if msg := objectSchema(c.Input); msg != "" {
				add("control %q: input %s", n, msg)
			}
		}
	}
	for _, n := range sortedKeys(ms) {
		m := ms[n]
		bound := 0
		for _, s := range []string{m.Query, m.Control, m.Ray} {
			if s != "" {
				bound++
			}
		}
		if bound != 1 {
			add("mcp tool %q must bind exactly one of query, control or ray, binds %d", n, bound)
		}
		if msg := objectSchema(m.Input); msg != "" {
			add("mcp tool %q: input %s", n, msg)
		}
		switch {
		case m.Query != "":
			q, ok := qs[m.Query]
			if !ok {
				add("mcp tool %q binds the query %q, which is not in the package", n, m.Query)
				break
			}
			for _, v := range sortedKeys(m.Bind) {
				if !regexp.MustCompile(`\$` + regexp.QuoteMeta(v) + `\b`).Match(q) {
					add("mcp tool %q binds $%s, which the query %q does not use", n, v, m.Query)
				}
			}
		case m.Control != "":
			if _, ok := cs[m.Control]; !ok {
				add("mcp tool %q binds the control %q, which is not in the package", n, m.Control)
			}
		case m.Ray != "":
			if ray == nil || ray.Name != m.Ray {
				add("mcp tool %q binds the ray %q, which is not in the package", n, m.Ray)
				break
			}
			for _, h := range sortedKeys(m.Bind) {
				if ray.Hole(h) == nil {
					add("mcp tool %q binds the hole %q, which the ray %q does not have", n, h, m.Ray)
				}
			}
		}
	}
	return out
}

// objectSchema is the minimal check of an input schema: a JSON object whose type is "object". Full
// JSON Schema validation is the host's (it has to validate calls against it anyway).
func objectSchema(raw json.RawMessage) string {
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil || s == nil {
		return "is not a JSON Schema object"
	}
	if s["type"] != "object" {
		return `must be a schema of "type": "object"`
	}
	return ""
}

func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// BuildVerticle builds a package of profile verticle from one source folder and seals it:
//
//	<src>/templates/<name>.json   claim templates
//	<src>/queries/<name>.rq       SPARQL queries
//	<src>/ray.json                optional: the computation, a ktonpkg.Ray; pinned inputs and the
//	                              holes' test data are paths relative to <src>
//	<src>/viewers/<name>.json     Viewer
//	<src>/controls/<name>.json    Control
//	<src>/mcp/<name>.json         MCPTool
//
// It refuses to seal a revision whose declarations do not hold together (VerticleProblems).
func BuildVerticle(dir, name, label, src string, key ed25519.PrivateKey, opt Options) (*Package, *Revision, error) {
	p, err := Create(dir, name, key, opt)
	if err != nil {
		return nil, nil, err
	}
	r, err := p.NewRevision(label)
	if err != nil {
		return nil, nil, err
	}
	if err := r.SetProfile(ProfileVerticle); err != nil {
		return nil, nil, err
	}
	n := 0
	for _, part := range []struct {
		dir, ext string
		put      func(string, []byte) (string, error)
	}{
		{"templates", ".json", r.AddTemplate},
		{"queries", ".rq", r.AddQuery},
		{"controls", ".json", r.AddControl},
		{"viewers", ".json", r.AddViewer},
		{"mcp", ".json", r.AddMCPTool},
	} {
		files, err := filepath.Glob(filepath.Join(src, part.dir, "*"+part.ext))
		if err != nil {
			return nil, nil, err
		}
		sort.Strings(files)
		for _, f := range files {
			body, err := os.ReadFile(f)
			if err != nil {
				return nil, nil, err
			}
			if _, err := part.put(strings.TrimSuffix(filepath.Base(f), part.ext), body); err != nil {
				return nil, nil, err
			}
			n++
		}
	}
	if body, err := os.ReadFile(filepath.Join(src, "ray.json")); err == nil {
		if err := addRayFrom(r, src, body); err != nil {
			return nil, nil, fmt.Errorf("ray.json: %w", err)
		}
		n++
	} else if !os.IsNotExist(err) {
		return nil, nil, err
	}
	if n == 0 {
		return nil, nil, fmt.Errorf("%s holds nothing of a verticle", src)
	}
	if probs := r.VerticleProblems(); len(probs) > 0 {
		return nil, nil, fmt.Errorf("the verticle does not hold together: %s", strings.Join(probs, "; "))
	}
	if _, err := r.Seal(); err != nil {
		return nil, nil, err
	}
	return p, r, nil
}

// addRayFrom reads a ktonpkg.Ray whose pinned inputs and test data are files under src.
func addRayFrom(r *Revision, src string, body []byte) error {
	ray, err := decode[ktonpkg.Ray](body)
	if err != nil {
		return err
	}
	read := func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(src, filepath.FromSlash(rel)))
	}
	bytesByHash := map[string][]byte{}
	for i := range ray.Steps {
		for j := range ray.Steps[i].Inputs {
			in := &ray.Steps[i].Inputs[j]
			if !in.Pinned() {
				continue
			}
			b, err := read(in.Path)
			if err != nil {
				return fmt.Errorf("step %s, %s: %w", ray.Steps[i].ID, in.Slot, err)
			}
			h := core.HashBytes(b)
			if in.Hash != "" && in.Hash != h {
				return fmt.Errorf("step %s, %s: %s has the hash %s, not %s", ray.Steps[i].ID, in.Slot, in.Path, h, in.Hash)
			}
			in.Hash = h
			bytesByHash[h] = b
		}
	}
	testData := map[string][]byte{}
	for _, h := range ray.Holes {
		if h.Default == "" {
			continue
		}
		switch h.Type {
		case ktonpkg.HoleFile:
			b, err := read(h.Default)
			if err != nil {
				return fmt.Errorf("test data of %s: %w", h.Name, err)
			}
			testData[h.Name] = b
		case ktonpkg.HoleParam:
			testData[h.Name] = []byte(h.Default)
		default:
			return fmt.Errorf("hole %s: test data for a %s hole is not supported by BuildVerticle yet", h.Name, h.Type)
		}
	}
	fetch := func(hash string) ([]byte, error) {
		if b, ok := bytesByHash[hash]; ok {
			return b, nil
		}
		return nil, fmt.Errorf("no bytes for %s", hash)
	}
	_, err = r.AddRay(&ray, fetch, testData)
	return err
}
