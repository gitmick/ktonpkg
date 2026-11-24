package scope

import (
	"encoding/json"
	"fmt"
	"strings"
)

// State is what a reader changes in a composition (W11): its vars' values and, per layout, the tab
// shown and the split ratio. It travels in the host's URL, so every value is untrusted until
// ValidateState has checked it. A nested composition's state is its own, under the part path of
// the part that nests it; Compose validates it when it reaches that part.
type State struct {
	Vars   map[string]json.RawMessage `json:"vars,omitempty"`
	Layout map[string]LayoutState     `json:"layout,omitempty"` // part path ("" = the composition) -> state
	Nested map[string]State           `json:"nested,omitempty"` // part path of a nesting part -> the child's state
}

// LayoutState is a reader's choice in one layout: the tab shown (tabs) or the first part's share
// (split).
type LayoutState struct {
	Active string  `json:"active,omitempty"`
	Ratio  float64 `json:"ratio,omitempty"`
}

// StateError says which value of a state was not taken and what was used instead.
type StateError struct {
	Key    string // "var.<name>" or "layout.<path>.active|ratio" or "nested.<path>"
	Reason string
}

func (e StateError) Error() string { return e.Key + ": " + e.Reason }

// PartPath is the key of a part in its composition: the ids from the top, joined by "/". An
// unnamed part is "#<n>", its 1-based position among its siblings, so every part has one key.
func PartPath(parent string, i int, p Part) string {
	seg := p.ID
	if seg == "" {
		seg = fmt.Sprintf("#%d", i+1)
	}
	if parent == "" {
		return seg
	}
	return parent + "/" + seg
}

// ValidateState checks a reader's state against a composition and returns the state compose may
// use, with every refused value reported. It never fails: a value of the wrong form or type falls
// back to the var's default, and a var without a default becomes unset (a use reading it waits,
// W9). In a report or deck nothing interacts, so every var holds its default and any value given
// is reported. A layout value that does not fit its layout falls back to the layout's own default.
// Nested states are kept by path, unchecked, for Compose to validate against the child.
func ValidateState(c Composition, in State) (State, []StateError) {
	out := State{Vars: map[string]json.RawMessage{}, Layout: map[string]LayoutState{}, Nested: map[string]State{}}
	var errs []StateError
	bad := func(key, format string, args ...any) {
		errs = append(errs, StateError{Key: key, Reason: fmt.Sprintf(format, args...)})
	}
	static := c.Kind == "report" || c.Kind == "deck"

	for _, name := range sortedKeys(in.Vars) {
		if _, ok := c.Vars[name]; !ok {
			bad("var."+name, "the composition declares no var %q; dropped", name)
		}
	}
	for _, name := range sortedKeys(c.Vars) {
		d := c.Vars[name]
		t, err := parseType(d.Type)
		if err != nil {
			bad("var."+name, "the declaration's type does not parse (%v); unset", err)
			continue
		}
		fallback := func(why string) {
			if len(d.Default) > 0 {
				out.Vars[name] = d.Default
				bad("var."+name, "%s; the default is used", why)
			} else {
				bad("var."+name, "%s; unset", why)
			}
		}
		v, given := in.Vars[name]
		switch {
		case given && static:
			fallback(fmt.Sprintf("a %s does not take state", c.Kind))
		case given && !conforms(v, t):
			fallback(fmt.Sprintf("%s is not a %s", short64(v), t))
		case given:
			out.Vars[name] = v
		case len(d.Default) > 0:
			out.Vars[name] = d.Default
		}
	}

	layouts := map[string]struct {
		l     *Layout
		parts []Part
	}{}
	if c.Layout != nil {
		layouts[""] = struct {
			l     *Layout
			parts []Part
		}{c.Layout, c.Parts}
	}
	nesting := map[string]bool{}
	var walk func(parent string, ps []Part)
	walk = func(parent string, ps []Part) {
		for i, p := range ps {
			path := PartPath(parent, i, p)
			if len(p.Composition) > 0 {
				nesting[path] = true
			}
			if p.Layout != nil {
				layouts[path] = struct {
					l     *Layout
					parts []Part
				}{p.Layout, p.Parts}
			}
			walk(path, p.Parts)
		}
	}
	walk("", c.Parts)

	for _, path := range sortedKeys(in.Layout) {
		ls := in.Layout[path]
		key := "layout." + path
		at, ok := layouts[path]
		if !ok {
			bad(key, "no layout at %q; dropped", path)
			continue
		}
		var keep LayoutState
		if ls.Active != "" {
			switch {
			case at.l.Type != "tabs":
				bad(key+".active", "a %s layout has no active tab; dropped", at.l.Type)
			case !hasPart(at.parts, ls.Active):
				bad(key+".active", "%q is not a part of this layout; the first tab is used", ls.Active)
			default:
				keep.Active = ls.Active
			}
		}
		if ls.Ratio != 0 {
			switch {
			case at.l.Type != "split":
				bad(key+".ratio", "a %s layout has no ratio; dropped", at.l.Type)
			case ls.Ratio <= 0 || ls.Ratio >= 1:
				bad(key+".ratio", "%v is not between 0 and 1; the layout's ratio is used", ls.Ratio)
			default:
				keep.Ratio = ls.Ratio
			}
		}
		if keep != (LayoutState{}) {
			out.Layout[path] = keep
		}
	}

	for _, path := range sortedKeys(in.Nested) {
		if !nesting[path] {
			bad("nested."+path, "no part at %q nests a composition; dropped", path)
			continue
		}
		out.Nested[path] = in.Nested[path]
	}
	return out, errs
}

func hasPart(ps []Part, id string) bool {
	for _, p := range ps {
		if p.ID == id {
			return true
		}
	}
	return false
}

// short64 shows a value in a message, cut to 64 bytes: the value comes from a URL.
func short64(v json.RawMessage) string {
	s := strings.TrimSpace(string(v))
	if len(s) > 64 {
		return s[:61] + "..."
	}
	return s
}
