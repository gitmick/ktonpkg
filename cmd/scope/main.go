// scope arbeitet mit Paketen als nekton-Scope (ktonpkg/scope, kton-cockpit ADR-009).
//
//	scope from-bundle <altes-paket> <ziel> <revision> --key <schlüssel>   überführen
//	scope inspect <paket>                                                  Paket, Revisionen, Siegel, Prüfbefund
//	scope view <paket> <revision>                                          lesbare Ansicht nach files/<revision>/
//	scope seal <paket> <revision> --key <schlüssel>                        den Kopf der Revision versiegeln
//
// Der Schlüssel ist eine Datei mit dem 32-Byte-Seed als Hex, wie `plankton keygen` und
// `nekton keygen` sie schreiben; fehlt sie, wird sie angelegt.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/gitmick/ktonpkg"
	"github.com/gitmick/ktonpkg/scope"
)

func main() {
	if len(os.Args) < 3 {
		fail("usage: scope from-bundle|inspect|view|seal …")
	}
	args, keyPath := flag(os.Args[2:], "--key")
	switch os.Args[1] {
	case "from-bundle":
		if len(args) != 3 {
			fail("scope from-bundle <altes-paket> <ziel> <revision> --key <schlüssel>")
		}
		b, err := ktonpkg.Open(args[0])
		check(err)
		p, r, err := scope.FromBundle(b, args[1], args[2], key(keyPath), scope.Options{})
		check(err)
		dir, err := r.WriteView()
		check(err)
		emit(map[string]any{"package": p.ID, "revision": r.ID, "view": dir, "problems": p.Verify()})
	case "inspect":
		p, err := scope.Open(args[0])
		check(err)
		out := map[string]any{"package": p.ID, "name": p.Name, "problems": p.Verify()}
		revs, err := p.Revisions()
		check(err)
		var rs []map[string]any
		for _, r := range revs {
			claims, err := r.Claims()
			check(err)
			var cs []string
			for _, c := range claims {
				cs = append(cs, fmt.Sprintf("%s %s %s %v", short(c.ID), last(c.Predicate), short(c.Subject), c.Object["value"]))
			}
			entry := map[string]any{"label": r.Label, "id": r.ID, "claims": cs}
			if ray, err := r.Ray(); err == nil {
				id, _ := ray.ID()
				entry["rayId"] = id
			}
			rs = append(rs, entry)
		}
		out["revisions"] = rs
		seals, err := p.Seals()
		check(err)
		var ss []string
		for _, s := range seals {
			ss = append(ss, fmt.Sprintf("%s seals %s at %s", short(s.ID), short(s.Subject), short(fmt.Sprint(s.Object["hash"]))))
		}
		out["seals"] = ss
		emit(out)
	case "view":
		p, err := scope.Open(args[0])
		check(err)
		r, err := p.Revision(args[1])
		check(err)
		dir, err := r.WriteView()
		check(err)
		emit(map[string]any{"view": dir})
	case "seal":
		p, err := scope.Open(args[0])
		check(err)
		p.WithSigner(key(keyPath), scope.Options{})
		r, err := p.Revision(args[1])
		check(err)
		id, err := r.Seal()
		check(err)
		emit(map[string]any{"seal": id, "problems": p.Verify()})
	default:
		fail("unbekannt: " + os.Args[1])
	}
}

func key(path string) ed25519.PrivateKey {
	if path == "" {
		fail("--key fehlt")
	}
	if b, err := os.ReadFile(path); err == nil {
		seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
		check(err)
		return ed25519.NewKeyFromSeed(seed)
	}
	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	check(err)
	check(os.WriteFile(path, []byte(hex.EncodeToString(seed)), 0o600))
	return ed25519.NewKeyFromSeed(seed)
}

func flag(args []string, name string) ([]string, string) {
	var rest []string
	v := ""
	for i := 0; i < len(args); i++ {
		if args[i] == name && i+1 < len(args) {
			v = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	return rest, v
}

func short(s string) string {
	if len(s) > 19 {
		return s[:19] + "…"
	}
	return s
}

func last(iri string) string {
	if i := strings.LastIndexAny(iri, "/#"); i >= 0 {
		return iri[i+1:]
	}
	return iri
}

func emit(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
