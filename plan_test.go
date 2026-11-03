package ktonpkg

import (
	"path"
	"strings"
	"testing"
)

const testImage = "oci://docker.io/pandoc/core@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// renderBundle is one step that renders a source with a template; proto is its protocol.
func renderBundle(t *testing.T, proto Protocol, outputs []string) *Bundle {
	t.Helper()
	b := New(t.TempDir(), KindRay, "render")
	sh, _ := b.AddFile("steps/render/render.sh", []byte("pandoc source.md -o document.$1\n"))
	if _, err := b.AddFile("testdaten/source.md", []byte("# Title\n")); err != nil {
		t.Fatal(err)
	}
	b.Ray.Steps = []Step{{ID: "render", Protocol: proto, Params: map[string]string{"format": "docx"},
		Inputs: []Input{
			{Slot: "render.sh", Path: "steps/render/render.sh", Hash: sh, Variable: "command-file"},
			{Slot: "source.md", From: "hole:source"},
		}, Outputs: outputs}}
	b.Ray.Holes = []Hole{{Name: "source", Step: "render", Slot: "source.md", Type: HoleFile, Required: true,
		Default: "testdaten/source.md"}}
	return b
}

// A step that says what it writes needs no reference run to be planned.
func TestPlan_UsesTheStepsOwnOutputs(t *testing.T) {
	b := renderBundle(t, Protocol{Command: &Command{Args: "<command-file>\n<format>"}}, []string{"document.docx"})
	pl, err := b.Plan("work", nil, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := pl.Steps[0].Outputs; len(got) != 1 || got[0] != path.Join("work", "render", "document.docx") {
		t.Fatalf("outputs %v", got)
	}
}

// With protocol.image the step names where it runs; the command line stays as written (no line is
// taken away, no Rscript is put in front), and the image is in the plan for the executor to honour.
func TestPlan_ImageIsCarriedAndTheCommandKeptAsWritten(t *testing.T) {
	b := renderBundle(t, Protocol{Image: testImage, Command: &Command{Args: "sh\n<command-file>\n<format>"}},
		[]string{"document.docx"})
	pl, err := b.Plan("work", nil, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s := pl.Steps[0]
	if s.Image != testImage {
		t.Errorf("image %q", s.Image)
	}
	if want := "cd work/render && sh render.sh docx"; s.Cmd != want {
		t.Errorf("cmd %q, want %q", s.Cmd, want)
	}
}

// The old form keeps working: an image as the first args line (improve) still becomes the plan's
// image, and the command its Rscript line, as before.
func TestPlan_TheImproveArgsLineStillWorks(t *testing.T) {
	line := "docker.io/r/base@sha256:" + strings.Repeat("1", 64)
	b := renderBundle(t, Protocol{Command: &Command{Args: line + "\n<command-file>"}}, []string{"document.docx"})
	pl, err := b.Plan("work", nil, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if s := pl.Steps[0]; s.Image != line || s.Cmd != "cd work/render && Rscript render.sh" {
		t.Fatalf("image %q, cmd %q", s.Image, s.Cmd)
	}
}

func TestPlan_RefusesAnImageNotPinnedByDigest(t *testing.T) {
	b := renderBundle(t, Protocol{Image: "oci://pandoc/core:latest", Command: &Command{Args: "<command-file>"}},
		[]string{"document.docx"})
	if _, err := b.Plan("work", nil, PlanOptions{}); err == nil || !strings.Contains(err.Error(), "@sha256:") {
		t.Fatalf("want a refusal naming @sha256:, got %v", err)
	}
}

// The image is part of the computation: the same step in another image is another potential. A step
// without the field keeps the identity it had before the field existed.
func TestIdentity_TheImageIsPartOfTheStep(t *testing.T) {
	cmd := &Command{Args: "<command-file>"}
	without := renderBundle(t, Protocol{Command: cmd}, nil)
	with := renderBundle(t, Protocol{Image: testImage, Command: cmd}, nil)
	other := renderBundle(t, Protocol{Image: strings.Replace(testImage, "0123", "4567", 1), Command: cmd}, nil)
	a, _ := without.Ray.FotonID("render", nil, nil)
	b, _ := with.Ray.FotonID("render", nil, nil)
	c, _ := other.Ray.FotonID("render", nil, nil)
	if a == b || b == c {
		t.Fatalf("ids must differ by image: %s %s %s", a, b, c)
	}
	f, err := without.Ray.Foton("render", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := f.Protocol.Descriptor["image"]; has {
		t.Fatal("a step without an image must not get an image key (old ids would change)")
	}
}
