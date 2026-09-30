package ktonpkg

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// Aus Ausführungen ein Potential.
//
// Die übliche Richtung ist Potential → Realisierung: ein Ray mit Löchern wird gebunden und läuft.
// Hier geht es umgekehrt. Jemand hat Schritt für Schritt gerechnet, und daraus soll etwas werden,
// das ein anderer auf seinen eigenen Daten laufen lassen kann.
//
// Zwei Hälften, und die Trennung ist der Punkt (kton-cockpit ADR-006):
//
//   - Propose sieht nach und schlägt vor. Es entscheidet nichts: welche Eingaben von außen kamen,
//     welche Werte zwischen Läufen desselben Codes verschieden waren — das sind Beobachtungen.
//   - Extract setzt um, was ein Mensch ausgewählt hat. Nichts wird zum Loch und nichts zum
//     Parameter, weil eine Regel es vermutet hat; was niemand wählt, bleibt fest.
//
// Beides arbeitet auf backendneutralen Ausführungen. Wie ein Cockpit-Foton oder ein improve-Step
// dazu wird, sagt ein Adapter auf der jeweiligen Seite.

// ExecFile ist eine Datei einer Ausführung, unter ihrem Slot: dem Namen relativ zu dem Ort, an dem
// der Befehl lief.
type ExecFile struct {
	Slot string `json:"slot"`
	Hash string `json:"hash"`
}

// Execution ist eine Ausführung, wie ein Adapter sie aus einem aufgezeichneten Foton liest.
type Execution struct {
	// ID ist die Foton-Id — die Herkunft, auf die das Paket später verweist.
	ID string `json:"id"`
	// Name ist ein Vorschlag des Adapters für den Step-Namen, etwa der Arbeitsordner. Leer ist
	// erlaubt.
	Name    string     `json:"name,omitempty"`
	Inputs  []ExecFile `json:"inputs"`
	Outputs []ExecFile `json:"outputs"`
	// Command ist die Befehlszeile, wie sie lief, in Wörtern, relativ zum Arbeitsort.
	Command []string `json:"command"`
	// CommandFile ist der Slot der Eingabe, die der Code ist. Der Adapter weiß das als Tatsache —
	// improves Variable command-file, oder ein Wort der Befehlszeile, das genau ein Eingabeslot ist —
	// und rät es nicht.
	CommandFile string `json:"commandFile,omitempty"`
	// Environment ist die Umgebung, wie sie aufgezeichnet ist (ein Image-Digest, eine Toolinstanz).
	Environment string `json:"environment,omitempty"`
	Label       string `json:"label,omitempty"`
	Rationale   string `json:"rationale,omitempty"`
}

// Proposal ist, was Propose gesehen hat.
type Proposal struct {
	Runs   []Run            `json:"runs"`
	Steps  []ProposedStep   `json:"steps"`
	Files  []FileCandidate  `json:"files"`
	Params []ParamCandidate `json:"params"`
	// Reproductions nennt Ausführungen, die dieselbe Berechnung mit denselben Bytes noch einmal
	// waren: gleicher Code, gleiche Befehlszeile, gleiche Ein- und Ausgaben. Sie sind kein eigener
	// Step; gezählt wird je Step eine, die kleinste Id, und die anderen stehen hier.
	Reproductions map[string][]string `json:"reproductions,omitempty"`
	// Conflicts nennt, was sich nicht zu einem Step fügt — eine Kante, die in einem Lauf besteht
	// und im anderen nicht, eine Befehlszeile anderer Länge. Mit Konflikten extrahiert Extract
	// nicht.
	Conflicts []string `json:"conflicts,omitempty"`
}

// Run ist eine Zusammenhangskomponente: Ausführungen, verbunden durch die Hashes, die zwischen
// ihnen flossen.
type Run struct {
	ID         string   `json:"id"`
	Executions []string `json:"executions"`
	// Endpoints sind die Ausgaben, die keine Ausführung desselben Laufs weiterverwendet hat.
	Endpoints []string `json:"endpoints"`
}

// ProposedStep ist eine Gruppe von Ausführungen über Läufe hinweg, die denselben Code und dieselbe
// Gestalt haben: denselben Hash der Kommandodatei, dieselben Ein- und Ausgabeslots.
type ProposedStep struct {
	ID string `json:"id"`
	// Executions ordnet je Lauf die Ausführung dieses Steps zu.
	Executions  map[string]string `json:"executions"`
	CommandFile string            `json:"commandFile,omitempty"`
	Inputs      []ProposedInput   `json:"inputs"`
	Outputs     []string          `json:"outputs"`
}

// ProposedInput ist ein Eingabeslot eines Steps. Upstream ist "<step>:<slot>", wenn die Bytes in
// jedem Lauf von diesem Step kamen, sonst leer — dann kam die Eingabe von außen und ist ein
// Dateikandidat.
type ProposedInput struct {
	Slot     string `json:"slot"`
	Upstream string `json:"upstream,omitempty"`
}

// FileCandidate ist eine Eingabe von außen. Sie wird zum Loch, wenn jemand sie so benennt, sonst
// reist sie als feste Datei mit.
type FileCandidate struct {
	Step string `json:"step"`
	Slot string `json:"slot"`
	// CommandFile sagt, dass diese Eingabe der Code des Steps ist.
	CommandFile bool `json:"commandFile,omitempty"`
	// Hashes je Lauf. Gleiche Hashes in allen Läufen heißen: hier hat niemand etwas anderes
	// hineingegeben.
	Hashes map[string]string `json:"hashes"`
}

// ParamCandidate ist eine Stelle der Befehlszeile, an der Läufe desselben Steps verschieden waren.
// Mit nur einem Lauf gibt es keine: eine Zahl in einer Befehlszeile ist kein Parameter, solange
// niemand das sagt.
type ParamCandidate struct {
	Step     string            `json:"step"`
	Position int               `json:"position"`
	Values   map[string]string `json:"values"`
}

// Propose ordnet Ausführungen zu Läufen und Steps und nennt die Kandidaten.
func Propose(execs []Execution) (*Proposal, error) {
	if len(execs) == 0 {
		return nil, fmt.Errorf("keine Ausführungen")
	}
	byID := map[string]*Execution{}
	for i := range execs {
		e := &execs[i]
		if e.ID == "" {
			return nil, fmt.Errorf("eine Ausführung ohne Foton-Id")
		}
		if byID[e.ID] != nil {
			return nil, fmt.Errorf("%s kommt zweimal vor", e.ID)
		}
		if e.CommandFile != "" && inputHash(e, e.CommandFile) == "" {
			return nil, fmt.Errorf("%s: die Kommandodatei %q ist keine Eingabe", e.ID, e.CommandFile)
		}
		byID[e.ID] = e
	}

	// Reproduktionen zusammenfassen, bevor irgendetwas gezählt wird: sonst verbindet ein zweiter
	// Erzeuger derselben Bytes zwei Läufe zu einem, und jeder Step kommt darin doppelt vor.
	p := &Proposal{}
	seenCalc := map[string]string{}
	for _, id := range sortedKeysExec(byID) {
		k := calculation(byID[id])
		if kept, dup := seenCalc[k]; dup {
			if p.Reproductions == nil {
				p.Reproductions = map[string][]string{}
			}
			p.Reproductions[kept] = append(p.Reproductions[kept], id)
			delete(byID, id)
			continue
		}
		seenCalc[k] = id
	}

	// Wer hat welchen Hash erzeugt. Mehrere Erzeuger desselben Hashes sind Reproduktionen; für die
	// Verdrahtung genügt einer, und genommen wird deterministisch der kleinste — aber nie der
	// Verbraucher selbst: ein Step, der seine Eingabe unverändert weitergibt, hat dieselben Bytes als
	// Ein- und Ausgabe, und aus sich selbst gespeist ist er trotzdem nicht.
	producer := producers(byID)

	runs := components(byID, producer)
	runOf := map[string]string{}
	for i, members := range runs {
		r := Run{ID: fmt.Sprintf("run-%d", i+1), Executions: members}
		consumed := map[string]bool{}
		for _, id := range members {
			for _, in := range byID[id].Inputs {
				consumed[in.Hash] = true
			}
		}
		seen := map[string]bool{}
		for _, id := range members {
			runOf[id] = r.ID
			for _, o := range byID[id].Outputs {
				if !consumed[o.Hash] && !seen[o.Hash] {
					r.Endpoints = append(r.Endpoints, o.Hash)
					seen[o.Hash] = true
				}
			}
		}
		sort.Strings(r.Endpoints)
		p.Runs = append(p.Runs, r)
	}

	// Steps: gleiche Gestalt über Läufe hinweg. Kommt dieselbe Gestalt in einem Lauf mehrfach vor,
	// sind das verschiedene Steps, unterschieden nach ihrer Stellung im Lauf.
	type group struct {
		key   string
		execs map[string]string
		first *Execution
	}
	var groups []*group
	byKey := map[string]*group{}
	for _, r := range p.Runs {
		occurrence := map[string]int{}
		for _, id := range topo(r.Executions, byID, producer) {
			e := byID[id]
			k := shape(e)
			occurrence[k]++
			k = fmt.Sprintf("%s#%d", k, occurrence[k])
			g := byKey[k]
			if g == nil {
				g = &group{key: k, execs: map[string]string{}, first: e}
				byKey[k] = g
				groups = append(groups, g)
			}
			g.execs[r.ID] = id
		}
	}

	stepOf := map[string]string{} // exec → step
	used := map[string]int{}
	for i, g := range groups {
		name := g.first.Name
		if name == "" {
			name = fmt.Sprintf("step-%d", i+1)
		}
		used[name]++
		if used[name] > 1 {
			name = fmt.Sprintf("%s-%d", name, used[name])
		}
		for _, id := range g.execs {
			stepOf[id] = name
		}
		p.Steps = append(p.Steps, ProposedStep{ID: name, Executions: g.execs, CommandFile: g.first.CommandFile})
	}

	for i := range p.Steps {
		s := &p.Steps[i]
		first := byID[s.Executions[sortedKeys(s.Executions)[0]]]
		for _, o := range first.Outputs {
			s.Outputs = append(s.Outputs, o.Slot)
		}
		sort.Strings(s.Outputs)

		for _, in := range sortedFiles(first.Inputs) {
			wiring := map[string]bool{}
			hashes := map[string]string{}
			for run, id := range s.Executions {
				h := inputHash(byID[id], in.Slot)
				hashes[run] = h
				up := ""
				if pid, ok := producer.of(h, id); ok && runOf[pid] == run {
					up = stepOf[pid] + ":" + outputSlot(byID[pid], h)
				}
				wiring[up] = true
			}
			if len(wiring) > 1 {
				p.Conflicts = append(p.Conflicts, fmt.Sprintf(
					"%s/%s: die Eingabe kommt nicht in jedem Lauf von derselben Stelle", s.ID, in.Slot))
				continue
			}
			var up string
			for w := range wiring {
				up = w
			}
			s.Inputs = append(s.Inputs, ProposedInput{Slot: in.Slot, Upstream: up})
			if up == "" {
				p.Files = append(p.Files, FileCandidate{Step: s.ID, Slot: in.Slot,
					CommandFile: in.Slot == s.CommandFile, Hashes: hashes})
			}
		}

		// Parameterkandidaten: nur wo Läufe desselben Steps an derselben Stelle verschieden sind.
		var n int
		for run, id := range s.Executions {
			if l := len(byID[id].Command); n == 0 {
				n = l
			} else if l != n {
				p.Conflicts = append(p.Conflicts, fmt.Sprintf(
					"%s: die Befehlszeile hat in %s eine andere Länge", s.ID, run))
				n = -1
				break
			}
		}
		for pos := 0; n > 0 && pos < n; pos++ {
			values := map[string]string{}
			distinct := map[string]bool{}
			for run, id := range s.Executions {
				v := byID[id].Command[pos]
				values[run] = v
				distinct[v] = true
			}
			if len(distinct) > 1 {
				p.Params = append(p.Params, ParamCandidate{Step: s.ID, Position: pos, Values: values})
			}
		}
	}
	p.Steps = canonicalOrder(p.Steps)
	return p, nil
}

// canonicalOrder bringt die Steps in eine Reihenfolge, die nur von ihnen selbst abhängt:
// topologisch, bei Gleichstand nach Name.
//
// Das ist Identität, nicht Ordnungsliebe. Ray.ID rechnet über Ray.Order, und die löst Gleichstände
// nach der Stellung im Array. Ohne das hier stünden zwei unabhängige Steps so, wie ihre Foton-Ids
// sortieren — und die unterscheiden sich zwischen Backends, weil das Protokoll mitgerechnet wird.
// Dieselben Berechnungen ergäben dann zwei Pakete. Ray.Order selbst bleibt, wie es ist: es zu
// ändern, verschöbe die Identität jedes vorhandenen Pakets.
func canonicalOrder(steps []ProposedStep) []ProposedStep {
	byID := map[string]ProposedStep{}
	deps := map[string]map[string]bool{}
	for _, s := range steps {
		byID[s.ID] = s
		deps[s.ID] = map[string]bool{}
		for _, in := range s.Inputs {
			if up, _, ok := strings.Cut(in.Upstream, ":"); ok && up != "" {
				deps[s.ID][up] = true
			}
		}
	}
	var names []string
	for id := range byID {
		names = append(names, id)
	}
	sort.Strings(names)
	var out []ProposedStep
	done := map[string]bool{}
	for len(out) < len(steps) {
		progressed := false
		for _, id := range names {
			if done[id] {
				continue
			}
			ready := true
			for d := range deps[id] {
				if !done[d] {
					ready = false
					break
				}
			}
			if ready {
				out = append(out, byID[id])
				done[id] = true
				progressed = true
				break // nach jedem Schritt wieder beim kleinsten Namen beginnen
			}
		}
		if !progressed { // ein Kreis — bei Fotons unmöglich, aber nicht hängen bleiben
			for _, id := range names {
				if !done[id] {
					out = append(out, byID[id])
					done[id] = true
				}
			}
		}
	}
	return out
}

// Choice ist die Auswahl eines Menschen.
type Choice struct {
	Name string `json:"name"`
	// Reference ist der Lauf, dessen Werte die Vorgaben der Löcher und dessen Ausgaben das Spectrum
	// werden.
	Reference string        `json:"reference"`
	Holes     []HoleChoice  `json:"holes,omitempty"`
	Params    []ParamChoice `json:"params,omitempty"`
	// Requires ist die Bedingung an das Werkzeug — ausdrücklich, wie alles andere hier.
	Requires []Requirement `json:"requires,omitempty"`
}

// HoleChoice macht einen Dateikandidaten zum Loch.
type HoleChoice struct {
	Name string `json:"name"`
	Step string `json:"step"`
	Slot string `json:"slot"`
}

// ParamChoice macht eine Stelle der Befehlszeile zum Parameter.
type ParamChoice struct {
	Name     string `json:"name"`
	Step     string `json:"step"`
	Position int    `json:"position"`
}

// Fetch liefert die Bytes zu einem Hash. Extract prüft, dass sie zum Hash passen: eine föderierte
// Ausführung darf nichts anderes ins Paket bringen, als ihr Foton sagt.
type Fetch func(hash string) ([]byte, error)

// Extract schreibt das Potential als Ray-Paket nach dir.
//
// Feste Eingaben liegen unter steps/<step>/<slot>, Testdaten unter testdaten/<loch>/<slot>, die
// Referenzausgaben unter reference/<step>-<slot>. Das Spectrum nennt die Ausgaben des
// Referenzlaufs; seine Member sind Ausgaben aufgezeichneter Fotons.
func Extract(p *Proposal, execs []Execution, c Choice, fetch Fetch, dir string) (*Bundle, error) {
	if len(p.Conflicts) > 0 {
		return nil, fmt.Errorf("der Vorschlag hat Konflikte:\n  %s", strings.Join(p.Conflicts, "\n  "))
	}
	if c.Name == "" {
		return nil, fmt.Errorf("das Paket braucht einen Namen")
	}
	if !hasRun(p, c.Reference) {
		return nil, fmt.Errorf("kein Lauf %q im Vorschlag", c.Reference)
	}
	byID := map[string]*Execution{}
	for i := range execs {
		byID[execs[i].ID] = &execs[i]
	}

	holeAt := map[string]string{} // step/slot → Lochname
	names := map[string]bool{}
	for _, h := range c.Holes {
		if names[h.Name] {
			return nil, fmt.Errorf("der Name %q ist doppelt vergeben", h.Name)
		}
		names[h.Name] = true
		if !isFileCandidate(p, h.Step, h.Slot) {
			return nil, fmt.Errorf("%s/%s ist keine Eingabe von außen und kann kein Loch werden", h.Step, h.Slot)
		}
		holeAt[h.Step+"/"+h.Slot] = h.Name
	}
	paramAt := map[string]string{} // step#pos → Parametername
	for _, pc := range c.Params {
		if names[pc.Name] {
			return nil, fmt.Errorf("der Name %q ist doppelt vergeben", pc.Name)
		}
		names[pc.Name] = true
		if !isParamCandidate(p, pc.Step, pc.Position) {
			return nil, fmt.Errorf("%s: an Stelle %d waren die Läufe nicht verschieden — "+
				"dort gibt es keinen Parameter zu wählen", pc.Step, pc.Position)
		}
		paramAt[fmt.Sprintf("%s#%d", pc.Step, pc.Position)] = pc.Name
	}

	get := func(hash string) ([]byte, error) {
		body, err := fetch(hash)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", hash, err)
		}
		if got := HashOf(body); got != hash {
			return nil, fmt.Errorf("die Bytes zu %s haben den Hash %s", hash, got)
		}
		return body, nil
	}

	b := New(dir, KindRay, c.Name)
	b.Requires = c.Requires
	// "of" nennt den Referenzlauf über seine Endpunkte, nicht über seinen Namen im Vorschlag: der
	// ("run-2") hängt davon ab, wie Foton-Ids sortieren, und das Spectrum zählt zur Identität des
	// Pakets. Dieselben Ausführungen, auf welchem Backend auch immer, ergeben dasselbe Paket.
	spec := &Spectrum{Spectrum: c.Name, Of: "extrahiert aus dem Lauf mit den Endpunkten " +
		strings.Join(runOf(p, c.Reference).Endpoints, ", ")}
	for _, ps := range p.Steps {
		id, ok := ps.Executions[c.Reference]
		if !ok {
			return nil, fmt.Errorf("der Step %s kommt im Referenzlauf %s nicht vor", ps.ID, c.Reference)
		}
		e := byID[id]
		if e == nil {
			return nil, fmt.Errorf("die Ausführung %s fehlt", id)
		}
		s := Step{ID: ps.ID, Label: e.Label, Rationale: e.Rationale, Outputs: ps.Outputs}

		for _, in := range ps.Inputs {
			switch {
			case in.Upstream != "":
				up, slot, _ := strings.Cut(in.Upstream, ":")
				s.Inputs = append(s.Inputs, Input{Slot: in.Slot, From: FromUpstream + up + ":" + slot})
			case holeAt[ps.ID+"/"+in.Slot] != "":
				name := holeAt[ps.ID+"/"+in.Slot]
				body, err := get(inputHash(e, in.Slot))
				if err != nil {
					return nil, err
				}
				def := path.Join("testdaten", name, in.Slot)
				if _, err := b.AddFile(def, body); err != nil {
					return nil, err
				}
				s.Inputs = append(s.Inputs, Input{Slot: in.Slot, From: FromHole + name})
				b.Ray.Holes = append(b.Ray.Holes, Hole{Name: name, Step: ps.ID, Slot: in.Slot,
					Type: HoleFile, Required: true, Default: def})
			default:
				body, err := get(inputHash(e, in.Slot))
				if err != nil {
					return nil, err
				}
				rel := path.Join("steps", ps.ID, in.Slot)
				h, err := b.AddFile(rel, body)
				if err != nil {
					return nil, err
				}
				s.Inputs = append(s.Inputs, Input{Slot: in.Slot, Path: rel, Hash: h})
			}
			if in.Slot == ps.CommandFile {
				s.Inputs[len(s.Inputs)-1].Variable = "command-file"
			}
		}

		args := make([]string, len(e.Command))
		for pos, tok := range e.Command {
			switch name := paramAt[fmt.Sprintf("%s#%d", ps.ID, pos)]; {
			case name != "":
				args[pos] = "<" + name + ">"
				if s.Params == nil {
					s.Params = map[string]string{}
				}
				s.Params[name] = FromHole + name
				b.Ray.Holes = append(b.Ray.Holes, Hole{Name: name, Step: ps.ID, Param: name,
					Type: HoleParam, Required: true, Default: tok})
			case tok == ps.CommandFile && tok != "":
				args[pos] = "<command-file>"
			default:
				args[pos] = tok
			}
		}
		s.Protocol = Protocol{Command: &Command{Args: strings.Join(args, "\n")}}
		b.Ray.Steps = append(b.Ray.Steps, s)

		for _, o := range sortedFiles(e.Outputs) {
			body, err := get(o.Hash)
			if err != nil {
				return nil, err
			}
			if _, err := b.AddFile(path.Join(ReferenceDir, ps.ID+"-"+o.Slot), body); err != nil {
				return nil, err
			}
			name := ps.ID
			if len(e.Outputs) > 1 {
				name = ps.ID + "/" + o.Slot
			}
			spec.Members = append(spec.Members, SpectrumMember{Name: name, Output: o.Hash})
		}
	}
	sort.Slice(b.Ray.Holes, func(i, j int) bool { return b.Ray.Holes[i].Name < b.Ray.Holes[j].Name })
	sort.Slice(spec.Members, func(i, j int) bool { return spec.Members[i].Name < spec.Members[j].Name })
	b.Spectrum = spec
	if err := b.Write(); err != nil {
		return nil, err
	}
	return b, nil
}

// DerivedFrom sind die Foton-Ids, aus denen das Paket extrahiert wurde — der Referenzlauf.
func DerivedFrom(p *Proposal, reference string) []string {
	var out []string
	for _, s := range p.Steps {
		if id, ok := s.Executions[reference]; ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// --- Hilfen ---------------------------------------------------------------------------------------

// shape ist die Gestalt einer Ausführung: Code und Slots. Zwei Ausführungen gleicher Gestalt sind
// derselbe Step in verschiedenen Läufen.
func shape(e *Execution) string {
	var ins, outs []string
	for _, f := range e.Inputs {
		ins = append(ins, f.Slot)
	}
	for _, f := range e.Outputs {
		outs = append(outs, f.Slot)
	}
	sort.Strings(ins)
	sort.Strings(outs)
	code := inputHash(e, e.CommandFile)
	if code == "" {
		code = "cmd:" + strings.Join(e.Command, " ")
	}
	return code + "|" + strings.Join(ins, ",") + "|" + strings.Join(outs, ",")
}

// calculation ist, was eine Ausführung gerechnet hat: Gestalt, Befehlszeile, Ein- und Ausgabebytes.
// Name und Arbeitsort gehören nicht dazu.
func calculation(e *Execution) string {
	var files []string
	for _, f := range e.Inputs {
		files = append(files, "in:"+f.Slot+"="+f.Hash)
	}
	for _, f := range e.Outputs {
		files = append(files, "out:"+f.Slot+"="+f.Hash)
	}
	sort.Strings(files)
	return shape(e) + "|" + strings.Join(e.Command, "\x00") + "|" + strings.Join(files, ",")
}

// components teilt die Ausführungen in Läufe: verbunden ist, was einen Hash weitergab.
func components(byID map[string]*Execution, producer producerIndex) [][]string {
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	for id := range byID {
		parent[id] = id
	}
	for _, e := range byID {
		for _, in := range e.Inputs {
			if pid, ok := producer.of(in.Hash, e.ID); ok {
				a, b := find(e.ID), find(pid)
				if a != b {
					if a < b {
						parent[b] = a
					} else {
						parent[a] = b
					}
				}
			}
		}
	}
	sets := map[string][]string{}
	for id := range byID {
		r := find(id)
		sets[r] = append(sets[r], id)
	}
	var out [][]string
	for _, s := range sets {
		sort.Strings(s)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// topo ordnet die Ausführungen eines Laufs so, dass jeder Erzeuger vor seinen Verbrauchern steht;
// bei Gleichstand nach Id.
func topo(ids []string, byID map[string]*Execution, producer producerIndex) []string {
	in := map[string]bool{}
	for _, id := range ids {
		in[id] = true
	}
	deps := map[string]map[string]bool{}
	for _, id := range ids {
		deps[id] = map[string]bool{}
		for _, f := range byID[id].Inputs {
			if pid, ok := producer.of(f.Hash, id); ok && in[pid] {
				deps[id][pid] = true
			}
		}
	}
	var out []string
	done := map[string]bool{}
	for len(out) < len(ids) {
		progressed := false
		for _, id := range ids { // ids sind sortiert
			if done[id] {
				continue
			}
			ready := true
			for d := range deps[id] {
				if !done[d] {
					ready = false
					break
				}
			}
			if ready {
				out = append(out, id)
				done[id] = true
				progressed = true
			}
		}
		if !progressed { // ein Kreis — bei Fotons unmöglich, aber nicht still hängen bleiben
			for _, id := range ids {
				if !done[id] {
					out = append(out, id)
					done[id] = true
				}
			}
		}
	}
	return out
}

// producerIndex hält je Hash seine Erzeuger, nach Id sortiert.
type producerIndex map[string][]string

func producers(byID map[string]*Execution) producerIndex {
	idx := producerIndex{}
	for _, id := range sortedKeysExec(byID) {
		for _, o := range byID[id].Outputs {
			idx[o.Hash] = append(idx[o.Hash], id)
		}
	}
	return idx
}

// of nennt den Erzeuger von hash, der nicht consumer ist.
func (idx producerIndex) of(hash, consumer string) (string, bool) {
	for _, id := range idx[hash] {
		if id != consumer {
			return id, true
		}
	}
	return "", false
}

func inputHash(e *Execution, slot string) string {
	for _, f := range e.Inputs {
		if f.Slot == slot {
			return f.Hash
		}
	}
	return ""
}

func outputSlot(e *Execution, hash string) string {
	for _, f := range e.Outputs {
		if f.Hash == hash {
			return f.Slot
		}
	}
	return ""
}

func sortedKeysExec(m map[string]*Execution) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedFiles(fs []ExecFile) []ExecFile {
	out := append([]ExecFile(nil), fs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

func runOf(p *Proposal, id string) Run {
	for _, r := range p.Runs {
		if r.ID == id {
			return r
		}
	}
	return Run{}
}

func hasRun(p *Proposal, id string) bool {
	for _, r := range p.Runs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func isFileCandidate(p *Proposal, step, slot string) bool {
	for _, f := range p.Files {
		if f.Step == step && f.Slot == slot {
			return true
		}
	}
	return false
}

func isParamCandidate(p *Proposal, step string, pos int) bool {
	for _, c := range p.Params {
		if c.Step == step && c.Position == pos {
			return true
		}
	}
	return false
}
