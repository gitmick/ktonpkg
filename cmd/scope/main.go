// scope arbeitet mit Paketen als nekton-Scope (ktonpkg/scope, kton-cockpit ADR-009).
//
//	scope from-bundle <altes-paket> <ziel> <revision> --key <schlüssel>   überführen
//	scope inspect <paket>                                                  Paket, Revisionen, Siegel, Prüfbefund
//	scope view <paket> <revision>                                          lesbare Ansicht nach files/<revision>/
//	scope seal <paket> <revision> --key <schlüssel>                        den Kopf der Revision versiegeln
//	scope add-run <paket> <revision> <foton-id> --registry <plankton-dir> --root <dir>
//	              --pub <schlüssel.pub> --kind reference|render [--step <step>] --key <schlüssel>
//	                                                                       einen Lauf samt Ausgaben aufnehmen
//	scope install <paket> <revision> <plankton-dir> <nekton-dir> <ansicht> beim User einspielen
//	scope union <ziel> <wurzel>… [--names <names.json>]                     union/keys/names für kton-web
//	scope templates <ziel> <name> <revision> --templates <dir> --queries <dir> --key <schlüssel>
//	                                                                       ein Vokabularpaket bauen (Template-Profil)
//
// from-bundle --only-measured lässt Referenzen und Dokumente weg, die mit ihren Läufen nachkommen.
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
	"path/filepath"
	"strings"

	"github.com/gitmick/ktonpkg"
	"github.com/gitmick/ktonpkg/scope"
	pregistry "kton.dev/plankton/registry"
)

func main() {
	if len(os.Args) < 3 {
		fail("usage: scope from-bundle|inspect|view|seal …")
	}
	args, keyPath := flag(os.Args[2:], "--key")
	args, onlyMeasured := boolFlag(args, "--only-measured")
	switch os.Args[1] {
	case "from-bundle":
		if len(args) != 3 {
			fail("scope from-bundle <altes-paket> <ziel> <revision> --key <schlüssel>")
		}
		b, err := ktonpkg.Open(args[0])
		check(err)
		p, r, err := scope.FromBundle(b, args[1], args[2], key(keyPath), scope.Options{OnlyMeasured: onlyMeasured})
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
			if id, err := r.RayID(); err == nil {
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
	case "add-run":
		args, regDir := flag(args, "--registry")
		args, root := flag(args, "--root")
		args, pubPath := flag(args, "--pub")
		args, kind := flag(args, "--kind")
		args, step := flag(args, "--step")
		if len(args) != 3 || regDir == "" || root == "" || pubPath == "" || kind == "" {
			fail("scope add-run <paket> <revision> <foton-id> --registry <dir> --root <dir> --pub <datei> --kind <art> [--step <step>] --key <schlüssel>")
		}
		p, err := scope.Open(args[0])
		check(err)
		p.WithSigner(key(keyPath), scope.Options{})
		r, err := p.Revision(args[1])
		check(err)
		reg, err := pregistry.Open(regDir)
		check(err)
		env, ok := reg.Envelope(args[2])
		if !ok {
			fail(args[2] + " liegt nicht in " + regDir)
		}
		f, _ := reg.Foton(args[2])
		outputs := map[string][]byte{}
		for _, o := range f.Outputs {
			body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(o.Path)))
			check(err)
			outputs[o.Path] = body
		}
		raw, err := os.ReadFile(pubPath)
		check(err)
		pub, err := hex.DecodeString(strings.TrimSpace(string(raw)))
		check(err)
		extra := map[string]any{}
		if step != "" {
			extra["step"] = step
		}
		id, err := r.AddRun(env, ed25519.PublicKey(pub), kind, extra, outputs)
		check(err)
		_, err = r.WriteView()
		check(err)
		emit(map[string]any{"run": id, "problems": p.Verify()})
	case "install":
		if len(args) != 5 {
			fail("scope install <paket> <revision> <plankton-dir> <nekton-dir> <ansicht>")
		}
		p, err := scope.Open(args[0])
		check(err)
		in, err := p.Install(args[1], args[2], args[3])
		check(err)
		r, err := p.Revision(args[1])
		check(err)
		view, err := r.WriteView()
		check(err)
		check(os.RemoveAll(args[4]))
		check(os.CopyFS(args[4], os.DirFS(view)))
		emit(in)
	case "templates":
		args, tdir := flag(args, "--templates")
		args, qdir := flag(args, "--queries")
		if len(args) != 3 {
			fail("scope templates <ziel> <name> <revision> --templates <dir> --queries <dir> --key <schlüssel>")
		}
		p, r, err := scope.BuildTemplates(args[0], args[1], args[2], tdir, qdir, key(keyPath), scope.Options{})
		check(err)
		ts, err := r.Templates()
		check(err)
		qs, err := r.Queries()
		check(err)
		emit(map[string]any{"package": p.ID, "name": p.Name, "revision": r.ID, "templates": scope.Names(ts),
			"queries": scope.Names(qs), "problems": p.Verify()})
	case "union":
		args, namesPath := flag(args, "--names")
		if len(args) < 2 {
			fail("scope union <ziel> <wurzel>… [--names <names.json>]")
		}
		check(writeUnion(args[0], args[1:], namesPath))
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

func boolFlag(args []string, name string) ([]string, bool) {
	var rest []string
	found := false
	for _, a := range args {
		if a == name {
			found = true
			continue
		}
		rest = append(rest, a)
	}
	return rest, found
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
