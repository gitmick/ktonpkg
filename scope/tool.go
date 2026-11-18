package scope

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gitmick/ktonpkg"
	"kton.dev/plankton/core"
	pregistry "kton.dev/plankton/registry"
	"kton.dev/plankton/spectrum"
)

// ToolSourceFile is the source manifest of a tool package, at the top of its source folder.
const ToolSourceFile = "tool.json"

// RunBuild is the kind of a run that built the tool's image (D2): its record says what it was built
// from, and one of its outputs is the image digest.
const RunBuild = "build"

// ToolSource is tool.json: what a tool package is built from. Unknown fields are refused, so a typo
// does not silently drop a part.
type ToolSource struct {
	Name string `json:"name"`
	Of   string `json:"of,omitempty"`
	// Image is where every step runs, pinned by digest: qualification is always against it (D2).
	Image string `json:"image"`
	// Command is the command frame as it runs in Image, args lines, e.g. "Rscript\n<command-file>".
	Command string `json:"command"`
	// Programs are the argv[0]s the tool serves (e.g. ["Rscript"], ["nmfe75"]): explicit, not parsed
	// from Command, so a backend binds the tool class per program from them.
	Programs []string `json:"programs"`
	// Hidden is configuration the tool needs but the package never carries (a licence): name and
	// where it is mounted. Its content is never in the package; ToolProblems refuses one that is.
	Hidden     []HiddenConfig   `json:"hidden,omitempty"`
	Members    []ToolMember     `json:"members"`
	Normalizer *ToolNormalizer  `json:"normalizer,omitempty"`
	Docs       []string         `json:"docs,omitempty"`
	Improve    *ImproveDefaults `json:"improve,omitempty"`
	Build      *ToolBuild       `json:"build,omitempty"`
}

// HiddenConfig is declared hidden configuration: a name and the mount target, never content.
type HiddenConfig struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

// ToolMember is one reference computation: a command file run in the image, writing Output.
type ToolMember struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	Output string `json:"output"`
	// Inputs are further files the member reads (a NONMEM model's data), staged beside File.
	Inputs []string `json:"inputs,omitempty"`
	// Verifies are requirement keys (frontmatter key); in the package they become hashes (D3).
	Verifies []string `json:"verifies,omitempty"`
	// Counter marks a counter-case: measured and carried, not a spectrum member.
	Counter bool `json:"counter,omitempty"`
}

// ToolNormalizer is the normaliser: a step with a hole for the output it normalises.
type ToolNormalizer struct {
	ID        string `json:"id"`
	File      string `json:"file"`
	Hole      string `json:"hole"`
	Output    string `json:"output"`
	Rationale string `json:"rationale,omitempty"`
}

// ImproveDefaults say how the improve backend sets the tool up (D1); other backends ignore them.
type ImproveDefaults struct {
	Instance  string `json:"instance"`
	RunServer string `json:"runServer,omitempty"`
	ToolType  string `json:"toolType"`
	Command   string `json:"command,omitempty"`
}

// ToolBuild is how the image was built, when it was (D2: optional). Record is a signed plankton
// record (DSSE envelope, JSON) of the build; one of its outputs is the image digest.
type ToolBuild struct {
	Flake  string `json:"flake,omitempty"`
	Record string `json:"record,omitempty"`
}

// ReferenceRuns are the measured runs of the members: a plankton registry, the folder their output
// paths are relative to, the signer's public key (hex file), and member name -> foton id.
type ReferenceRuns struct {
	Registry string
	Root     string
	Pub      string
	ByMember map[string]string
}

// RequirementCoverage is one requirement: its key and hash, the steps that verify it, and an EARL
// outcome (passed, failed, cantTell, inapplicable, untested).
type RequirementCoverage struct {
	Key     string   `json:"key"`
	Hash    string   `json:"hash"`
	Path    string   `json:"path"`
	By      []string `json:"by,omitempty"`
	Outcome string   `json:"outcome"`
}

// EARL outcomes.
const (
	EarlPassed       = "passed"
	EarlFailed       = "failed"
	EarlCantTell     = "cantTell"
	EarlInapplicable = "inapplicable"
	EarlUntested     = "untested"
)

var pinnedImage = regexp.MustCompile(`^oci://[^@\s]+@sha256:[0-9a-f]{64}$`)

// LoadToolSource reads and checks <src>/tool.json.
func LoadToolSource(src string) (*ToolSource, error) {
	raw, err := os.ReadFile(filepath.Join(src, ToolSourceFile))
	if err != nil {
		return nil, err
	}
	s, err := decode[ToolSource](raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ToolSourceFile, err)
	}
	var probs []string
	if s.Name == "" {
		probs = append(probs, "no name")
	}
	if !pinnedImage.MatchString(s.Image) {
		probs = append(probs, fmt.Sprintf("image %q is not oci://<repo>@sha256:<hex>", s.Image))
	}
	if !strings.Contains(s.Command, "<command-file>") {
		probs = append(probs, "command does not name <command-file>")
	}
	if len(s.Members) == 0 {
		probs = append(probs, "no members")
	}
	if len(s.Programs) == 0 {
		probs = append(probs, "no programs (the argv[0]s the tool serves)")
	}
	for _, h := range s.Hidden {
		if h.Name == "" || !strings.HasPrefix(h.Target, "/") {
			probs = append(probs, fmt.Sprintf("hidden %q needs a name and an absolute target", h.Name))
		}
	}
	seen := map[string]bool{}
	for _, m := range s.Members {
		if m.Name == "" || m.File == "" || m.Output == "" {
			probs = append(probs, fmt.Sprintf("member %q needs name, file and output", m.Name))
		}
		if seen[m.Name] {
			probs = append(probs, fmt.Sprintf("member %q twice", m.Name))
		}
		seen[m.Name] = true
	}
	if n := s.Normalizer; n != nil {
		if n.ID == "" || n.File == "" || n.Hole == "" || n.Output == "" {
			probs = append(probs, "normalizer needs id, file, hole and output")
		}
		if seen[n.ID] {
			probs = append(probs, fmt.Sprintf("normalizer id %q is also a member", n.ID))
		}
	}
	if s.Improve != nil && (s.Improve.Instance == "" || s.Improve.ToolType == "") {
		probs = append(probs, "improve needs instance and toolType")
	}
	if len(probs) > 0 {
		return nil, fmt.Errorf("%s: %s", ToolSourceFile, strings.Join(probs, "; "))
	}
	return &s, nil
}

// BuildTool builds a tool package (profile tool) from src, or a new revision of the package at dir.
// Each member becomes a step in the image with its requirement hashes; each member's measured run
// is added as a reference run, and the spectrum names their outputs. The revision is sealed only if
// ToolProblems finds nothing; otherwise nothing is sealed and the problems are the error.
func BuildTool(dir, label, src string, runs ReferenceRuns, key ed25519.PrivateKey, opt Options) (*Package, *Revision, error) {
	s, err := LoadToolSource(src)
	if err != nil {
		return nil, nil, err
	}
	reqs, err := sourceRequirements(src, s.Docs)
	if err != nil {
		return nil, nil, err
	}
	read := func(rel string) ([]byte, error) { return os.ReadFile(filepath.Join(src, filepath.FromSlash(rel))) }

	// Everything that can be refused is checked before anything is written.
	ray := &ktonpkg.Ray{Name: s.Name}
	bodies := map[string][]byte{}
	for _, m := range s.Members {
		body, err := read(m.File)
		if err != nil {
			return nil, nil, fmt.Errorf("member %s: %w", m.Name, err)
		}
		h := core.HashBytes(body)
		bodies[h] = body
		var verifies []string
		for _, k := range m.Verifies {
			r, ok := reqs[k]
			if !ok {
				return nil, nil, fmt.Errorf("member %s verifies %q, which no requirement in %v has as its key", m.Name, k, s.Docs)
			}
			verifies = append(verifies, r.Hash)
		}
		inputs := []ktonpkg.Input{{Slot: path.Base(m.File), Path: m.File, Hash: h, Variable: "command-file"}}
		for _, in := range m.Inputs {
			body, err := read(in)
			if err != nil {
				return nil, nil, fmt.Errorf("member %s: %w", m.Name, err)
			}
			ih := core.HashBytes(body)
			bodies[ih] = body
			inputs = append(inputs, ktonpkg.Input{Slot: path.Base(in), Path: in, Hash: ih})
		}
		ray.Steps = append(ray.Steps, ktonpkg.Step{ID: m.Name, Label: "Member " + m.Name,
			Protocol: ktonpkg.Protocol{Image: s.Image, Command: &ktonpkg.Command{Args: s.Command}},
			Inputs:   inputs, Outputs: []string{m.Output}, Verifies: verifies})
	}
	if n := s.Normalizer; n != nil {
		body, err := read(n.File)
		if err != nil {
			return nil, nil, fmt.Errorf("normalizer: %w", err)
		}
		h := core.HashBytes(body)
		bodies[h] = body
		ray.Steps = append(ray.Steps, ktonpkg.Step{ID: n.ID, Label: "Normalizer", Rationale: n.Rationale,
			Protocol: ktonpkg.Protocol{Image: s.Image, Command: &ktonpkg.Command{Args: s.Command + "\n" + n.Hole + "\n" + n.Output}},
			Inputs: []ktonpkg.Input{{Slot: path.Base(n.File), Path: n.File, Hash: h, Variable: "command-file"},
				{Slot: n.Hole, From: ktonpkg.FromHole + n.Hole}},
			Outputs: []string{n.Output}})
		ray.Holes = append(ray.Holes, ktonpkg.Hole{Name: n.Hole, Step: n.ID, Slot: n.Hole, Type: ktonpkg.HoleFile,
			Required: true, Description: "the output to normalise"})
	}
	measured, err := loadRuns(s, runs)
	if err != nil {
		return nil, nil, err
	}
	var build *measuredRun
	if s.Build != nil && s.Build.Record != "" {
		if build, err = loadBuildRecord(filepath.Join(src, filepath.FromSlash(s.Build.Record)), runs.Pub, s.Image); err != nil {
			return nil, nil, err
		}
	}

	p, err := createOrOpen(dir, s.Name, label, key, opt)
	if err != nil {
		return nil, nil, err
	}
	r, err := p.NewRevision(label)
	if err != nil {
		return nil, nil, err
	}
	if err := r.SetProfile(ProfileTool); err != nil {
		return nil, nil, err
	}
	fetch := func(h string) ([]byte, error) {
		if b, ok := bodies[h]; ok {
			return b, nil
		}
		return nil, fmt.Errorf("no bytes for %s", h)
	}
	if _, err := r.AddRay(ray, fetch, nil); err != nil {
		return nil, nil, err
	}
	if _, err := r.Claim(r.ID, Image, map[string]any{"value": s.Image}); err != nil {
		return nil, nil, err
	}
	for _, d := range s.Docs {
		if err := addDocs(r, src, d); err != nil {
			return nil, nil, err
		}
	}
	spec := ktonpkg.Spectrum{Spectrum: s.Name, Of: s.Of}
	for _, m := range s.Members {
		run, ok := measured[m.Name]
		if !ok {
			continue // a counter-case without a run (loadRuns refused a member without one)
		}
		if _, err := r.AddRun(run.env, run.pub, RunReference, map[string]any{"step": m.Name, "member": m.Name,
			"counter": m.Counter}, run.outputs); err != nil {
			return nil, nil, fmt.Errorf("member %s: %w", m.Name, err)
		}
		if !m.Counter {
			spec.Members = append(spec.Members, ktonpkg.SpectrumMember{Name: m.Name, Output: run.output})
		}
	}
	if s.Normalizer != nil {
		f, err := ray.Foton(s.Normalizer.ID, nil, nil)
		if err != nil {
			return nil, nil, err
		}
		spec.Normalizer = f.Protocol.Ref
	}
	if err := addJSON(r, RoleSpectrum, spec, map[string]any{"spectrum": mustID(spec)}); err != nil {
		return nil, nil, err
	}
	if err := addJSON(r, RoleTool, toolFacts{Programs: s.Programs, Hidden: s.Hidden, Command: s.Command},
		map[string]any{"name": "tool.json"}); err != nil {
		return nil, nil, err
	}
	if s.Improve != nil {
		if err := addJSON(r, RoleInstance, s.Improve, map[string]any{"name": "improve.json"}); err != nil {
			return nil, nil, err
		}
	}
	if s.Build != nil && s.Build.Flake != "" {
		for _, f := range []string{s.Build.Flake, strings.TrimSuffix(s.Build.Flake, ".nix") + ".lock"} {
			raw, err := read(f)
			if err != nil {
				if f == s.Build.Flake {
					return nil, nil, fmt.Errorf("build: %w", err)
				}
				continue
			}
			h, err := p.PutBlob(raw)
			if err != nil {
				return nil, nil, err
			}
			if _, err := r.Claim(h, PartOf, member(RoleBuild, map[string]any{"name": path.Base(f)})); err != nil {
				return nil, nil, err
			}
		}
	}
	if build != nil {
		// Not AddRun: the image is an output the package names but does not carry.
		if err := p.AddKey(build.pub); err != nil {
			return nil, nil, err
		}
		id, err := p.AddFoton(build.env)
		if err != nil {
			return nil, nil, fmt.Errorf("build record: %w", err)
		}
		if _, err := r.Claim(id, PartOf, member(RoleRun, map[string]any{"kind": RunBuild, "image": s.Image})); err != nil {
			return nil, nil, err
		}
	}
	if probs := r.ToolProblems(); len(probs) > 0 {
		return nil, nil, fmt.Errorf("not sealed: %s", strings.Join(probs, "; "))
	}
	if _, err := r.Seal(); err != nil {
		return nil, nil, err
	}
	return p, r, nil
}

// RoleTool is the part saying how the tool is invoked: its programs, hidden configuration, frame.
const RoleTool = "tool"

type toolFacts struct {
	Programs []string       `json:"programs"`
	Hidden   []HiddenConfig `json:"hidden,omitempty"`
	Command  string         `json:"command"`
}

// Programs are the argv[0]s the tool serves.
func (r *Revision) Programs() ([]string, error) {
	var f toolFacts
	_, err := r.jsonPart(RoleTool, &f)
	return f.Programs, err
}

// Hidden is the declared hidden configuration: names and mount targets, never content.
func (r *Revision) Hidden() ([]HiddenConfig, error) {
	var f toolFacts
	_, err := r.jsonPart(RoleTool, &f)
	return f.Hidden, err
}

// MemberCall is how one member runs: the argv with the command frame rendered (each <command-file>
// replaced by its slot), the files staged beside it (slot -> hash) and the output it writes.
type MemberCall struct {
	Argv   []string          `json:"argv"`
	Inputs map[string]string `json:"inputs"`
	Output string            `json:"output"`
	Image  string            `json:"image"`
}

// MemberInvocation renders the command frame for one member, so every backend reads it one way.
func (r *Revision) MemberInvocation(name string) (*MemberCall, error) {
	ray, err := r.Ray()
	if err != nil {
		return nil, err
	}
	st := ray.Step(name)
	if st == nil {
		return nil, fmt.Errorf("no member %q", name)
	}
	inv := &MemberCall{Inputs: map[string]string{}, Image: st.Protocol.Image}
	vars := map[string]string{}
	for _, in := range st.Inputs {
		inv.Inputs[in.Slot] = in.Hash
		if in.Variable != "" {
			vars[in.Variable] = in.Slot
		}
	}
	if st.Protocol.Command != nil {
		for _, l := range strings.Split(st.Protocol.Command.Args, "\n") {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			if strings.HasPrefix(l, "<") && strings.HasSuffix(l, ">") {
				if v, ok := vars[l[1:len(l)-1]]; ok {
					l = v
				}
			}
			inv.Argv = append(inv.Argv, l)
		}
	}
	if len(st.Outputs) > 0 {
		inv.Output = st.Outputs[0]
	}
	return inv, nil
}

// ReferenceRecords are the signed records of the revision's reference runs, for a backend to add to
// its own registry before SpectrumCheck: plankton counts a reference only if a recorded run made it.
func (r *Revision) ReferenceRecords() ([]core.Envelope, error) {
	runs, err := r.referenceRuns()
	if err != nil {
		return nil, err
	}
	reg, err := pregistry.Open(filepath.Join(r.P.Dir, PlanktonDir))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(runs))
	for id := range runs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []core.Envelope
	for _, id := range ids {
		env, ok := reg.Envelope(id)
		if !ok {
			return nil, fmt.Errorf("reference run %s is not in the package", short(id))
		}
		out = append(out, env)
	}
	return out, nil
}

type measuredRun struct {
	env     core.Envelope
	pub     ed25519.PublicKey
	outputs map[string][]byte
	output  string // the member's output hash
}

// loadRuns reads each member's run from the registry and the bytes of its outputs. A spectrum member
// without a run is refused: its reference would be bytes nobody measured.
func loadRuns(s *ToolSource, runs ReferenceRuns) (map[string]*measuredRun, error) {
	out := map[string]*measuredRun{}
	if len(runs.ByMember) == 0 {
		return nil, fmt.Errorf("no reference runs: a tool package's references are measured runs")
	}
	pub, err := readPub(runs.Pub)
	if err != nil {
		return nil, err
	}
	reg, err := pregistry.Open(runs.Registry)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, m := range s.Members {
		known[m.Name] = true
		id, ok := runs.ByMember[m.Name]
		if !ok {
			if m.Counter {
				continue
			}
			return nil, fmt.Errorf("member %s has no reference run", m.Name)
		}
		env, ok := reg.Envelope(id)
		if !ok {
			return nil, fmt.Errorf("member %s: run %s is not in %s", m.Name, short(id), runs.Registry)
		}
		f, _ := reg.Foton(id)
		run := &measuredRun{env: env, pub: pub, outputs: map[string][]byte{}}
		for _, o := range f.Outputs {
			body, err := os.ReadFile(filepath.Join(runs.Root, filepath.FromSlash(o.Path)))
			if err != nil {
				return nil, fmt.Errorf("member %s: %w", m.Name, err)
			}
			run.outputs[o.Path] = body
			if path.Base(o.Path) == path.Base(m.Output) {
				run.output = o.Hash
			}
		}
		if run.output == "" {
			return nil, fmt.Errorf("member %s: run %s has no output %s", m.Name, short(id), m.Output)
		}
		out[m.Name] = run
	}
	for name := range runs.ByMember {
		if !known[name] {
			return nil, fmt.Errorf("a reference run names %q, which is no member", name)
		}
	}
	return out, nil
}

// loadBuildRecord reads the signed record of the image build; one of its outputs must be the image's
// digest, or the provenance would belong to another image.
func loadBuildRecord(file, pubFile, image string) (*measuredRun, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("build record: %w", err)
	}
	var env core.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("build record: %w", err)
	}
	pub, err := readPub(pubFile)
	if err != nil {
		return nil, err
	}
	st, err := env.Statement()
	if err != nil {
		return nil, fmt.Errorf("build record: %w", err)
	}
	f, err := st.ToFoton()
	if err != nil {
		return nil, fmt.Errorf("build record: %w", err)
	}
	if core.VerifiedSignerKeyID(env, []ed25519.PublicKey{pub}) == "" {
		return nil, fmt.Errorf("build record: not signed with %s", pubFile)
	}
	digest := image[strings.LastIndex(image, "@")+1:]
	for _, o := range f.Outputs {
		if o.Hash == digest {
			// The image is not in the package: its output is named, not carried.
			return &measuredRun{env: env, pub: pub, outputs: map[string][]byte{}}, nil
		}
	}
	return nil, fmt.Errorf("build record: no output is the image digest %s", short(digest))
}

func readPub(file string) (ed25519.PublicKey, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	b, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%s is no ed25519 public key in hex", file)
	}
	return ed25519.PublicKey(b), nil
}

// sourceRequirements are the requirements in the source's docs folders, by key.
func sourceRequirements(src string, docs []string) (map[string]ktonpkg.Anforderung, error) {
	b := ktonpkg.New(src, ktonpkg.KindTool, "source")
	all, err := b.Anforderungen()
	if err != nil {
		return nil, err
	}
	in := map[string]bool{}
	for _, d := range docs {
		in[path.Clean(d)] = true
	}
	out := map[string]ktonpkg.Anforderung{}
	for _, a := range all {
		if in[strings.SplitN(a.Path, "/", 2)[0]] && !a.Sammeldokument {
			out[a.Key] = a
		}
	}
	return out, nil
}

func addDocs(r *Revision, src, dir string) error {
	root := filepath.Join(src, filepath.FromSlash(dir))
	files, err := readTree(root)
	if err != nil {
		return fmt.Errorf("docs %s: %w", dir, err)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		h, err := r.P.PutBlob(files[n])
		if err != nil {
			return err
		}
		if _, err := r.Claim(h, PartOf, member(RoleDocument, map[string]any{"path": path.Join(path.Clean(dir), n)})); err != nil {
			return err
		}
	}
	return nil
}

func addJSON(r *Revision, role string, v any, extra map[string]any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	h, err := r.P.PutBlob(append(raw, '\n'))
	if err != nil {
		return err
	}
	_, err = r.Claim(h, PartOf, member(role, extra))
	return err
}

func mustID(s ktonpkg.Spectrum) string {
	id, err := s.ID()
	if err != nil {
		panic(err)
	}
	return id
}

// partsOf are the revision's isPartOf claims of one role: subject (the part's hash or id) -> object.
func (r *Revision) partsOf(role string) (map[string]map[string]any, error) {
	claims, err := r.Claims()
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for _, c := range claims {
		if v := value(c); c.Predicate == PartOf && str(v["role"]) == role {
			out[c.Subject] = v
		}
	}
	return out, nil
}

func (r *Revision) jsonPart(role string, v any) (bool, error) {
	parts, err := r.partsOf(role)
	if err != nil || len(parts) == 0 {
		return false, err
	}
	for h := range parts {
		body, err := r.P.Blob(h)
		if err != nil {
			return false, err
		}
		d := json.NewDecoder(bytes.NewReader(body))
		if err := d.Decode(v); err != nil {
			return false, fmt.Errorf("%s %s: %w", role, short(h), err)
		}
		return true, nil
	}
	return false, nil
}

// Spectrum is the revision's spectrum, nil if it has none. Its ID() (the hash of its canonical form,
// not the hash of the stored bytes) is the tool class: what goes into a run's descriptor.environment.
func (r *Revision) Spectrum() (*ktonpkg.Spectrum, error) {
	var s ktonpkg.Spectrum
	ok, err := r.jsonPart(RoleSpectrum, &s)
	if !ok || err != nil {
		return nil, err
	}
	return &s, nil
}

// ImproveDefaults are how the improve backend sets the tool up (D1), nil if the package says nothing.
func (r *Revision) ImproveDefaults() (*ImproveDefaults, error) {
	parts, err := r.partsOf(RoleInstance)
	if err != nil {
		return nil, err
	}
	for h, o := range parts {
		if str(o["name"]) != "improve.json" {
			continue // the old improve/tool-instance.json of FromBundle has another shape
		}
		body, err := r.P.Blob(h)
		if err != nil {
			return nil, err
		}
		d, err := decode[ImproveDefaults](body)
		if err != nil {
			return nil, err
		}
		return &d, nil
	}
	return nil, nil
}

// referenceRuns are the revision's runs of kind reference: foton id -> member name, counter flag.
func (r *Revision) referenceRuns() (map[string]map[string]any, error) {
	runs, err := r.partsOf(RoleRun)
	if err != nil {
		return nil, err
	}
	for id, o := range runs {
		if str(o["kind"]) != RunReference {
			delete(runs, id)
		}
	}
	return runs, nil
}

// ToolProblems checks a tool revision beyond its structure: every step runs in an image pinned by
// digest; there is a spectrum with members; each member's reference output is an output of a
// reference run of this revision (measured, not laid in); the normaliser is a step of the ray; every
// requirement a step verifies is a document of this revision; and a build run, if any, names the
// image's digest. An empty answer means the revision is a qualifiable tool package.
func (r *Revision) ToolProblems() []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if prof, _ := r.Profile(); prof != ProfileTool {
		add("profile is %q, not %s", prof, ProfileTool)
	}
	ray, err := r.Ray()
	if err != nil {
		return append(out, "ray: "+err.Error())
	}
	for _, st := range ray.Steps {
		if !pinnedImage.MatchString(st.Protocol.Image) {
			add("step %s: image %q is not pinned by digest", st.ID, st.Protocol.Image)
		}
	}
	spec, err := r.Spectrum()
	switch {
	case err != nil:
		add("spectrum: %v", err)
	case spec == nil:
		add("no spectrum")
	case len(spec.Members) == 0:
		add("the spectrum has no members: a tool without reference computations qualifies nothing")
	}
	runs, err := r.referenceRuns()
	if err != nil {
		return append(out, err.Error())
	}
	reg, err := pregistry.Open(filepath.Join(r.P.Dir, PlanktonDir))
	if err != nil {
		return append(out, err.Error())
	}
	measured := map[string]string{} // output hash -> member
	for id, o := range runs {
		f, ok := reg.Foton(id)
		if !ok {
			add("reference run %s is not in the package", short(id))
			continue
		}
		for _, x := range f.Outputs {
			measured[x.Hash] = str(o["member"])
		}
	}
	if spec != nil {
		for _, m := range spec.Members {
			if !ktonpkg.IsContentHash(m.Output) {
				add("member %s: reference %q is no content hash", m.Name, m.Output)
			} else if measured[m.Output] == "" {
				add("member %s: reference %s is the output of no reference run in the package", m.Name, short(m.Output))
			}
		}
		if spec.Normalizer != "" {
			found := false
			for _, st := range ray.Steps {
				if f, err := ray.Foton(st.ID, nil, nil); err == nil && f.Protocol.Ref == spec.Normalizer {
					found = true
				}
			}
			if !found {
				add("the normalizer %s is no step of the ray", short(spec.Normalizer))
			}
		}
	}
	docs, err := r.partsOf(RoleDocument)
	if err != nil {
		return append(out, err.Error())
	}
	for _, st := range ray.Steps {
		for _, v := range st.Verifies {
			if _, ok := docs[v]; !ok {
				add("step %s verifies %s, which is no requirement of this revision", st.ID, short(v))
			}
		}
	}
	hidden, _ := r.Hidden()
	if len(hidden) > 0 {
		var paths []string
		for _, role := range []string{RoleDocument, RoleBuild, RoleInstance, RoleReference} {
			parts, _ := r.partsOf(role)
			for _, o := range parts {
				paths = append(paths, str(o["path"]), str(o["name"]))
			}
		}
		for _, st := range ray.Steps {
			for _, in := range st.Inputs {
				paths = append(paths, in.Slot, in.Path)
			}
		}
		for _, h := range hidden {
			for _, p := range paths {
				if p != "" && (path.Base(p) == path.Base(h.Target) || path.Base(p) == h.Name) {
					add("hidden %s: the package carries %s; hidden configuration is declared, never carried", h.Name, p)
				}
			}
		}
	}
	builds, _ := r.partsOf(RoleRun)
	for id, o := range builds {
		if str(o["kind"]) != RunBuild {
			continue
		}
		img := str(o["image"])
		digest := img[strings.LastIndex(img, "@")+1:]
		f, ok := reg.Foton(id)
		hit := false
		for _, x := range f.Outputs {
			hit = hit || x.Hash == digest
		}
		if !ok || !hit {
			add("build run %s does not name the image digest %s", short(id), short(digest))
		}
		if len(ray.Steps) > 0 && ray.Steps[0].Protocol.Image != img {
			add("build run %s is about %s, the steps run in %s", short(id), img, ray.Steps[0].Protocol.Image)
		}
	}
	sort.Strings(out)
	return out
}

// Requirements are the revision's requirements with a frontmatter key: key -> (hash, path).
func (r *Revision) Requirements() ([]RequirementCoverage, error) {
	docs, err := r.partsOf(RoleDocument)
	if err != nil {
		return nil, err
	}
	var out []RequirementCoverage
	for h, o := range docs {
		p := str(o["path"])
		if !strings.HasSuffix(p, ".md") {
			continue
		}
		body, err := r.P.Blob(h)
		if err != nil {
			return nil, err
		}
		if key := frontmatterKey(body); key != "" {
			out = append(out, RequirementCoverage{Key: key, Hash: h, Path: p})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Coverage says for each requirement which steps verify it and, given a spectrum check, its outcome:
// passed when every member that verifies it was fulfilled, failed when one was not, cantTell when one
// was not checked or could not be compared, inapplicable when only counter-cases name it, untested
// when no step names it or there is no result.
func (r *Revision) Coverage(result *spectrum.Result) ([]RequirementCoverage, error) {
	reqs, err := r.Requirements()
	if err != nil {
		return nil, err
	}
	ray, err := r.Ray()
	if err != nil {
		return nil, err
	}
	runs, err := r.referenceRuns()
	if err != nil {
		return nil, err
	}
	counter := map[string]bool{}
	for _, o := range runs {
		if b, _ := o["counter"].(bool); b {
			counter[str(o["member"])] = true
		}
	}
	status := map[string]spectrum.Status{}
	if result != nil {
		for _, m := range result.Members {
			status[m.Name] = m.Status
		}
	}
	for i := range reqs {
		c := &reqs[i]
		for _, st := range ray.Steps {
			for _, v := range st.Verifies {
				if v == c.Hash {
					c.By = append(c.By, st.ID)
				}
			}
		}
		c.Outcome = outcome(c.By, counter, status, result != nil)
	}
	return reqs, nil
}

func outcome(by []string, counter map[string]bool, status map[string]spectrum.Status, checked bool) string {
	if len(by) == 0 || !checked {
		return EarlUntested
	}
	result, members := EarlPassed, 0
	for _, s := range by {
		if counter[s] {
			continue
		}
		members++
		switch st := status[s]; {
		case st.Fulfilled():
		case st == spectrum.NotFulfilled:
			return EarlFailed
		default:
			result = EarlCantTell
		}
	}
	if members == 0 {
		return EarlInapplicable
	}
	return result
}

// SpectrumCheck is plankton's spectrum.Check over the revision's spectrum: candidates are member
// name -> candidate output hash, reg holds the reference and candidate runs.
func (r *Revision) SpectrumCheck(reg *pregistry.Registry, candidates map[string]string) (spectrum.Result, error) {
	s, err := r.Spectrum()
	if err != nil {
		return spectrum.Result{}, err
	}
	if s == nil {
		return spectrum.Result{}, fmt.Errorf("revision %s has no spectrum", r.Label)
	}
	ps := spectrum.Spectrum{Spectrum: s.Spectrum, Of: s.Of, Normalizer: s.Normalizer}
	for _, m := range s.Members {
		ps.Members = append(ps.Members, spectrum.Member{Name: m.Name, Output: m.Output, Model: m.Model, Data: m.Data})
	}
	return spectrum.Check(reg, ps, candidates)
}

// frontmatterKey is the key: line of a YAML frontmatter, "" without one.
func frontmatterKey(doc []byte) string {
	s := string(doc)
	if !strings.HasPrefix(s, "---\n") {
		return ""
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return ""
	}
	for _, l := range strings.Split(s[4:4+end], "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "key" {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}
