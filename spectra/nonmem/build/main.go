// build writes the nonmem-spectrum tool package (kton-package/1):
//
//	go run ./spectra/nonmem/build <out> <reference-dir>
//
// <reference-dir> holds what build.sh prepared: normalized/run1.{lst,xml,cpu} (the normaliser's own
// outputs for the reference run), raw/run1.lst (the hole's test data), model.ctl and data.csv.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gitmick/ktonpkg"
	"github.com/gitmick/ktonpkg/spectra/nonmem"
)

func main() {
	if len(os.Args) != 3 {
		fail(fmt.Errorf("usage: build <out> <reference-dir>"))
	}
	out, ref := os.Args[1], os.Args[2]
	read := func(rel string) []byte {
		b, err := os.ReadFile(filepath.Join(ref, rel))
		fail(err)
		return b
	}
	b := ktonpkg.New(out, ktonpkg.KindTool, "nonmem-spectrum")
	_, err := b.AddFile(nonmem.ScriptPath, nonmem.Script)
	fail(err)
	_, err = b.AddFile("testdaten/run1.lst", read("raw/run1.lst"))
	fail(err)
	b.Ray = nonmem.Ray()
	b.Ray.Holes[0].Default = "testdaten/run1.lst"
	normalizer, err := nonmem.Ref()
	fail(err)
	model, data := ktonpkg.HashOf(read("model.ctl")), ktonpkg.HashOf(read("data.csv"))
	names, err := filepath.Glob(filepath.Join(ref, "normalized", "*"))
	fail(err)
	sort.Strings(names)
	spec := &ktonpkg.Spectrum{Spectrum: nonmem.Spectrum, Of: nonmem.Of, Normalizer: normalizer}
	for _, n := range names {
		base := filepath.Base(n)
		if len(read(filepath.Join("normalized", base))) == 0 {
			continue // .cpu: emptied by the normaliser, so it qualifies nothing
		}
		h, err := b.AddFile(filepath.Join(ktonpkg.ReferenceDir, base), read(filepath.Join("normalized", base)))
		fail(err)
		spec.Members = append(spec.Members, ktonpkg.SpectrumMember{Name: "THEO-101 " + base, Output: h, Model: model, Data: data})
	}
	b.Spectrum = spec
	fail(b.Write())
	if probs := b.Verify(); len(probs) > 0 {
		fail(fmt.Errorf("verify: %v", probs))
	}
	id, _ := spec.ID()
	fmt.Printf("normalizer %s\nspectrum %s (%d members)\n", normalizer, id, len(spec.Members))
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
}
