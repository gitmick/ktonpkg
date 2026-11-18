package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/gitmick/ktonpkg/scope"
	nregistry "kton.dev/nekton/registry"
	"kton.dev/plankton/core"
	pregistry "kton.dev/plankton/registry"
)

// record ist ein Eintrag der union, wie kton-web ihn liest und `cockpit show` ihn liefert.
type record struct {
	Seq      int             `json:"seq"`
	FotonID  string          `json:"fotonId,omitempty"`
	ClaimID  string          `json:"claimId,omitempty"`
	Envelope json.RawMessage `json:"envelope"`
}

// writeUnion writes union.json, keys.json and names.json for a kton-web viewer: the records from
// <root>/{plankton,nekton}, read through the kernel, and the public keys under <root>/keys at any
// depth. A root is a package, or a repository, whose kton part is <root>/.kton (ADR-016). A record
// found under two roots is written once.
func writeUnion(out string, roots []string, namesPath string) error {
	var recs []record
	seen := map[string]bool{}
	keys := map[string]string{}
	for _, root := range roots {
		if _, err := os.Stat(filepath.Join(root, ".kton", "cockpit.config.json")); err == nil {
			root = filepath.Join(root, ".kton")
		}
		preg, err := pregistry.Open(filepath.Join(root, scope.PlanktonDir))
		if err != nil {
			return err
		}
		for _, r := range preg.Records(0) {
			if seen[r.FotonID] {
				continue
			}
			seen[r.FotonID] = true
			env, err := json.Marshal(r.Envelope)
			if err != nil {
				return err
			}
			recs = append(recs, record{Seq: len(recs) + 1, FotonID: r.FotonID, Envelope: env})
		}
		nreg, err := nregistry.Open(filepath.Join(root, scope.NektonDir))
		if err != nil {
			return err
		}
		for _, r := range nreg.Records(0) {
			if seen[r.ClaimID] {
				continue
			}
			seen[r.ClaimID] = true
			env, err := json.Marshal(r.Envelope)
			if err != nil {
				return err
			}
			recs = append(recs, record{Seq: len(recs) + 1, ClaimID: r.ClaimID, Envelope: env})
		}
		if _, err := os.Stat(filepath.Join(root, scope.KeyDir)); err == nil {
			pubs, err := scope.KeysIn(filepath.Join(root, scope.KeyDir))
			if err != nil {
				return err
			}
			for _, pub := range pubs {
				keys[core.KeyIDHex(pub)] = hex.EncodeToString(pub)
			}
		}
	}
	names := map[string]string{}
	if namesPath != "" {
		b, err := os.ReadFile(namesPath)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &names); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	ids := make([]string, 0, len(keys))
	for k := range keys {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	for name, v := range map[string]any{"union.json": recs, "keys.json": keys, "names.json": names} {
		b, err := json.MarshalIndent(v, "", " ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, name), append(b, '\n'), 0o644); err != nil {
			return err
		}
	}
	emit(map[string]any{"out": out, "records": len(recs), "keys": ids})
	return nil
}
