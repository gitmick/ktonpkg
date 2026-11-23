package scope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// Fill is the third step, compose → query → fill (§6): a composed tree and the answers to its uses'
// asks give report-v2 for render_report.R, the files it names, and slots.json, the record of what
// the report rests on. It mirrors qm-packages' zusammensetzen.py, so render does not change:
//   - sections flat in reading order with their level; an id that occurs twice is qualified by its
//     key ("/" -> "."); an unnamed part writes no section;
//   - text -> a narrative sections/<part>-<n>.md (the content's bytes); a table, image or diagram file
//     -> tables/, images/, diagrams/ under its own name;
//   - a query table -> tables/<part>--<use>.json {columns, rows, boldHeader}; a query tree ->
//     diagrams/<part>--<use>.json and an image block render draws; a number -> a narrative line;
//   - with no rows and empty omit, a query use writes nothing;
//   - slots.json lists every query use with its row count, also 0, and every other use as skipped.

// ReportV2 is report-v2 as render_report.R reads it.
type ReportV2 struct {
	Schema   string          `json:"schema"`
	Document ReportDocument  `json:"document"`
	Sections []ReportSection `json:"sections"`
}

// ReportDocument is report-v2's document head.
type ReportDocument struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Class string `json:"class"`
}

// ReportSection is one section of report-v2.
type ReportSection struct {
	ID      string        `json:"id"`
	Heading string        `json:"heading"`
	Level   int           `json:"level"`
	Blocks  []ReportBlock `json:"blocks"`
	Rules   []string      `json:"rules"`
}

// ReportBlock is one block of a report-v2 section.
type ReportBlock struct {
	Type    string  `json:"type"`
	File    string  `json:"file"`
	Caption *string `json:"caption,omitempty"`
	Source  string  `json:"source,omitempty"`
}

// SlotRecord is one line of slots.json.
type SlotRecord struct {
	Use       string `json:"use"`
	Part      string `json:"part"`
	Query     string `json:"query,omitempty"`
	QueryHash string `json:"queryHash,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Rows      *int   `json:"rows,omitempty"`
	Block     string `json:"block,omitempty"`
	Skipped   string `json:"skipped,omitempty"`
}

// Filled is what Fill gives: the report, its files by path, and the slot record.
type Filled struct {
	Report ReportV2
	Files  map[string][]byte
	Slots  []SlotRecord
}

// Fill writes the report. Every use with a query needs its answer in results, by its ask.
func Fill(c *Composed, results map[string]Answer) (*Filled, error) {
	out := &Filled{Files: map[string][]byte{}, Slots: []SlotRecord{}}
	count := map[string]int{}
	for _, s := range c.Sections {
		count[s.ID]++
	}
	sid := func(s Section) string {
		if count[s.ID] > 1 {
			return strings.ReplaceAll(s.Key, "/", ".")
		}
		return s.ID
	}
	uses := map[string]ComposedUse{}
	for _, u := range c.Uses {
		uses[u.Key] = u
	}
	out.Report = ReportV2{Schema: "report-v2", Sections: []ReportSection{},
		Document: ReportDocument{ID: c.Subject.Identifier, Title: c.Subject.Title, Class: c.Subject.Class}}
	if out.Report.Document.ID == "" {
		out.Report.Document.ID = c.Subject.IRI
	}
	for _, s := range c.Sections {
		id := sid(s)
		blocks := []ReportBlock{}
		n := 0
		narrative := func(body []byte) {
			n++
			fn := fmt.Sprintf("sections/%s-%d.md", id, n)
			out.Files[fn] = body
			blocks = append(blocks, ReportBlock{Type: "narrative", File: fn})
		}
		for _, key := range s.Uses {
			u := uses[key]
			var ans *Answer
			if u.Query != nil {
				if u.Ask == "" {
					return nil, fmt.Errorf("fill: %s waits for %s; a report has no unset vars", u.Key, strings.Join(u.Waiting, ", "))
				}
				a, ok := results[u.Ask]
				if !ok {
					return nil, fmt.Errorf("fill: no answer for %s (%s)", u.Key, u.Ask)
				}
				ans = &a
				rows := len(a.Results.Bindings)
				out.Slots = append(out.Slots, SlotRecord{Use: u.Key, Part: id, Query: u.Query.where() + "/" + u.Query.Name,
					QueryHash: u.Query.Hash, Scope: u.Scope, Rows: &rows})
			}
			stem := id + "--" + u.ID
			var cp *string
			if u.Caption != "" {
				cp = &u.Caption
			}
			base := ""
			if u.Content != nil {
				base = path.Base(u.Content.Path)
			}
			switch {
			case u.Form == "text" && u.Content != nil:
				narrative(u.Content.Bytes)
			case u.Form == "table" && u.Content != nil:
				out.Files["tables/"+base] = u.Content.Bytes
				blocks = append(blocks, ReportBlock{Type: "table", File: "tables/" + base, Caption: cp})
			case u.Form == "image" && u.Content != nil:
				out.Files["images/"+base] = u.Content.Bytes
				blocks = append(blocks, ReportBlock{Type: "image", File: "images/" + base, Caption: cp})
			case u.Form == "diagram" && u.Content != nil:
				out.Files["diagrams/"+base] = u.Content.Bytes
				blocks = append(blocks, ReportBlock{Type: "diagram", File: "diagrams/" + base, Caption: cp})
			case u.Form == "table" && ans != nil:
				if len(ans.Results.Bindings) == 0 && u.Empty == "omit" {
					continue
				}
				t, err := queryTable(u, ans)
				if err != nil {
					return nil, fmt.Errorf("fill: %s: %v", u.Key, err)
				}
				out.Files["tables/"+stem+".json"] = t
				blocks = append(blocks, ReportBlock{Type: "table", File: "tables/" + stem + ".json", Caption: cp})
			case u.Form == "tree" && ans != nil:
				if len(ans.Results.Bindings) == 0 && u.Empty == "omit" {
					continue
				}
				t, err := queryTree(u, ans)
				if err != nil {
					return nil, fmt.Errorf("fill: %s: %v", u.Key, err)
				}
				out.Files["diagrams/"+stem+".json"] = t
				empty := u.Caption
				blocks = append(blocks, ReportBlock{Type: "image", File: "images/" + stem + ".png", Caption: &empty,
					Source: "diagrams/" + stem + ".json"})
			case u.Form == "number" && ans != nil:
				narrative(numberLine(u, ans))
			default:
				out.Slots = append(out.Slots, SlotRecord{Use: u.Key, Part: id, Block: u.Block,
					Skipped: fmt.Sprintf("no report-v2 form for a %s use (shown live, not assembled)", u.Form)})
			}
		}
		if !s.Unnamed {
			rules := s.Rules
			if rules == nil {
				rules = []string{}
			}
			out.Report.Sections = append(out.Report.Sections, ReportSection{ID: id, Heading: s.Heading, Level: s.Level,
				Blocks: blocks, Rules: rules})
		}
	}
	return out, nil
}

func (q *QueryRef) where() string {
	if q.Input != "" {
		return q.Input
	}
	return q.Package
}

// queryTable writes a query's rows as a report-v2 table, as zusammensetzen does.
func queryTable(u ComposedUse, a *Answer) ([]byte, error) {
	// a copy: decoding into the answer's own slice would overwrite its variable names
	columns := append([]string(nil), a.Head.Vars...)
	if raw, ok := u.Options["columns"]; ok {
		columns = nil
		if err := json.Unmarshal(raw, &columns); err != nil {
			return nil, fmt.Errorf("columns: %v", err)
		}
	}
	rows := [][]string{}
	for _, b := range a.Results.Bindings {
		row := make([]string, len(a.Head.Vars))
		for i, v := range a.Head.Vars {
			row[i] = b[v].Text()
		}
		rows = append(rows, row)
	}
	return pyJSON(struct {
		Columns    []string   `json:"columns"`
		Rows       [][]string `json:"rows"`
		BoldHeader bool       `json:"boldHeader"`
	}{columns, rows, true})
}

// queryTree writes an organisation tree's nodes; render draws it (zusammensetzen's mermaid-tree).
func queryTree(u ComposedUse, a *Answer) ([]byte, error) {
	o := map[string]string{"node": "node", "label": "label", "holder": "holder", "parent": "parent"}
	for k := range o {
		if raw, ok := u.Options[k]; ok {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				o[k] = s
			}
		}
	}
	type node struct {
		ID     string `json:"id"`
		Label  string `json:"label"`
		Holder string `json:"holder"`
		Parent string `json:"parent"`
	}
	nodes := []node{}
	for _, b := range a.Results.Bindings {
		nodes = append(nodes, node{ID: b[o["node"]].Value, Label: b[o["label"]].Text(), Holder: b[o["holder"]].Text(),
			Parent: b[o["parent"]].Value})
	}
	return pyJSON(struct {
		Nodes []node `json:"nodes"`
	}{nodes})
}

func numberLine(u ComposedUse, a *Answer) []byte {
	v := ""
	name := ""
	if raw, ok := u.Options["var"]; ok {
		_ = json.Unmarshal(raw, &name)
	}
	if name == "" && len(a.Head.Vars) > 0 {
		name = a.Head.Vars[0]
	}
	if len(a.Results.Bindings) > 0 {
		v = a.Results.Bindings[0][name].Text()
	}
	label := ""
	if raw, ok := u.Options["label"]; ok {
		_ = json.Unmarshal(raw, &label)
	}
	return []byte(strings.TrimSpace("**"+v+"** "+label) + "\n")
}

// pyJSON writes JSON as Python's json.dumps(indent=2, ensure_ascii=False) does: two-space indent,
// no HTML escaping, no trailing newline.
func pyJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// ReportJSON writes the report as zusammensetzen writes report.json.
func (f *Filled) ReportJSON() ([]byte, error) { return pyJSON(f.Report) }

// SlotsJSON writes slots.json.
func (f *Filled) SlotsJSON() ([]byte, error) { return pyJSON(f.Slots) }
