package scope

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ConvertDokument reads one document of qm-packages (documents/<ID>/: dokument.md and kapitel/*.md)
// and gives its chapters as a composition input (F11), plus the eachPart uses its class composition
// needs. It reports what it could not carry over and never guesses (§ The converters).
//   - frontmatter id, heading and level -> parts, nested by level; a level jump gets an unnamed
//     part between, which is reported (F12, F22);
//   - the text between marks -> text uses, split as zusammensetzen splits it;
//   - {{table|image|diagram: <file>[| caption]}} -> a use with that content file;
//   - {{slot: <name>[| caption]}} -> a use of the slot's query; a slot with every: chapter -> the
//     class composition's eachPart, its scope kept with the chapters;
//   - a query's $document -> subject.iri, $chapter -> part.iri (what zusammensetzen binds).
func ConvertDokument(dir string) (*DokumentConversion, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "dokument.md"))
	if err != nil {
		return nil, err
	}
	doc, docBody, err := splitFront(string(raw))
	if err != nil {
		return nil, fmt.Errorf("dokument.md: %w", err)
	}
	c := &DokumentConversion{ID: doc.str("id"), Class: doc.str("class"), Files: map[string][]byte{},
		Report: ConversionReport{Source: dir, Carried: []ConversionNote{}, NotCarried: []ConversionNote{},
			Dropped: []ConversionNote{}, Unnamed: []ConversionNote{}}}
	if c.ID == "" {
		return nil, fmt.Errorf("dokument.md has no id")
	}
	rep := &c.Report
	for _, k := range []string{"title", "quelle"} {
		if _, ok := doc.get(k); ok {
			why := "document metadata is a claim about the subject"
			if k == "quelle" {
				why = "provenance of the conversion source, kept in this report"
			}
			rep.Dropped = append(rep.Dropped, ConversionNote{What: "dokument.md " + k, Why: why})
		}
	}
	if strings.TrimSpace(docBody) != "" {
		rep.Dropped = append(rep.Dropped, ConversionNote{What: "dokument.md body",
			Why: "a note on the document, not a chapter; zusammensetzen does not render it either"})
	}
	for _, k := range doc.keys {
		if !has([]string{"id", "title", "class", "quelle", "slots"}, k) {
			rep.NotCarried = append(rep.NotCarried, ConversionNote{What: "dokument.md key " + k, Why: "unknown key"})
		}
	}
	slotsV, _ := doc.get("slots")
	slots, _ := slotsV.(*ymap)

	slotUse := func(name, where string) (*Use, error) {
		dv, ok := slots.get(name)
		d, isMap := dv.(*ymap)
		if !ok || !isMap {
			rep.NotCarried = append(rep.NotCarried, ConversionNote{What: "slot " + name, Where: where, Why: "the slot is not defined in dokument.md"})
			return nil, nil
		}
		for _, k := range d.keys {
			if !has([]string{"scope", "query", "as", "columns", "every"}, k) {
				rep.NotCarried = append(rep.NotCarried, ConversionNote{What: "slot " + name + "." + k, Where: where, Why: "unknown slot key"})
			}
		}
		q := d.str("query")
		vocab := regexp.MustCompile(`(?:^|/)vocabulary/([^/]+)/queries/([^/]+)\.rq$`).FindStringSubmatch(q)
		local := regexp.MustCompile(`^queries/([^/]+)\.rq$`).FindStringSubmatch(q)
		if vocab == nil && local == nil {
			rep.NotCarried = append(rep.NotCarried, ConversionNote{What: "slot " + name + ".query " + q, Where: where,
				Why: "not a package query path (vocabulary/<package>/queries/<name>.rq or queries/<name>.rq)"})
			return nil, nil
		}
		block := map[string]string{"table": "table", "mermaid-tree": "tree"}[d.str("as")]
		if block == "" {
			rep.NotCarried = append(rep.NotCarried, ConversionNote{What: "slot " + name + ".as " + d.str("as"), Where: where, Why: "no block for this presentation"})
			return nil, nil
		}
		text, err := os.ReadFile(filepath.Join(dir, q))
		if err != nil {
			return nil, fmt.Errorf("slot %s: %w", name, err)
		}
		params := map[string]string{}
		for _, v := range queryVars(text) {
			src := map[string]string{"document": "subject.iri", "chapter": "part.iri"}[v]
			if src == "" {
				rep.NotCarried = append(rep.NotCarried, ConversionNote{What: "slot " + name + ": $" + v, Where: where,
					Why: "zusammensetzen binds only $document and $chapter; no source for this variable"})
				return nil, nil
			}
			params[v] = src
		}
		u := &Use{ID: name, Use: jsonString(block), Params: params, Empty: "omit"}
		if vocab != nil {
			u.Query = mustJSON(map[string]string{"package": vocab[1], "query": vocab[2]})
		} else {
			u.Query = mustJSON(map[string]string{"input": "chapters", "query": local[1]})
			c.Files["queries/"+local[1]+".rq"] = text
		}
		if sc := d.str("scope"); sc != "" {
			if d.str("every") != "" {
				u.Scope = mustJSON(map[string]string{"from": "chapters", "slot": name})
			} else {
				u.Scope = jsonString(sc)
			}
		}
		if cols, ok := d.get("columns"); ok {
			u.Options = map[string]json.RawMessage{"columns": mustJSON(cols)}
		}
		rep.Carried = append(rep.Carried, ConversionNote{What: "slot " + name, Where: where, As: block + " use"})
		return u, nil
	}

	scopes := map[string]string{}
	if slots != nil {
		for _, name := range slots.keys {
			dv, _ := slots.get(name)
			d, _ := dv.(*ymap)
			every := d.str("every")
			if every == "" {
				continue
			}
			if every != "chapter" {
				rep.NotCarried = append(rep.NotCarried, ConversionNote{What: "slot " + name + ".every " + every, Why: "only every: chapter is known (eachPart)"})
				continue
			}
			u, err := slotUse(name, "eachPart")
			if err != nil {
				return nil, err
			}
			if u != nil {
				c.EachPart = append(c.EachPart, *u)
			}
			if sc := d.str("scope"); sc != "" {
				scopes[name] = sc
			}
		}
	}

	files, _ := filepath.Glob(filepath.Join(dir, "kapitel", "*.md"))
	if sorted := byNumber(files); !equalStrings(sorted, textSorted(files)) {
		rep.NotCarried = append(rep.NotCarried, ConversionNote{What: "kapitel order",
			Why: "the chapters are taken in the order of their number; zusammensetzen sorts the names as text, which puts 100- before 11-"})
		files = sorted
	} else {
		files = sorted
	}
	type open struct {
		level int
		n     *partNode
	}
	var top []*partNode
	var stack []open
	attach := func(n *partNode) {
		if len(stack) == 0 {
			top = append(top, n)
		} else {
			parent := stack[len(stack)-1].n
			parent.kids = append(parent.kids, n)
		}
	}
	ids := map[string]bool{}
	unnamedN := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		k, body, err := splitFront(string(raw))
		name := filepath.Base(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		lv, _ := k.get("level")
		level, isInt := lv.(int)
		if k.str("id") == "" || k.str("heading") == "" || !isInt {
			return nil, fmt.Errorf("%s: the frontmatter needs id, heading and level", name)
		}
		id := k.str("id")
		if ids[id] {
			return nil, fmt.Errorf("%s: the part id %s is not unique", name, id)
		}
		ids[id] = true
		for _, key := range k.keys {
			if !has([]string{"id", "heading", "level", "rules"}, key) {
				rep.NotCarried = append(rep.NotCarried, ConversionNote{What: name + " frontmatter " + key, Why: "unknown key"})
			}
		}
		part := &Part{ID: id, Heading: k.str("heading")}
		if rv, ok := k.get("rules"); ok {
			rs, _ := rv.([]any)
			for _, r := range rs {
				if s, ok := r.(string); ok {
					part.Rules = append(part.Rules, s)
				}
			}
		}
		n := 0
		for _, seg := range splitMarks(body) {
			m := markParts.FindStringSubmatch(seg)
			if m == nil {
				if regexp.MustCompile(`^\{\{.*\}\}$`).MatchString(seg) {
					rep.NotCarried = append(rep.NotCarried, ConversionNote{What: seg, Where: id,
						Why: "a mark that does not parse; zusammensetzen would print it as text, so it is kept as text"})
				}
				if t := strings.TrimSpace(seg); t != "" {
					n++
					p := fmt.Sprintf("content/%s-%d.md", id, n)
					c.Files[p] = []byte(t + "\n")
					part.Blocks = append(part.Blocks, Use{Use: jsonString("text"), File: p})
				}
				continue
			}
			kind, ref, caption := m[1], m[2], m[3]
			switch kind {
			case "slot":
				u, err := slotUse(ref, id)
				if err != nil {
					return nil, err
				}
				if u != nil {
					if caption != "" {
						u.Caption = caption
					}
					part.Blocks = append(part.Blocks, *u)
				}
			case "table", "image", "diagram":
				b, err := os.ReadFile(filepath.Join(dir, ref))
				if err != nil {
					return nil, fmt.Errorf("%s: {{%s: %s}}: %w", name, kind, ref, err)
				}
				p := "content/" + kind + "s/" + path.Base(ref)
				c.Files[p] = b
				part.Blocks = append(part.Blocks, Use{Use: jsonString(kind), File: p, Caption: caption})
				rep.Carried = append(rep.Carried, ConversionNote{What: "{{" + kind + ": " + ref + "}}", Where: id, As: kind + " use with a content file"})
			default:
				rep.NotCarried = append(rep.NotCarried, ConversionNote{What: seg, Where: id, Why: "no block for {{" + kind + ":…}}"})
			}
		}
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			stack = stack[:len(stack)-1]
		}
		for {
			have := 0
			if len(stack) > 0 {
				have = stack[len(stack)-1].level
			}
			if have >= level-1 {
				break
			}
			unnamedN++
			u := &partNode{part: Part{ID: fmt.Sprintf("_unnamed-%d", unnamedN), Unnamed: true}}
			rep.Unnamed = append(rep.Unnamed, ConversionNote{Where: name, What: fmt.Sprintf("level %d", have+1),
				Why: fmt.Sprintf("level %d follows level %d; an unnamed level-%d part holds it (fill writes no section for it)", level, have, have+1)})
			attach(u)
			stack = append(stack, open{level: have + 1, n: u})
		}
		node := &partNode{part: *part}
		attach(node)
		stack = append(stack, open{level: level, n: node})
	}
	c.Chapters = Chapters{Chapters: c.ID, Parts: toParts(top)}
	if len(scopes) > 0 {
		c.Chapters.Scopes = scopes
	}
	rep.Parts = len(ids)
	return c, nil
}

// partNode is a part while the tree is built: its children are attached after it.
type partNode struct {
	part Part
	kids []*partNode
}

func toParts(ns []*partNode) []Part {
	var out []Part
	for _, n := range ns {
		p := n.part
		p.Parts = toParts(n.kids)
		out = append(out, p)
	}
	return out
}

// DokumentConversion is one converted document.
type DokumentConversion struct {
	ID       string
	Class    string
	Chapters Chapters
	Files    map[string][]byte // the input's files besides chapters.json
	EachPart []Use
	Report   ConversionReport
}

// ConversionReport says what was carried over, what was not and why, what was dropped, and where
// an unnamed part was inserted.
type ConversionReport struct {
	Source     string           `json:"source"`
	Carried    []ConversionNote `json:"carried"`
	NotCarried []ConversionNote `json:"notCarried"`
	Dropped    []ConversionNote `json:"dropped"`
	Unnamed    []ConversionNote `json:"unnamed"`
	Parts      int              `json:"parts"`
}

// ConversionNote is one line of a conversion report.
type ConversionNote struct {
	What  string `json:"what"`
	Where string `json:"where,omitempty"`
	As    string `json:"as,omitempty"`
	Why   string `json:"why,omitempty"`
}

var (
	markRe    = regexp.MustCompile(`\{\{[a-z]+:[^}]*\}\}`)
	markParts = regexp.MustCompile(`^\{\{([a-z]+):\s*([^|}]+?)\s*(?:\|\s*([^}]*?))?\s*\}\}$`)
)

// splitMarks splits a body into text and marks, keeping the marks (Python's re.split with a group).
func splitMarks(body string) []string {
	var out []string
	last := 0
	for _, ix := range markRe.FindAllStringIndex(body, -1) {
		out = append(out, body[last:ix[0]], body[ix[0]:ix[1]])
		last = ix[1]
	}
	return append(out, body[last:])
}

// InputFiles are the files of the document's chapters input, with chapters.json and manifest.json.
func (c *DokumentConversion) InputFiles() (map[string][]byte, error) {
	files := map[string][]byte{}
	for p, b := range c.Files {
		files[p] = b
	}
	ch, err := pyJSON(c.Chapters)
	if err != nil {
		return nil, err
	}
	files[ChaptersFile] = append(ch, '\n')
	m, err := ManifestFor(c.Class, files)
	if err != nil {
		return nil, err
	}
	files["manifest.json"] = m
	return files, nil
}

// ClassComposition is the report composition of a document class: the chapters, and the eachPart
// uses of the class's slots. Fixed parts (a header, a training area) are added around it by hand.
func ClassComposition(class string, eachPart []Use) Composition {
	c := Composition{Composition: strings.ToLower(class) + "-class", Kind: "report", Class: class,
		Inputs: []string{"chapters"}, EachPart: eachPart, Parts: []Part{{From: "chapters"}}}
	return c
}

func jsonString(s string) json.RawMessage { return mustJSON(s) }

func mustJSON(v any) json.RawMessage {
	if m, ok := v.(*ymap); ok {
		v = m.plain()
	}
	if xs, ok := v.([]any); ok {
		out := make([]any, len(xs))
		for i, x := range xs {
			if m, ok := x.(*ymap); ok {
				out[i] = m.plain()
			} else {
				out[i] = x
			}
		}
		v = out
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func (m *ymap) plain() map[string]any {
	out := map[string]any{}
	for _, k := range m.keys {
		v := m.vals[k]
		if c, ok := v.(*ymap); ok {
			v = c.plain()
		}
		out[k] = v
	}
	return out
}

// byNumber sorts kapitel files by their leading number, then by name: 9-, 10-, 100-.
func byNumber(files []string) []string {
	out := append([]string(nil), files...)
	num := func(f string) (int, bool) {
		b := filepath.Base(f)
		i := 0
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			i++
		}
		if i == 0 {
			return 0, false
		}
		n := 0
		for _, c := range b[:i] {
			n = n*10 + int(c-'0')
		}
		return n, true
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, aok := num(out[i])
		b, bok := num(out[j])
		if aok && bok && a != b {
			return a < b
		}
		if aok != bok {
			return aok
		}
		return filepath.Base(out[i]) < filepath.Base(out[j])
	})
	return out
}

func textSorted(files []string) []string {
	out := append([]string(nil), files...)
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
