package ktonpkg

import (
	"fmt"
	"sort"
	"strings"

	"kton.dev/plankton/core"
)

// Identität — gerechnet vom kton-Kernel, nirgends nachgebaut.
//
//	Step       → Foton (SPEC §6.2), Identität = FotonID (§6.3)
//	gebunden   → Aktionsschlüssel (§6.3)
//	ungebunden → Potential (§6.4): eine Identität, aber kein Schlüssel
//	Paket      → Hash über die Foton-IDs seiner Steps und seine Löcher
//
// Der Protokolldeskriptor führt die **Kommandodatei als Inhaltshash** und die
// aufgelöste Befehlszeile. Runserver, Tool und Instanz stehen nicht darin —
// sie sind eine Bindung an ein Haus, keine Eigenschaft der Berechnung, und
// hängen als Claim daneben.

// Bindings sind die Werte für die Löcher: Dateihash oder Parameterwert.
type Bindings map[string]string

// Foton baut das Foton eines Steps.
//
// Ungebundene Stellen bleiben ohne Hash — genau so beschreibt SPEC §6.4 ein
// Potential.
func (r *Ray) Foton(stepID string, bindings Bindings, upstream map[string]string) (*core.Foton, error) {
	s := r.Step(stepID)
	if s == nil {
		return nil, fmt.Errorf("kein Step %q im Potential", stepID)
	}
	f := &core.Foton{}
	hashBySlot := map[string]string{}
	for _, in := range s.Inputs {
		slot := strings.TrimPrefix(in.Slot, "./")
		hash := ""
		switch {
		case in.Pinned():
			hash = in.Hash
		case strings.HasPrefix(in.From, FromHole):
			hash = bindings[strings.TrimPrefix(in.From, FromHole)]
		case strings.HasPrefix(in.From, FromUpstream):
			up, upSlot, _ := ParseUpstream(in.From)
			hash = upstream[up+":"+upSlot]
		}
		hashBySlot[slot] = hash
		f.Inputs = append(f.Inputs, core.FileRef{Path: slot, Hash: hash})
	}
	sort.Slice(f.Inputs, func(i, j int) bool { return f.Inputs[i].Path < f.Inputs[j].Path })
	for _, out := range s.Outputs {
		f.Outputs = append(f.Outputs, core.FileRef{Path: strings.TrimPrefix(out, "./")})
	}

	params, _ := ResolveParams(s, bindings)
	d := descriptor(s, hashBySlot, params)
	ref, err := core.ComputeProtocolRef(d)
	if err != nil {
		return nil, err
	}
	f.Protocol = core.Protocol{Kind: "improve.tool", Ref: ref, Descriptor: d}
	return f, nil
}

// descriptor beschreibt die Berechnung.
func descriptor(s *Step, hashBySlot, params map[string]string) map[string]any {
	d := map[string]any{}
	command := map[string]any{}
	if slot := CommandSlot(s); slot != "" {
		if h := hashBySlot[slot]; h != "" {
			command["file"] = h
		} else {
			command["slot"] = slot
		}
	}
	if s.Protocol.Command != nil {
		if args := ResolvedArgs(s, params); args != "" {
			command["args"] = args
		}
	}
	if len(command) > 0 {
		d["command"] = command
	} else if s.Protocol.Tool != "" {
		d["tool"] = s.Protocol.Tool
	}

	var vars []any
	for _, in := range s.Inputs {
		if in.Variable != "" {
			vars = append(vars, map[string]any{
				"name": in.Variable, "slot": strings.TrimPrefix(in.Slot, "./")})
		}
	}
	if len(vars) > 0 {
		sort.Slice(vars, func(i, j int) bool {
			return vars[i].(map[string]any)["name"].(string) <
				vars[j].(map[string]any)["name"].(string)
		})
		d["variables"] = vars
	}
	if len(params) > 0 {
		d["params"] = params
	}
	return d
}

// CommandSlot liefert den Slot der Kommandodatei.
func CommandSlot(s *Step) string {
	for _, in := range s.Inputs {
		if in.Variable == "command-file" {
			return strings.TrimPrefix(in.Slot, "./")
		}
	}
	return ""
}

// ResolveParams löst die skalaren Parameter auf: Literale bleiben,
// `hole:<name>` wird gebunden. Offene Löcher kommen als Namen zurück.
func ResolveParams(s *Step, bindings Bindings) (map[string]string, []string) {
	if len(s.Params) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(s.Params))
	var unbound []string
	for name, value := range s.Params {
		if !strings.HasPrefix(value, FromHole) {
			out[name] = value
			continue
		}
		if v := bindings[strings.TrimPrefix(value, FromHole)]; v != "" {
			out[name] = v
			continue
		}
		unbound = append(unbound, name)
	}
	sort.Strings(unbound)
	return out, unbound
}

// ResolvedArgs setzt die Parameterwerte in die Befehlszeile ein: `<name>`
// wird zum Wert. `<command-file>` bleibt stehen — den setzt improve.
func ResolvedArgs(s *Step, params map[string]string) string {
	if s.Protocol.Command == nil {
		return ""
	}
	args := s.Protocol.Command.Args
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		args = strings.ReplaceAll(args, "<"+name+">", params[name])
	}
	return args
}

// ErrPotential sagt, dass ein Step noch offene Stellen hat.
type ErrPotential struct {
	Step string
	Open []string
}

func (e *ErrPotential) Error() string {
	return fmt.Sprintf("Step %q ist ein Potential: %s ohne Bindung",
		e.Step, strings.Join(e.Open, ", "))
}

// ActionKey ist der Wiederverwendungsschlüssel eines Steps (SPEC §6.3).
//
// Offene Stellen sind ein Fehler und kein leerer Schlüssel: was nicht
// gebunden ist, darf nichts wiederverwenden.
func (r *Ray) ActionKey(stepID string, bindings Bindings, upstream map[string]string) (string, error) {
	f, err := r.Foton(stepID, bindings, upstream)
	if err != nil {
		return "", err
	}
	var open []string
	for _, in := range f.Inputs {
		if in.Hash == "" {
			open = append(open, in.Path)
		}
	}
	if _, unbound := ResolveParams(r.Step(stepID), bindings); len(unbound) > 0 {
		open = append(open, unbound...)
	}
	if len(open) > 0 {
		sort.Strings(open)
		return "", &ErrPotential{Step: stepID, Open: open}
	}
	return f.ActionKey()
}

// FotonID ist die Identität eines Steps — auch als Potential.
func (r *Ray) FotonID(stepID string, bindings Bindings, upstream map[string]string) (string, error) {
	f, err := r.Foton(stepID, bindings, upstream)
	if err != nil {
		return "", err
	}
	return f.FotonID()
}

// ID ist die Identität des Potentials als Ganzes: die Foton-Identitäten
// seiner Steps in Abhängigkeitsreihenfolge, samt seiner Löcher.
//
// Damit lässt sich über **das Paket** etwas behaupten, ohne es zu
// realisieren — der Bezugspunkt für Claims.
func (r *Ray) ID() (string, error) {
	order, err := r.Order()
	if err != nil {
		return "", err
	}
	ids := make([]any, 0, len(order))
	for _, id := range order {
		fid, err := r.FotonID(id, nil, nil)
		if err != nil {
			return "", err
		}
		ids = append(ids, map[string]any{"step": id, "foton": fid})
	}
	holes := make([]any, 0, len(r.Holes))
	sorted := append([]Hole(nil), r.Holes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, h := range sorted {
		holes = append(holes, map[string]any{
			"name": h.Name, "step": h.Step, "slot": h.Slot,
			"param": h.Param, "type": h.Type, "required": h.Required,
		})
	}
	b, err := core.CanonValue(map[string]any{"steps": ids, "holes": holes})
	if err != nil {
		return "", err
	}
	return core.HashBytes(b), nil
}

// ClaimID ist die Identität eines Claims.
func ClaimID(c Claim) (string, error) {
	b, err := core.CanonValue(c)
	if err != nil {
		return "", err
	}
	return core.HashBytes(b), nil
}

// MissingBindings nennt die Pflichtlöcher ohne Wert.
func (r *Ray) MissingBindings(bindings Bindings) []string {
	var out []string
	for _, h := range r.Holes {
		if !h.Required {
			continue
		}
		if bindings[h.Name] == "" && h.Default == "" {
			out = append(out, h.Name)
		}
	}
	sort.Strings(out)
	return out
}

// WithDefaults ergänzt fehlende Bindungen um die Vorgabewerte.
func (r *Ray) WithDefaults(bindings Bindings) Bindings {
	out := Bindings{}
	for k, v := range bindings {
		out[k] = v
	}
	for _, h := range r.Holes {
		if out[h.Name] == "" && h.Default != "" {
			out[h.Name] = h.Default
		}
	}
	return out
}
