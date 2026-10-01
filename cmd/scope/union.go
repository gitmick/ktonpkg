package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

// writeUnion schreibt union.json, keys.json und names.json für einen kton-web-Betrachter: die
// Records aus <wurzel>/registry/{plankton,nekton}, gelesen über den Kernel, und die Schlüssel aus
// <wurzel>/keys und <wurzel>/registry/keys. Doppelte Records (dieselbe Id aus zwei Wurzeln) einmal.
func writeUnion(out string, roots []string, namesPath string) error {
	var recs []record
	seen := map[string]bool{}
	keys := map[string]string{}
	for _, root := range roots {
		preg, err := pregistry.Open(filepath.Join(root, "registry", "plankton"))
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
		nreg, err := nregistry.Open(filepath.Join(root, "registry", "nekton"))
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
		for _, d := range []string{"keys", filepath.Join("registry", "keys")} {
			files, _ := filepath.Glob(filepath.Join(root, d, "*.pub"))
			for _, f := range files {
				b, err := os.ReadFile(f)
				if err != nil {
					return err
				}
				hexKey := strings.TrimSpace(string(b))
				pub, err := hexDecodeKey(hexKey)
				if err != nil {
					continue // keine Schlüsseldatei in der erwarteten Form; der Betrachter prüft ohne sie
				}
				keys[core.KeyIDHex(pub)] = hexKey
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

func hexDecodeKey(s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("kein öffentlicher Schlüssel")
	}
	return b, nil
}
