package ktonpkg

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// Plan ist ein Workflow-Paket, gebunden und in Reihenfolge: je Step, was vor dem Lauf wohin kopiert
// wird, die Befehlszeile und die Ein- und Ausgaben, so wie ein Ausführender sie aufzeichnet.
type Plan struct {
	Bindings Bindings   `json:"bindings"`
	Steps    []PlanStep `json:"steps"`
}

// PlanStep ist ein Step des Plans.
type PlanStep struct {
	ID      string            `json:"id"`
	Dir     string            `json:"dir"`
	Stage   []Stage           `json:"stage"`
	Params  map[string]string `json:"params,omitempty"`
	Cmd     string            `json:"cmd"`
	Inputs  []string          `json:"inputs"`
	Outputs []string          `json:"outputs"`
}

// Stage ist eine Kopie vor dem Lauf. Die Bytes landen unter ihrem Slotnamen im Stepordner, weil
// Skripte in ihrem Arbeitsordner lesen und schreiben. Eine Kopie ändert keinen Hash — darum
// verbindet sich die Linie über die Steps trotzdem.
type Stage struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// MissingError nennt die Pflichtlöcher, die ungebunden sind. Ein unvollständig gebundener Workflow
// läuft nicht an.
type MissingError struct{ Holes []string }

func (e *MissingError) Error() string {
	return "offene Pflichtlöcher: " + strings.Join(e.Holes, ", ")
}

// PlanOptions steuert das Binden.
type PlanOptions struct {
	// NoDefaults lässt die mitgelieferten Testdaten nicht als Bindung gelten.
	NoDefaults bool
}

// Plan bindet das Paket und legt die Steps in Reihenfolge an, mit work als Arbeitsordner. Pfade
// sind relativ zu dem Ordner, in dem auch b.Dir angegeben ist.
func (b *Bundle) Plan(work string, bindings Bindings, opt PlanOptions) (*Plan, error) {
	if b.Ray == nil {
		return nil, fmt.Errorf("%s ist kein Workflow-Paket", b.Package.Name)
	}
	if bindings == nil {
		bindings = Bindings{}
	}
	var missing []string
	if opt.NoDefaults {
		for _, h := range b.Ray.Holes {
			if h.Required && bindings[h.Name] == "" {
				missing = append(missing, h.Name)
			}
		}
		sort.Strings(missing)
	} else {
		missing = b.Ray.MissingBindings(bindings)
		bindings = b.Ray.WithDefaults(bindings)
	}
	if len(missing) > 0 {
		return nil, &MissingError{Holes: missing}
	}
	for name := range bindings {
		if b.Ray.Hole(name) == nil {
			return nil, fmt.Errorf("der Workflow hat kein Loch %q", name)
		}
	}
	outputs, err := b.declaredOutputs()
	if err != nil {
		return nil, err
	}
	order, err := b.Ray.Order()
	if err != nil {
		return nil, err
	}
	pl := &Plan{Bindings: bindings}
	for _, id := range order {
		s := b.Ray.Step(id)
		dir := path.Join(work, id)
		p := PlanStep{ID: id, Dir: dir}
		for _, in := range s.Inputs {
			var from string
			switch {
			case in.Pinned():
				from = path.Join(b.Dir, in.Path)
			case strings.HasPrefix(in.From, FromHole):
				name := strings.TrimPrefix(in.From, FromHole)
				v := bindings[name]
				if h := b.Ray.Hole(name); h != nil && v == h.Default {
					v = path.Join(b.Dir, v) // der Testdatensatz liegt im Paket
				}
				from = v
			default:
				up, slot, ok := ParseUpstream(in.From)
				if !ok {
					return nil, fmt.Errorf("%s: unlesbare Verdrahtung %q", id, in.From)
				}
				from = path.Join(work, up, slot)
			}
			to := path.Join(dir, strings.TrimPrefix(in.Slot, "./"))
			p.Stage = append(p.Stage, Stage{From: from, To: to})
			p.Inputs = append(p.Inputs, to)
		}
		sort.Strings(p.Inputs) // eine Ordnung, unabhängig von der Slot-Reihenfolge des Pakets (K7)
		params, unbound := ResolveParams(s, bindings)
		if len(unbound) > 0 {
			return nil, fmt.Errorf("%s: ungebundene Parameter %v", id, unbound)
		}
		p.Params = params
		p.Cmd = "cd " + dir + " && " + commandLine(s, params)
		for _, o := range outputs[id] {
			p.Outputs = append(p.Outputs, path.Join(dir, o))
		}
		if len(p.Outputs) == 0 {
			return nil, fmt.Errorf("%s: das Paket sagt nicht, was dieser Step erzeugt", id)
		}
		pl.Steps = append(pl.Steps, p)
	}
	return pl, nil
}

// commandLine macht aus der Befehlszeile eines Steps eine Shell-Zeile. In improve ist die erste
// Zeile das Image und `<command-file>` setzt der Server; hier läuft der Befehl im Image, das der
// Ausführende pinnt, und das Skript heißt wie sein Slot. Ein aus Ausführungen extrahiertes Paket
// trägt die Befehlszeile so, wie sie lief — ohne Image-Zeile und mit dem Interpreter vorne.
func commandLine(s *Step, params map[string]string) string {
	lines := strings.Split(ResolvedArgs(s, params), "\n")
	improve := len(lines) > 0 && strings.Contains(lines[0], "@sha256:")
	if improve {
		lines = lines[1:]
	}
	for i, l := range lines {
		if l == "<command-file>" {
			lines[i] = CommandSlot(s)
		}
	}
	if improve {
		return "Rscript " + strings.Join(lines, " ")
	}
	return strings.Join(lines, " ")
}

// declaredOutputs liest, was jeder Step erzeugt, aus den Referenzausgaben des Pakets:
// reference/<step>-<datei>.
func (b *Bundle) declaredOutputs() (map[string][]string, error) {
	names, err := b.List(ReferenceDir)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, s := range b.Ray.Steps {
		for _, n := range names {
			base := path.Base(n)
			if strings.HasPrefix(base, s.ID+"-") {
				out[s.ID] = append(out[s.ID], strings.TrimPrefix(base, s.ID+"-"))
			}
		}
	}
	return out, nil
}
