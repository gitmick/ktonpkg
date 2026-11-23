package scope

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"kton.dev/plankton/core"
)

// --- Terms, answers, keys ------------------------------------------------------------------------

// Term is an RDF term in the W3C SPARQL JSON results form.
type Term struct {
	Type     string `json:"type"` // uri, literal, bnode
	Value    string `json:"value"`
	Datatype string `json:"datatype,omitempty"`
	Lang     string `json:"xml:lang,omitempty"`
}

const xsdInteger = "http://www.w3.org/2001/XMLSchema#integer"

var badIRI = regexp.MustCompile(`[\s<>"{}|\\^` + "`" + `]`)

// Sparql writes the term as SPARQL text: an IRI in <>, a literal quoted with its datatype or language.
func (t Term) Sparql() (string, error) {
	switch t.Type {
	case "uri":
		if t.Value == "" || badIRI.MatchString(t.Value) {
			return "", fmt.Errorf("not a usable IRI: %q", t.Value)
		}
		return "<" + t.Value + ">", nil
	case "literal":
		r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)
		s := `"` + r.Replace(t.Value) + `"`
		switch {
		case t.Lang != "":
			return s + "@" + t.Lang, nil
		case t.Datatype != "":
			if badIRI.MatchString(t.Datatype) {
				return "", fmt.Errorf("not a usable datatype: %q", t.Datatype)
			}
			return s + "^^<" + t.Datatype + ">", nil
		}
		return s, nil
	}
	return "", fmt.Errorf("a %s term cannot be bound into a query", t.Type)
}

// Text is what a report shows for a term, as zusammensetzen does: an IRI's part after the last '#',
// a literal's value.
func (t Term) Text() string {
	if t.Type == "uri" {
		if i := strings.LastIndex(t.Value, "#"); i >= 0 {
			return t.Value[i+1:]
		}
	}
	return t.Value
}

// Answer is a SPARQL JSON SELECT answer.
type Answer struct {
	Head struct {
		Vars []string `json:"vars"`
	} `json:"head"`
	Results struct {
		Bindings []map[string]Term `json:"bindings"`
	} `json:"results"`
}

// QueryKey is the key of a bound query, the one key compose's needs, a use's ask and fill's results
// share: the query's hash, then its params sorted by name as SPARQL terms.
func QueryKey(hash string, params map[string]Term) (string, error) {
	var b strings.Builder
	b.WriteString(hash)
	b.WriteString("?")
	for i, k := range sortedKeys(params) {
		s, err := params[k].Sparql()
		if err != nil {
			return "", fmt.Errorf("$%s: %w", k, err)
		}
		if i > 0 {
			b.WriteString("&")
		}
		b.WriteString(k + "=" + s)
	}
	return b.String(), nil
}

// BindQuery writes the params into a query's text, as zusammensetzen does: every bound $VAR becomes
// its term. A $VAR left out (an optional one) stays a free variable.
func BindQuery(text []byte, params map[string]Term) (string, error) {
	var err error
	out := regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`).ReplaceAllStringFunc(string(text), func(m string) string {
		t, ok := params[m[1:]]
		if !ok {
			return m
		}
		s, e := t.Sparql()
		if e != nil && err == nil {
			err = e
		}
		return s
	})
	return out, err
}

// contentIRI is kton v0.4.0's IRI of a content hash: the one conversion (hash -> iri, round 5b).
func contentIRI(hash string) string {
	return "https://kton.dev/o/" + strings.ToLower(strings.TrimPrefix(hash, "sha256:"))
}

// --- The input manifest ---------------------------------------------------------------------------

// InputSchema is the schema of a composition input's manifest (round 3, F16).
const InputSchema = "kton.dev/composition-input/v0"

// Manifest lists every file of a composition input by hash, and the subject class it is for.
type Manifest struct {
	Schema string            `json:"schema"`
	Class  string            `json:"class,omitempty"`
	Files  map[string]string `json:"files"`
}

// Chapters is a document's chapters as an input (F11): its parts, and the scopes of its slots.
type Chapters struct {
	Chapters string            `json:"chapters"` // the document's id
	Scopes   map[string]string `json:"scopes,omitempty"`
	Parts    []Part            `json:"parts"`
}

// Input is a bound composition input: the manifest, its hash, and the files it lists, each checked.
type Input struct {
	Name     string
	Manifest Manifest
	Hash     string // of manifest.json's bytes: what compose records
	Chapters *Chapters
	Files    map[string][]byte
}

// ChaptersFile is where an input keeps its chapters.
const ChaptersFile = "chapters.json"

// LoadInput reads an input through read (a path relative to the input's folder): manifest.json, then
// every file it lists, each checked against its hash. A file the manifest does not list is not read.
// Paths follow the member-name rule.
func LoadInput(name string, read func(path string) ([]byte, error)) (*Input, error) {
	raw, err := read("manifest.json")
	if err != nil {
		return nil, fmt.Errorf("input %s: %w", name, err)
	}
	m, err := decode[Manifest](raw)
	if err != nil {
		return nil, fmt.Errorf("input %s: manifest.json: %w", name, err)
	}
	if m.Schema != InputSchema {
		return nil, fmt.Errorf("input %s: manifest schema %q, not %q", name, m.Schema, InputSchema)
	}
	in := &Input{Name: name, Manifest: m, Hash: core.HashBytes(raw), Files: map[string][]byte{}}
	paths := make([]string, 0, len(m.Files))
	for p := range m.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if msg := memberNameProblem(p); msg != "" {
			return nil, fmt.Errorf("input %s: a file %s", name, msg)
		}
		b, err := read(p)
		if err != nil {
			return nil, fmt.Errorf("input %s: %w", name, err)
		}
		if got := core.HashBytes(b); got != m.Files[p] {
			return nil, fmt.Errorf("input %s: %s has the hash %s; the input manifest says %s", name, p, got, m.Files[p])
		}
		in.Files[p] = b
	}
	if b, ok := in.Files[ChaptersFile]; ok {
		c, err := decode[Chapters](b)
		if err != nil {
			return nil, fmt.Errorf("input %s: %s: %w", name, ChaptersFile, err)
		}
		in.Chapters = &c
	}
	return in, nil
}

// ManifestFor writes the manifest of an input's files (path -> bytes), sorted, as LoadInput reads it.
func ManifestFor(class string, files map[string][]byte) ([]byte, error) {
	m := Manifest{Schema: InputSchema, Class: class, Files: map[string]string{}}
	for p, b := range files {
		if msg := memberNameProblem(p); msg != "" {
			return nil, fmt.Errorf("a file %s", msg)
		}
		m.Files[p] = core.HashBytes(b)
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
