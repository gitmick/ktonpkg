package scope

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kton.dev/plankton/core"
	"kton.dev/plankton/foton"
	pregistry "kton.dev/plankton/registry"
	"kton.dev/plankton/spectrum"
)

const testToolImage = "oci://docker.io/scinteco/jam-r@sha256:" +
	"1111111111111111111111111111111111111111111111111111111111111111"

// runner signs runs into a plankton registry, with their outputs under root, as an executor would.
type runner struct {
	t    *testing.T
	dir  string
	priv ed25519.PrivateKey
	pub  string // path of the hex public key
}

func newRunner(t *testing.T) *runner {
	t.Helper()
	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(nil)
	pf := filepath.Join(dir, "runner.pub")
	if err := os.WriteFile(pf, []byte(hex.EncodeToString(pub)), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"registry", "root"} {
		os.MkdirAll(filepath.Join(dir, d), 0o755)
	}
	return &runner{t: t, dir: dir, priv: priv, pub: pf}
}

func (r *runner) registry() string { return filepath.Join(r.dir, "registry") }
func (r *runner) root() string     { return filepath.Join(r.dir, "root") }

// run records one run: inputs and outputs by path, outputs written under root. Returns its id.
func (r *runner) run(cmd string, inputs, outputs map[string]string) string {
	r.t.Helper()
	spec := foton.Spec{Protocol: &foton.ProtocolSpec{Kind: "script", Descriptor: map[string]any{"cmd": cmd}}}
	for p, body := range inputs {
		spec.Inputs = append(spec.Inputs, foton.FileSpec{Path: p, Hash: core.HashBytes([]byte(body))})
	}
	for p, body := range outputs {
		spec.Outputs = append(spec.Outputs, foton.FileSpec{Path: p, Hash: core.HashBytes([]byte(body))})
		f := filepath.Join(r.root(), filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(f), 0o755)
		if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
	return r.sign(spec)
}

func (r *runner) sign(spec foton.Spec) string {
	r.t.Helper()
	env, _, err := foton.SignWith(spec, r.priv)
	if err != nil {
		r.t.Fatal(err)
	}
	reg, err := pregistry.Open(r.registry())
	if err != nil {
		r.t.Fatal(err)
	}
	id, _, err := reg.Add(env)
	if err != nil {
		r.t.Fatal(err)
	}
	return id
}

// toolSrc writes a jam-r-like source: two members (one reading data), a counter-case, a normaliser,
// three requirements (URS-3 verified by nobody), improve defaults and a declared licence.
func toolSrc(t *testing.T, edit func(m map[string]any), files map[string]string) string {
	t.Helper()
	src := t.TempDir()
	manifest := map[string]any{
		"name": "jam-r", "of": "R as set up for TQ", "image": testToolImage,
		"command": "Rscript\n<command-file>", "programs": []string{"Rscript"},
		"hidden": []map[string]string{{"name": "licence", "target": "/opt/tool/licence.lic"}},
		"members": []map[string]any{
			{"name": "lm", "file": "members/lm.R", "output": "lm.out", "verifies": []string{"URS-1"}},
			{"name": "fit", "file": "members/fit.R", "output": "fit.out", "inputs": []string{"members/data.csv"},
				"verifies": []string{"URS-1", "URS-4"}},
			{"name": "rng", "file": "members/rng.R", "output": "rng.out", "counter": true, "verifies": []string{"URS-2"}},
		},
		"normalizer": map[string]any{"id": "fences", "file": "normalizer/fences.R", "hole": "output",
			"output": "normalized.out", "rationale": "drops the timing lines"},
		"docs":    []string{"requirements"},
		"improve": map[string]any{"instance": "jam-r 4.4", "runServer": "default", "toolType": "R", "command": "Rscript"},
	}
	if edit != nil {
		edit(manifest)
	}
	raw, _ := json.MarshalIndent(manifest, "", "  ")
	all := map[string]string{
		"tool.json":             string(raw),
		"members/lm.R":          "print(coef(lm(dist ~ speed, cars)))\n",
		"members/fit.R":         "d <- read.csv('data.csv'); print(mean(d$x))\n",
		"members/data.csv":      "x\n1\n2\n",
		"members/rng.R":         "print(runif(1))\n",
		"normalizer/fences.R":   "writeLines(grep('^time', readLines(commandArgs(TRUE)[1]), invert=TRUE, value=TRUE), commandArgs(TRUE)[2])\n",
		"requirements/URS-1.md": "---\nkey: URS-1\n---\n# Linear models give R's coefficients\n",
		"requirements/URS-2.md": "---\nkey: URS-2\n---\n# Random numbers are not reproducible without a seed\n",
		"requirements/URS-3.md": "---\nkey: URS-3\n---\n# Nobody tests this yet\n",
		"requirements/URS-4.md": "---\nkey: URS-4\n---\n# Data files are read as given\n",
	}
	for k, v := range files {
		all[k] = v
	}
	for rel, body := range all {
		if body == "" {
			continue
		}
		p := filepath.Join(src, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return src
}

// referenceRuns measures the three members once each.
func referenceRuns(r *runner) ReferenceRuns {
	return ReferenceRuns{Registry: r.registry(), Root: r.root(), Pub: r.pub, ByMember: map[string]string{
		"lm":  r.run("Rscript lm.R", map[string]string{"lm.R": "x"}, map[string]string{"ref/lm/lm.out": "(Intercept) -17.58 speed 3.93\n"}),
		"fit": r.run("Rscript fit.R", map[string]string{"fit.R": "y"}, map[string]string{"ref/fit/fit.out": "1.5\n"}),
		"rng": r.run("Rscript rng.R", map[string]string{"rng.R": "z"}, map[string]string{"ref/rng/rng.out": "0.2875775\n"}),
	}}
}

func TestBuildTool_BuildsAQualifiableToolPackage(t *testing.T) {
	r := newRunner(t)
	dir := filepath.Join(t.TempDir(), "jam-r")
	p, rev, err := BuildTool(dir, "v1", toolSrc(t, nil, nil), referenceRuns(r), newKey(t), Options{Clock: fixedClock()})
	if err != nil {
		t.Fatal(err)
	}
	if probs := p.Verify(); len(probs) > 0 {
		t.Fatalf("Verify: %v", probs)
	}
	if probs := rev.ToolProblems(); len(probs) > 0 {
		t.Fatalf("ToolProblems: %v", probs)
	}
	sealed, _, err := p.Sealed()
	if err != nil || sealed.ID != rev.ID {
		t.Fatalf("the built revision must be the sealed one (%v)", err)
	}
	spec, err := rev.Spectrum()
	if err != nil || spec == nil || len(spec.Members) != 2 {
		t.Fatalf("spectrum %+v (%v): want lm and fit, not the counter-case", spec, err)
	}
	ray, _ := rev.Ray()
	norm, _ := ray.Foton("fences", nil, nil)
	if spec.Normalizer != norm.Protocol.Ref {
		t.Errorf("normalizer %s, want the fences step's ref %s", spec.Normalizer, norm.Protocol.Ref)
	}
	if id, err := spec.ID(); err != nil || !strings.HasPrefix(id, "sha256:") {
		t.Errorf("spectrum id %q (%v)", id, err)
	}
	if imp, err := rev.ImproveDefaults(); err != nil || imp == nil || imp.Instance != "jam-r 4.4" || imp.ToolType != "R" {
		t.Errorf("improve defaults %+v (%v)", imp, err)
	}
	if progs, _ := rev.Programs(); len(progs) != 1 || progs[0] != "Rscript" {
		t.Errorf("programs %v", progs)
	}
	if hid, _ := rev.Hidden(); len(hid) != 1 || hid[0].Target != "/opt/tool/licence.lic" {
		t.Errorf("hidden %v", hid)
	}
	call, err := rev.MemberInvocation("fit")
	if err != nil || strings.Join(call.Argv, " ") != "Rscript fit.R" || call.Inputs["data.csv"] == "" ||
		call.Output != "fit.out" || call.Image != testToolImage {
		t.Errorf("invocation %+v (%v)", call, err)
	}
	recs, err := rev.ReferenceRecords()
	if err != nil || len(recs) != 3 {
		t.Errorf("reference records: %d (%v), want the three runs", len(recs), err)
	}
	for _, st := range ray.Steps {
		if st.ID == "fit" && len(st.Verifies) != 2 {
			t.Errorf("fit verifies %v, want two requirement hashes", st.Verifies)
		}
	}
	user := t.TempDir()
	if in, err := p.Install("v1", filepath.Join(user, "plankton"), filepath.Join(user, "nekton")); err != nil || in.Claims == 0 {
		t.Fatalf("install %+v (%v)", in, err)
	}
}

// Coverage follows plankton's spectrum check: a fulfilled member passes its requirements, a failed
// one fails them, a counter-case alone is inapplicable, a requirement nobody verifies is untested.
func TestCoverage_FollowsTheSpectrumCheck(t *testing.T) {
	r := newRunner(t)
	runs := referenceRuns(r)
	_, rev, err := BuildTool(filepath.Join(t.TempDir(), "jam-r"), "v1", toolSrc(t, nil, nil), runs, newKey(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := rev.Coverage(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range before {
		if c.Outcome != EarlUntested {
			t.Errorf("%s before any check: %s", c.Key, c.Outcome)
		}
	}

	// A candidate environment: lm gives the reference bytes, fit gives other ones.
	cand := newRunner(t)
	reg, err := pregistry.Open(cand.registry())
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := rev.ReferenceRecords()
	for _, env := range recs {
		if _, _, err := reg.Add(env); err != nil {
			t.Fatal(err)
		}
	}
	lm := "(Intercept) -17.58 speed 3.93\n"
	cand.run("Rscript lm.R in candidate", map[string]string{"lm.R": "x"}, map[string]string{"c/lm.out": lm})
	cand.run("Rscript fit.R in candidate", map[string]string{"fit.R": "y"}, map[string]string{"c/fit.out": "1.6\n"})
	reg, _ = pregistry.Open(cand.registry())
	res, err := rev.SpectrumCheck(reg, map[string]string{
		"lm": core.HashBytes([]byte(lm)), "fit": core.HashBytes([]byte("1.6\n"))})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome() != spectrum.OutcomeFailed {
		t.Fatalf("outcome %s: %+v", res.Outcome(), res.Members)
	}
	cov, err := rev.Coverage(&res)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"URS-1": EarlFailed, "URS-2": EarlInapplicable, "URS-3": EarlUntested, "URS-4": EarlFailed}
	for _, c := range cov {
		if want[c.Key] != c.Outcome {
			t.Errorf("%s: %s, want %s (by %v)", c.Key, c.Outcome, want[c.Key], c.By)
		}
	}
	if len(cov) != 4 {
		t.Errorf("coverage has %d requirements, want 4", len(cov))
	}

	// With lm alone checked, URS-1 cannot be told: fit, which also verifies it, was not checked.
	res, _ = rev.SpectrumCheck(reg, map[string]string{"lm": core.HashBytes([]byte(lm))})
	cov, _ = rev.Coverage(&res)
	for _, c := range cov {
		if c.Key == "URS-1" && c.Outcome != EarlCantTell {
			t.Errorf("URS-1 with fit unchecked: %s", c.Outcome)
		}
	}
}

func TestBuildTool_Refuses(t *testing.T) {
	cases := map[string]struct {
		edit  func(m map[string]any)
		files map[string]string
		runs  func(ReferenceRuns) ReferenceRuns
		want  string
	}{
		"an image not pinned by digest": {edit: func(m map[string]any) { m["image"] = "oci://docker.io/r-base:4.4" }, want: "image"},
		"no programs":                   {edit: func(m map[string]any) { delete(m, "programs") }, want: "programs"},
		"an unknown field":              {edit: func(m map[string]any) { m["imgae"] = "x" }, want: "imgae"},
		"a requirement key nobody has": {edit: func(m map[string]any) {
			m["members"].([]map[string]any)[0]["verifies"] = []string{"URS-9"}
		}, want: "URS-9"},
		"a member without a measured run": {runs: func(r ReferenceRuns) ReferenceRuns {
			delete(r.ByMember, "fit")
			return r
		}, want: "fit"},
		"a run for no member": {runs: func(r ReferenceRuns) ReferenceRuns {
			r.ByMember["ghost"] = r.ByMember["lm"]
			return r
		}, want: "ghost"},
		"a carried licence": {files: map[string]string{"requirements/licence.lic": "SECRET"}, want: "hidden licence"},
		"a normaliser image not pinned": {edit: func(m map[string]any) {
			n := m["normalizer"].(map[string]any)
			n["image"], n["command"] = "oci://docker.io/library/busybox:latest", "sh\n<command-file>"
		}, want: "normalizer image"},
		"a normaliser image without a command": {edit: func(m map[string]any) {
			m["normalizer"].(map[string]any)["image"] = testToolImage
		}, want: "normalizer command"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			runs := referenceRuns(newRunner(t))
			if c.runs != nil {
				runs = c.runs(runs)
			}
			_, _, err := BuildTool(filepath.Join(t.TempDir(), "jam-r"), "v1", toolSrc(t, c.edit, c.files), runs, newKey(t), Options{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an error mentioning %q, got %v", c.want, err)
			}
			if testing.Verbose() {
				t.Log(err)
			}
		})
	}
}

// D2: when the image was built, the package carries the signed build record, and it must name the
// image's digest; a record about another image is refused.
func TestBuildTool_BuildRecordMustNameTheImage(t *testing.T) {
	r := newRunner(t)
	digest := testToolImage[strings.LastIndex(testToolImage, "@")+1:]
	record := func(out string) string {
		spec := foton.Spec{Protocol: &foton.ProtocolSpec{Kind: "script", Descriptor: map[string]any{"cmd": "nix build"}},
			Inputs:  []foton.FileSpec{{Path: "flake.nix", Hash: core.HashBytes([]byte("{ }\n"))}},
			Outputs: []foton.FileSpec{{Path: "image", Hash: out}}}
		env, _, err := foton.SignWith(spec, r.priv)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(env)
		return string(raw)
	}
	build := func(m map[string]any) {
		m["build"] = map[string]any{"flake": "flake.nix", "record": "build/record.json"}
	}
	files := map[string]string{"flake.nix": "{ }\n", "build/record.json": record(digest)}
	_, rev, err := BuildTool(filepath.Join(t.TempDir(), "jam-r"), "v1", toolSrc(t, build, files), referenceRuns(r), newKey(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if probs := rev.ToolProblems(); len(probs) > 0 {
		t.Fatalf("ToolProblems: %v", probs)
	}
	files["build/record.json"] = record(core.HashBytes([]byte("another image")))
	_, _, err = BuildTool(filepath.Join(t.TempDir(), "jam-r"), "v1", toolSrc(t, build, files), referenceRuns(r), newKey(t), Options{})
	if err == nil || !strings.Contains(err.Error(), "image digest") {
		t.Fatalf("want a refusal naming the image digest, got %v", err)
	}
}

// <output> in the frame is the member's output (nmfe75 <ctl> <lst>); a normaliser with its own image
// and command runs there, not under the tool's frame.
func TestBuildTool_OutputInTheFrameAndAnOwnNormalizer(t *testing.T) {
	busybox := "oci://docker.io/library/busybox@sha256:" + strings.Repeat("2", 64)
	src := toolSrc(t, func(m map[string]any) {
		m["command"] = "Rscript\n<command-file>\n<output>"
		n := m["normalizer"].(map[string]any)
		n["image"], n["command"] = busybox, "sh\n<command-file>"
	}, nil)
	_, r, err := BuildTool(filepath.Join(t.TempDir(), "jam-r"), "v1", src, referenceRuns(newRunner(t)), newKey(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	call, err := r.MemberInvocation("lm")
	if err != nil || strings.Join(call.Argv, " ") != "Rscript lm.R lm.out" {
		t.Fatalf("argv: %+v %v", call, err)
	}
	ray, _ := r.Ray()
	st := ray.Step("fences")
	if st == nil || st.Protocol.Image != busybox || st.Protocol.Command.Args != "sh\n<command-file>\noutput\nnormalized.out" {
		t.Fatalf("the normaliser step: %+v", st)
	}
}
