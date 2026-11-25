// apply re-executes the NONMEM normaliser on outputs and records each application, so plankton's
// Reproduces (and so scope spectrum-check) can reach L1 for two runs:
//
//	go run ./spectra/nonmem/apply <registry> <key> <file>...
//
// For each file it runs normalize-nonmem.sh in its pinned busybox image (nonmem.Image, no network),
// signs nonmem.Application(hash of the file, hash of what the script wrote) with key (a hex ed25519
// seed) and adds it to registry. It prints "<file hash> -> <normalized hash> <application id>".
// Re-executing rather than trusting a recorded application: kton security normalizer-forge.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gitmick/ktonpkg/spectra/nonmem"
	"kton.dev/plankton/core"
	"kton.dev/plankton/foton"
	pregistry "kton.dev/plankton/registry"
)

func main() {
	if len(os.Args) < 4 {
		fail(fmt.Errorf("usage: apply <registry> <key> <file>..."))
	}
	regDir, keyPath, files := os.Args[1], os.Args[2], os.Args[3:]
	raw, err := os.ReadFile(keyPath)
	fail(err)
	seed, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	fail(err)
	if len(seed) != ed25519.SeedSize {
		fail(fmt.Errorf("%s: not a hex ed25519 seed", keyPath))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	fail(os.MkdirAll(regDir, 0o755))
	reg, err := pregistry.Open(regDir)
	fail(err)
	image := strings.TrimPrefix(strings.TrimPrefix(nonmem.Image, "oci://"), "docker.io/library/")
	for _, f := range files {
		in, err := os.ReadFile(f)
		fail(err)
		w, err := os.MkdirTemp("", "nonmem-apply-")
		fail(err)
		fail(os.WriteFile(filepath.Join(w, "in"), in, 0o644))
		fail(os.WriteFile(filepath.Join(w, "normalize-nonmem.sh"), nonmem.Script, 0o644))
		cmd := exec.Command("docker", "run", "--rm", "--network", "none", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
			"-v", w+":/w", "-w", "/w", image, "sh", "normalize-nonmem.sh", "in", "normalized")
		if out, err := cmd.CombinedOutput(); err != nil {
			fail(fmt.Errorf("%s: %v: %s", f, err, out))
		}
		norm, err := os.ReadFile(filepath.Join(w, "normalized"))
		fail(err)
		os.RemoveAll(w)
		app, err := nonmem.Application(core.HashBytes(in), core.HashBytes(norm))
		fail(err)
		spec := foton.Spec{Protocol: &foton.ProtocolSpec{Kind: app.Protocol.Kind, Descriptor: app.Protocol.Descriptor}}
		for _, i := range app.Inputs {
			spec.Inputs = append(spec.Inputs, foton.FileSpec{Path: i.Path, Hash: i.Hash})
		}
		for _, o := range app.Outputs {
			spec.Outputs = append(spec.Outputs, foton.FileSpec{Path: o.Path, Hash: o.Hash})
		}
		env, _, err := foton.SignWith(spec, priv)
		fail(err)
		id, _, err := reg.Add(env)
		fail(err)
		if want, err := app.FotonID(); err != nil || want != id {
			fail(fmt.Errorf("%s: the kernel assigns %s, ktonpkg computes %s (%v)", f, id, want, err))
		}
		fmt.Printf("%s -> %s %s\n", core.HashBytes(in), core.HashBytes(norm), id)
	}
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "apply:", err)
		os.Exit(1)
	}
}
