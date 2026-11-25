package scope

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gitmick/ktonpkg"
	"kton.dev/plankton/core"
)

// Migrate moves a package from the layout before v0.8.0 (registry/{nekton,plankton,keys},
// blobs/sha256/<hex>) to ADR-016's (nekton/, plankton/, keys/, blobs/<hh>/<hex>). Every id in a
// package is a content hash and names no folder, so the package, its revisions, claims and fotons
// keep their ids. Bytes are checked against their hash as they move. A package already in the new
// layout is left as it is; Migrate reports whether it moved anything.
func Migrate(dir string) (bool, error) {
	old := filepath.Join(dir, "registry")
	oldBlobs := filepath.Join(dir, "blobs", "sha256")
	_, errReg := os.Stat(filepath.Join(old, "nekton"))
	_, errBlobs := os.Stat(oldBlobs)
	if errReg != nil && errBlobs != nil {
		if _, err := os.Stat(filepath.Join(dir, NektonDir)); err != nil {
			return false, fmt.Errorf("%s is not a package: neither %s nor registry/nekton", dir, NektonDir)
		}
		return false, nil
	}
	if errReg == nil {
		for _, d := range []string{NektonDir, PlanktonDir} {
			to := filepath.Join(dir, d)
			if entries, err := os.ReadDir(to); err == nil && len(entries) > 0 {
				return false, fmt.Errorf("%s has both registry/%s and %s", dir, d, d)
			}
			os.Remove(to)
			if err := os.Rename(filepath.Join(old, d), to); err != nil {
				return false, err
			}
		}
		// Keys a registry kept beside its records (recorded runs, people's signatures) join keys/.
		files, err := ktonpkg.NamedFiles(filepath.Join(old, "keys"), ".pub")
		if err != nil {
			return false, err
		}
		if len(files) > 0 {
			if err := os.MkdirAll(filepath.Join(dir, KeyDir), 0o755); err != nil {
				return false, err
			}
			for _, f := range files {
				if err := os.Rename(f, filepath.Join(dir, KeyDir, filepath.Base(f))); err != nil {
					return false, err
				}
			}
			os.Remove(filepath.Join(old, "keys"))
		}
		if err := os.Remove(old); err != nil {
			return false, fmt.Errorf("registry/ still holds something after the move: %w", err)
		}
	}
	if errBlobs == nil {
		entries, err := os.ReadDir(oldBlobs)
		if err != nil {
			return false, err
		}
		for _, e := range entries {
			if e.IsDir() {
				return false, fmt.Errorf("blobs/sha256/%s: a folder where bytes were expected", e.Name())
			}
			body, err := os.ReadFile(filepath.Join(oldBlobs, e.Name()))
			if err != nil {
				return false, err
			}
			if got := core.HashBytes(body); got != "sha256:"+e.Name() {
				return false, fmt.Errorf("blobs/sha256/%s holds bytes with hash %s", e.Name(), got)
			}
			p := &Package{Dir: dir}
			if _, err := p.PutBlob(body); err != nil {
				return false, err
			}
			if err := os.Remove(filepath.Join(oldBlobs, e.Name())); err != nil {
				return false, err
			}
		}
		if err := os.Remove(oldBlobs); err != nil {
			return false, err
		}
	}
	return true, nil
}
