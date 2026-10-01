package scope

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gitmick/ktonpkg"
	"kton.dev/plankton/core"
	"kton.dev/plankton/foton"
	pregistry "kton.dev/plankton/registry"
)

// Das Workflow-Profil: ein Ray als Teil einer Revision.
//
// Jeder Step ist ein Potential im plankton-Register — dieselbe Identität, die ktonpkg.Ray.Foton
// rechnet, nur jetzt aufgezeichnet und signiert. Wie die Steps zusammenhängen, kann plankton nicht
// sagen: ein Loch hat keinen Hash, also fehlt der Ausdruck für "die Ausgabe von Step A". Die
// Verdrahtung steht deshalb in einem zusammengesetzten Potential der Art RayKind, dessen Deskriptor
// die Steps per Potential-Id und die Kanten nennt. plankton deutet den Deskriptor nicht, hasht ihn
// aber: die Ray-Id ist die Id dieses Potentials, und die Verdrahtung gehört zu ihr.
//
// FileRef.id (K4) ist dafür nicht der Ort: id ist mitgetragen, nicht abgedeckt, und die Verdrahtung
// muss zur Identität gehören. Ob plankton sie eines Tages selbst ausdrückt, ist eine offene Frage
// im Workstream (kernel-proposals/K4).

// RayKind ist die Protokollart des zusammengesetzten Potentials.
const RayKind = "kton.ray"

// Rollen eines Teils, im Objekt des isPartOf-Claims.
const (
	RoleRay      = "ray"
	RoleStep     = "step"
	RoleCode     = "code"      // die Bytes einer festen Eingabe eines Steps
	RoleTestData = "test-data" // die Vorgabe eines Lochs
	RoleSpectrum = "spectrum"
	RoleRun      = "run" // ein aufgezeichneter Lauf (Messung, Test, Rendern) — Unterart im Objekt
	RoleDocument = "document"
)

type rayDescriptor struct {
	Format string     `json:"format"`
	Steps  []rayStep  `json:"steps"`
	Wiring []rayWire  `json:"wiring,omitempty"`
	Params []rayParam `json:"params,omitempty"`
	Holes  []rayHole  `json:"holes,omitempty"`
	Order  []string   `json:"order"`
}

type rayStep struct {
	ID        string `json:"id"`
	Potential string `json:"potential"`
}

type rayWire struct {
	Step string `json:"step"`
	Slot string `json:"slot"`
	From string `json:"from"`
}

type rayParam struct {
	Step string `json:"step"`
	Name string `json:"name"`
	From string `json:"from"`
}

type rayHole struct {
	Name     string `json:"name"`
	Step     string `json:"step"`
	Slot     string `json:"slot,omitempty"`
	Param    string `json:"param,omitempty"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
}

// AddRay schreibt einen Ray in die Revision und gibt seine Id zurück (die des zusammengesetzten
// Potentials). fetch liefert die Bytes fester Eingaben nach Hash; testData die Vorgabe je Loch.
func (r *Revision) AddRay(ray *ktonpkg.Ray, fetch ktonpkg.Fetch, testData map[string][]byte) (string, error) {
	p := r.P
	order, err := ray.Order()
	if err != nil {
		return "", err
	}
	desc := rayDescriptor{Format: "kton-ray/1", Order: order}
	var outputs []foton.FileSpec
	for _, id := range order {
		s := ray.Step(id)
		f, err := ray.Foton(id, nil, nil)
		if err != nil {
			return "", err
		}
		pid, err := p.addPotential(f)
		if err != nil {
			return "", fmt.Errorf("Step %s: %w", id, err)
		}
		desc.Steps = append(desc.Steps, rayStep{ID: id, Potential: pid})
		if _, err := r.Claim(pid, PartOf, member(RoleStep, map[string]any{"step": id})); err != nil {
			return "", err
		}
		if s.Label != "" {
			if _, err := r.Claim(pid, Description, map[string]any{"value": s.Label}); err != nil {
				return "", err
			}
		}
		if s.Rationale != "" {
			if _, err := r.Claim(pid, Rationale, map[string]any{"value": s.Rationale}); err != nil {
				return "", err
			}
		}
		for _, in := range s.Inputs {
			slot := strings.TrimPrefix(in.Slot, "./")
			switch {
			case in.Pinned():
				body, err := fetch(in.Hash)
				if err != nil {
					return "", fmt.Errorf("Step %s, %s: %w", id, slot, err)
				}
				h, err := p.PutBlob(body)
				if err != nil {
					return "", err
				}
				if h != in.Hash {
					return "", fmt.Errorf("Step %s, %s: die Bytes haben den Hash %s, nicht %s", id, slot, h, in.Hash)
				}
				if _, err := r.Claim(h, PartOf, member(RoleCode, map[string]any{"step": id, "slot": slot})); err != nil {
					return "", err
				}
			default:
				desc.Wiring = append(desc.Wiring, rayWire{Step: id, Slot: slot, From: in.From})
			}
		}
		for name, v := range s.Params {
			if strings.HasPrefix(v, ktonpkg.FromHole) {
				desc.Params = append(desc.Params, rayParam{Step: id, Name: name, From: v})
			}
		}
		for _, o := range s.Outputs {
			outputs = append(outputs, foton.FileSpec{Path: id + "/" + strings.TrimPrefix(o, "./")})
		}
	}
	sort.Slice(desc.Params, func(i, j int) bool {
		return desc.Params[i].Step+desc.Params[i].Name < desc.Params[j].Step+desc.Params[j].Name
	})
	var inputs []foton.FileSpec
	for _, h := range ray.Holes {
		desc.Holes = append(desc.Holes, rayHole{Name: h.Name, Step: h.Step, Slot: h.Slot, Param: h.Param,
			Type: h.Type, Required: h.Required})
		if h.Type == ktonpkg.HoleFile {
			inputs = append(inputs, foton.FileSpec{Path: "hole/" + h.Name})
		}
	}
	sort.Slice(desc.Holes, func(i, j int) bool { return desc.Holes[i].Name < desc.Holes[j].Name })
	if len(outputs) == 0 {
		outputs = []foton.FileSpec{{Path: "ray"}}
	}
	rayID, err := p.addSpec(foton.Spec{Inputs: inputs, Outputs: outputs,
		Protocol: &foton.ProtocolSpec{Kind: RayKind, Descriptor: toMap(desc)}})
	if err != nil {
		return "", fmt.Errorf("der Ray: %w", err)
	}
	if _, err := r.Claim(rayID, PartOf, member(RoleRay, map[string]any{"name": ray.Name})); err != nil {
		return "", err
	}
	names := make([]string, 0, len(testData))
	for n := range testData {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		h, err := p.PutBlob(testData[name])
		if err != nil {
			return "", err
		}
		hole := ray.Hole(name)
		if hole == nil {
			return "", fmt.Errorf("Testdaten für %q, aber der Ray hat kein solches Loch", name)
		}
		extra := map[string]any{"hole": name, "ray": rayID}
		if hole.Slot != "" {
			extra["slot"] = hole.Slot
		}
		if _, err := r.Claim(h, PartOf, member(RoleTestData, extra)); err != nil {
			return "", err
		}
	}
	return rayID, nil
}

// RayID ist die Identität des Rays einer Revision: die Id seines zusammengesetzten Potentials im
// plankton-Register. Sie hängt nur an der Berechnung (Steps, Verdrahtung, Löcher), nicht an Uhr,
// Schlüssel oder Paket — dieselbe Berechnung in zwei Paketen hat dieselbe Ray-Id.
func (r *Revision) RayID() (string, error) {
	claims, err := r.Claims()
	if err != nil {
		return "", err
	}
	for _, c := range claims {
		if c.Predicate == PartOf && value(c)["role"] == RoleRay {
			return c.Subject, nil
		}
	}
	return "", fmt.Errorf("die Revision %s enthält keinen Ray", r.Label)
}

// Ray liest den Ray einer Revision aus den Records zurück. Feste Eingaben zeigen auf
// steps/<step>/<slot>, Testdaten auf testdaten/<loch>/<slot> — die Pfade der lesbaren Ansicht.
func (r *Revision) Ray() (*ktonpkg.Ray, error) {
	claims, err := r.Claims()
	if err != nil {
		return nil, err
	}
	var rayID, name string
	labels, rationales := map[string]string{}, map[string]string{}
	testData := map[string]ClaimInfo{}
	for _, c := range claims {
		switch c.Predicate {
		case PartOf:
			v := value(c)
			switch v["role"] {
			case RoleRay:
				rayID, name = c.Subject, str(v["name"])
			case RoleTestData:
				testData[str(v["hole"])] = c
			}
		case Description:
			labels[c.Subject] = str(c.Object["value"])
		case Rationale:
			rationales[c.Subject] = str(c.Object["value"])
		}
	}
	if rayID == "" {
		return nil, fmt.Errorf("die Revision %s enthält keinen Ray", r.Label)
	}
	reg, err := pregistry.Open(filepath.Join(r.P.Dir, PlanktonDir))
	if err != nil {
		return nil, err
	}
	rf, ok := reg.Foton(rayID)
	if !ok {
		return nil, fmt.Errorf("der Ray %s liegt nicht im Paket", rayID)
	}
	var desc rayDescriptor
	if err := fromMap(rf.Protocol.Descriptor, &desc); err != nil {
		return nil, fmt.Errorf("der Deskriptor des Rays: %w", err)
	}
	wire := map[string]string{}
	for _, w := range desc.Wiring {
		wire[w.Step+"/"+w.Slot] = w.From
	}
	out := &ktonpkg.Ray{Name: name}
	for _, st := range desc.Steps {
		f, ok := reg.Foton(st.Potential)
		if !ok {
			return nil, fmt.Errorf("das Potential von %s (%s) liegt nicht im Paket", st.ID, st.Potential)
		}
		s := ktonpkg.Step{ID: st.ID, Label: labels[st.Potential], Rationale: rationales[st.Potential]}
		cmd, _ := f.Protocol.Descriptor["command"].(map[string]any)
		if args, ok := cmd["args"].(string); ok {
			s.Protocol.Command = &ktonpkg.Command{Args: args}
		}
		vars := map[string]string{}
		if vs, ok := f.Protocol.Descriptor["variables"].([]any); ok {
			for _, v := range vs {
				if m, ok := v.(map[string]any); ok {
					vars[str(m["slot"])] = str(m["name"])
				}
			}
		}
		for _, in := range f.Inputs {
			i := ktonpkg.Input{Slot: in.Path, Variable: vars[in.Path]}
			if from, ok := wire[st.ID+"/"+in.Path]; ok {
				i.From = from
			} else {
				i.Path, i.Hash = path.Join("steps", st.ID, in.Path), in.Hash
			}
			s.Inputs = append(s.Inputs, i)
		}
		if ps, ok := f.Protocol.Descriptor["params"].(map[string]any); ok {
			for k, v := range ps {
				if s.Params == nil {
					s.Params = map[string]string{}
				}
				s.Params[k] = str(v)
			}
		}
		for _, p := range desc.Params {
			if p.Step == st.ID {
				if s.Params == nil {
					s.Params = map[string]string{}
				}
				s.Params[p.Name] = p.From
			}
		}
		for _, o := range f.Outputs {
			s.Outputs = append(s.Outputs, o.Path)
		}
		out.Steps = append(out.Steps, s)
	}
	for _, h := range desc.Holes {
		hole := ktonpkg.Hole{Name: h.Name, Step: h.Step, Slot: h.Slot, Param: h.Param, Type: h.Type, Required: h.Required}
		if c, ok := testData[h.Name]; ok {
			if h.Type == ktonpkg.HoleFile {
				hole.Default = path.Join("testdaten", h.Name, h.Slot)
			} else if body, err := r.P.Blob(c.Subject); err == nil {
				hole.Default = string(body)
			}
		}
		out.Holes = append(out.Holes, hole)
	}
	return out, nil
}

// --- Hilfen ----------------------------------------------------------------------------------------

func member(role string, extra map[string]any) map[string]any {
	v := map[string]any{"role": role}
	for k, x := range extra {
		v[k] = x
	}
	return map[string]any{"value": v}
}

func value(c ClaimInfo) map[string]any {
	v, _ := c.Object["value"].(map[string]any)
	return v
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func toMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func fromMap(m map[string]any, v any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// addPotential zeichnet ein Potential auf, wie ktonpkg es rechnet, und prüft, dass der Kernel
// dieselbe Id vergibt — zwei Meinungen über eine Identität würden sonst still auseinanderlaufen.
func (p *Package) addPotential(f *core.Foton) (string, error) {
	spec := foton.Spec{Protocol: &foton.ProtocolSpec{Kind: f.Protocol.Kind, Descriptor: f.Protocol.Descriptor}}
	for _, in := range f.Inputs {
		spec.Inputs = append(spec.Inputs, foton.FileSpec{Path: in.Path, Hash: in.Hash})
	}
	for _, o := range f.Outputs {
		spec.Outputs = append(spec.Outputs, foton.FileSpec{Path: o.Path, Hash: o.Hash})
	}
	id, err := p.addSpec(spec)
	if err != nil {
		return "", err
	}
	want, err := f.FotonID()
	if err == nil && want != id {
		return "", fmt.Errorf("der Kernel vergibt %s, ktonpkg rechnet %s", id, want)
	}
	return id, nil
}

func (p *Package) addSpec(spec foton.Spec) (string, error) {
	if p.key == nil {
		return "", fmt.Errorf("das Paket ist nur zum Lesen geöffnet")
	}
	env, id, err := foton.SignWith(spec, ed25519.PrivateKey(p.key))
	if err != nil {
		return "", err
	}
	got, err := p.AddFoton(env)
	if err != nil {
		return "", err
	}
	if got != id {
		return "", fmt.Errorf("das Register vergibt %s für %s", got, id)
	}
	return id, nil
}
