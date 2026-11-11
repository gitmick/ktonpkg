// Package nonmem is the minimal NONMEM spectrum (MH 11.11.): its normaliser and the ray that
// applies it. The normaliser is one shell script, normalize-nonmem.sh, embedded here; there is no
// second implementation. Its potential (Ref) covers the script's bytes, its arguments and a pinned
// image, so anyone can re-execute exactly it (kton security: normalizer-forge) and every application
// has the same ref, whatever file it normalises.
//
// An application is the foton Application gives: input = the output being normalised (slot "in"),
// first output = "normalized". plankton's Reproduces then finds L1 for two outputs whose
// applications under Ref end in the same hash.
package nonmem

import (
	_ "embed"

	"github.com/gitmick/ktonpkg"
	"kton.dev/plankton/core"
)

//go:embed normalize-nonmem.sh
var Script []byte

const (
	// Image runs the script: busybox, pinned by digest.
	Image = "oci://docker.io/library/busybox@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec"
	// ScriptPath is where the package carries the script.
	ScriptPath = "steps/normalize/normalize-nonmem.sh"
	// Spectrum and Of name the spectrum and the tool it qualifies.
	Spectrum = "nonmem-7.5"
	Of       = "nonmem_7.5 docker (NONMEM 7.5.1, improve)"
)

// Ray is the one-step ray: sh normalize-nonmem.sh in normalized, in Image; hole "output" is the file.
func Ray() *ktonpkg.Ray {
	return &ktonpkg.Ray{Name: "nonmem-normalize",
		Steps: []ktonpkg.Step{{ID: "normalize",
			Protocol: ktonpkg.Protocol{Image: Image, Command: &ktonpkg.Command{Args: "sh\n<command-file>\nin\nnormalized"}},
			Inputs: []ktonpkg.Input{
				{Slot: "normalize-nonmem.sh", Path: ScriptPath, Hash: core.HashBytes(Script), Variable: "command-file"},
				{Slot: "in", From: "hole:output"},
			},
			Outputs: []string{"normalized"}}},
		Holes: []ktonpkg.Hole{{Name: "output", Step: "normalize", Slot: "in", Type: ktonpkg.HoleFile, Required: true}}}
}

// Ref is the normaliser's potential: the spectrum's normalizer, and the protocol of every application.
func Ref() (string, error) {
	f, err := Ray().Foton("normalize", nil, nil)
	if err != nil {
		return "", err
	}
	return f.Protocol.Ref, nil
}

// Application is the foton of one application: output is the hash of the file normalised, normalized
// the hash of what the script wrote. The caller signs and adds it.
func Application(output, normalized string) (*core.Foton, error) {
	f, err := Ray().Foton("normalize", ktonpkg.Bindings{"output": output}, nil)
	if err != nil {
		return nil, err
	}
	f.Outputs = []core.FileRef{{Path: "normalized", Hash: normalized}}
	return f, nil
}
