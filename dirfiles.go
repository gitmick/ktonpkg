package ktonpkg

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// NamedFiles lists the entries of dir whose name ends in suffix ("" for all), as sorted paths
// joined to dir. It reads the directory instead of globbing Join(dir, "*"+suffix): a glob reads
// dir itself as a pattern, so a '*', '?' or '[' in it matches sibling folders and a backslash
// matches nothing (kton-review G2 on kton-cockpit #256). A dir that does not exist lists nothing,
// as the glob did; any other error is returned, which the glob swallowed.
func NamedFiles(dir, suffix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) && len(e.Name()) > len(suffix) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}
