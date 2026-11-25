package nonmem_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gitmick/ktonpkg/scope"
	"github.com/gitmick/ktonpkg/spectra/nonmem"
	"kton.dev/plankton/core"
	"kton.dev/plankton/foton"
	pregistry "kton.dev/plankton/registry"
)

// The NONMEM tool source (tool/) builds a tool package whose normaliser is exactly spectra/nonmem's
// (the ref robert1's L1 rests on) and whose member runs nmfe75 <ctl> <lst>. The runs here are stand-ins
// signed by a test key; the real ones are measured in the image (tool/README.md).
func TestToolSource(t *testing.T) {
	src := "tool"
	if b, err := os.ReadFile(filepath.Join(src, nonmem.ScriptPath)); err != nil || !bytes.Equal(b, nonmem.Script) {
		t.Fatalf("tool/%s must be normalize-nonmem.sh byte for byte (%v)", nonmem.ScriptPath, err)
	}

	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(nil)
	pubFile := filepath.Join(dir, "runner.pub")
	os.WriteFile(pubFile, []byte(hex.EncodeToString(pub)), 0o644)
	reg := filepath.Join(dir, "registry")
	root := filepath.Join(dir, "root")
	os.MkdirAll(reg, 0o755)
	r, err := pregistry.Open(reg)
	if err != nil {
		t.Fatal(err)
	}
	run := func(member, out string) string {
		ctl, _ := os.ReadFile(filepath.Join(src, "members/theo-run1/run1.ctl"))
		data, _ := os.ReadFile(filepath.Join(src, "members/theo-run1/nm_data.csv"))
		body := []byte("stand-in " + out + "\n")
		p := filepath.Join(root, member, out)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, body, 0o644)
		spec := foton.Spec{Protocol: &foton.ProtocolSpec{Kind: "script", Descriptor: map[string]any{"cmd": "nmfe75 run1.ctl " + out}},
			Inputs:  []foton.FileSpec{{Path: "run1.ctl", Hash: core.HashBytes(ctl)}, {Path: "nm_data.csv", Hash: core.HashBytes(data)}},
			Outputs: []foton.FileSpec{{Path: member + "/" + out, Hash: core.HashBytes(body)}}}
		env, _, err := foton.SignWith(spec, priv)
		if err != nil {
			t.Fatal(err)
		}
		id, _, err := r.Add(env)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	runs := scope.ReferenceRuns{Registry: reg, Root: root, Pub: pubFile, ByMember: map[string]string{
		"theo-run1": run("theo-run1", "run1.lst")}}
	_, key, _ := ed25519.GenerateKey(nil)
	_, rev, err := scope.BuildTool(filepath.Join(dir, "pkg"), "v1", src, runs, key, scope.Options{})
	if err != nil {
		t.Fatal(err)
	}

	spec, err := rev.Spectrum()
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := nonmem.Ref()
	if spec.Normalizer != ref {
		t.Fatalf("the normaliser is %s, spectra/nonmem's is %s", spec.Normalizer, ref)
	}
	if spec.Spectrum != nonmem.Spectrum || len(spec.Members) != 1 {
		t.Fatalf("spectrum: %+v", spec)
	}
	call, err := rev.MemberInvocation("theo-run1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"nmfe75", "run1.ctl", "run1.lst"}; !reflect.DeepEqual(call.Argv, want) {
		t.Fatalf("argv %v, want %v", call.Argv, want)
	}
	if len(call.Inputs) != 2 || call.Image != "oci://docker.io/scinteco/improve-docker-tool@sha256:877b49a5abb323377c947e01bd3bbdf646e0d97142ee54bc14a118fac1160f7a" {
		t.Fatalf("call: %+v", call)
	}
	hidden, _ := rev.Hidden()
	if len(hidden) != 1 || hidden[0].Target != "/opt/nm/license/nonmem.lic" {
		t.Fatalf("hidden: %+v", hidden)
	}
	cov, err := rev.Coverage(nil)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string][]string{}
	for _, c := range cov {
		by[c.Key] = c.By
	}
	if len(by) != 4 || len(by["NM-OQ-1"]) != 1 || len(by["NM-OQ-2"]) != 0 {
		t.Fatalf("coverage: %+v", cov)
	}
}
