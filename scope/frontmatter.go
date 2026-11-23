package scope

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The frontmatter of qm-packages' dokument.md and kapitel/*.md: a small YAML subset, read strictly.
// Block mappings by indentation, '#' comments, plain and double-quoted scalars, integers, and flow
// collections ({k: v, …}, […]) that may continue over lines. Anything else is an error rather than
// a guess. Mappings keep their key order: slots are appended in the order a document declares them.

// ymap is a mapping in key order.
type ymap struct {
	keys []string
	vals map[string]any // string, int, []any, *ymap
}

func (m *ymap) get(k string) (any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m.vals[k]
	return v, ok
}

// str is a scalar as text: a number such as id: 10 is the text "10".
func (m *ymap) str(k string) string {
	v, _ := m.get(k)
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	}
	return ""
}

func (m *ymap) set(k string, v any) error {
	if _, dup := m.vals[k]; dup {
		return fmt.Errorf("the key %q appears twice", k)
	}
	m.keys = append(m.keys, k)
	m.vals[k] = v
	return nil
}

func newYmap() *ymap { return &ymap{vals: map[string]any{}} }

var frontRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?(.*)$`)

// splitFront splits a file into its frontmatter and its body. No frontmatter: an empty map.
func splitFront(text string) (*ymap, string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	m := frontRe.FindStringSubmatch(text)
	if m == nil {
		return newYmap(), text, nil
	}
	front, err := parseYAML(m[1])
	return front, m[2], err
}

type yline struct {
	indent int
	text   string
	n      int
}

func parseYAML(src string) (*ymap, error) {
	var lines []yline
	for i, raw := range strings.Split(src, "\n") {
		t := stripComment(raw)
		if strings.TrimSpace(t) == "" {
			continue
		}
		ind := len(t) - len(strings.TrimLeft(t, " "))
		if strings.HasPrefix(strings.TrimLeft(t, " "), "\t") {
			return nil, fmt.Errorf("line %d: a tab in the indentation", i+1)
		}
		lines = append(lines, yline{indent: ind, text: strings.TrimSpace(t), n: i + 1})
	}
	// a flow collection left open continues on the next lines
	var joined []yline
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		for depth(l.text) > 0 && i+1 < len(lines) {
			i++
			l.text += " " + lines[i].text
		}
		if depth(l.text) != 0 {
			return nil, fmt.Errorf("line %d: a flow collection is not closed", l.n)
		}
		joined = append(joined, l)
	}
	m, rest, err := parseBlock(joined, 0)
	if err != nil {
		return nil, err
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("line %d: unexpected indentation", rest[0].n)
	}
	return m, nil
}

// depth is how many brackets a line leaves open, outside quotes.
func depth(s string) int {
	d, q := 0, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && q:
			i++
		case c == '"':
			q = !q
		case !q && (c == '{' || c == '['):
			d++
		case !q && (c == '}' || c == ']'):
			d--
		}
	}
	return d
}

func stripComment(s string) string {
	q := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && q:
			i++
		case c == '"':
			q = !q
		case c == '#' && !q && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return s[:i]
		}
	}
	return s
}

func parseBlock(lines []yline, indent int) (*ymap, []yline, error) {
	m := newYmap()
	for len(lines) > 0 {
		l := lines[0]
		if l.indent < indent {
			return m, lines, nil
		}
		if l.indent > indent {
			return nil, nil, fmt.Errorf("line %d: unexpected indentation", l.n)
		}
		k, v, ok := cutKey(l.text)
		if !ok {
			return nil, nil, fmt.Errorf("line %d: not a key: value", l.n)
		}
		lines = lines[1:]
		if v == "" {
			if len(lines) == 0 || lines[0].indent <= indent {
				if err := m.set(k, ""); err != nil {
					return nil, nil, fmt.Errorf("line %d: %v", l.n, err)
				}
				continue
			}
			child, rest, err := parseBlock(lines, lines[0].indent)
			if err != nil {
				return nil, nil, err
			}
			if err := m.set(k, child); err != nil {
				return nil, nil, fmt.Errorf("line %d: %v", l.n, err)
			}
			lines = rest
			continue
		}
		val, rest, err := parseValue(v, false)
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %v", l.n, err)
		}
		if strings.TrimSpace(rest) != "" {
			return nil, nil, fmt.Errorf("line %d: unexpected %q", l.n, rest)
		}
		if err := m.set(k, val); err != nil {
			return nil, nil, fmt.Errorf("line %d: %v", l.n, err)
		}
	}
	return m, nil, nil
}

var keyRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*):(?:\s+(.*)|$)`)

func cutKey(s string) (string, string, bool) {
	m := keyRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	return m[1], strings.TrimSpace(m[2]), true
}

// parseFlow reads one value inside a flow collection, where , ] and } end a plain scalar.
func parseFlow(s string) (any, string, error) { return parseValue(s, true) }

// parseValue reads one value from the start of s and returns the rest. At block level a plain
// scalar runs to the end of the line, commas and brackets included, as in YAML.
func parseValue(s string, inFlow bool) (any, string, error) {
	s = strings.TrimLeft(s, " ")
	switch {
	case strings.HasPrefix(s, "{"):
		m := newYmap()
		s = strings.TrimLeft(s[1:], " ")
		for !strings.HasPrefix(s, "}") {
			k, rest, ok := strings.Cut(s, ":")
			k = strings.TrimSpace(k)
			if !ok || !keyRe.MatchString(k+":") {
				return nil, "", fmt.Errorf("a flow mapping key in %q", s)
			}
			v, rest, err := parseFlow(rest)
			if err != nil {
				return nil, "", err
			}
			if err := m.set(k, v); err != nil {
				return nil, "", err
			}
			s = strings.TrimLeft(rest, " ")
			if strings.HasPrefix(s, ",") {
				s = strings.TrimLeft(s[1:], " ")
			} else if !strings.HasPrefix(s, "}") {
				return nil, "", fmt.Errorf("a flow mapping goes on with %q", s)
			}
		}
		return m, s[1:], nil
	case strings.HasPrefix(s, "["):
		xs := []any{}
		s = strings.TrimLeft(s[1:], " ")
		for !strings.HasPrefix(s, "]") {
			v, rest, err := parseFlow(s)
			if err != nil {
				return nil, "", err
			}
			xs = append(xs, v)
			s = strings.TrimLeft(rest, " ")
			if strings.HasPrefix(s, ",") {
				s = strings.TrimLeft(s[1:], " ")
			} else if !strings.HasPrefix(s, "]") {
				return nil, "", fmt.Errorf("a flow sequence goes on with %q", s)
			}
		}
		return xs, s[1:], nil
	case strings.HasPrefix(s, `"`):
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				if i+1 >= len(s) {
					return nil, "", fmt.Errorf("an escape at the end of %q", s)
				}
				i++
				switch s[i] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				case '"', '\\', '/':
					b.WriteByte(s[i])
				default:
					return nil, "", fmt.Errorf("the escape \\%c is not read", s[i])
				}
			case '"':
				return b.String(), s[i+1:], nil
			default:
				b.WriteByte(s[i])
			}
		}
		return nil, "", fmt.Errorf("a quoted string is not closed: %q", s)
	}
	// a plain scalar runs to the end of the line; inside a flow collection to , ] or }
	end := len(s)
	if inFlow {
		if i := strings.IndexAny(s, ",]}"); i >= 0 {
			end = i
		}
	}
	v := strings.TrimSpace(s[:end])
	if v == "" {
		return nil, "", fmt.Errorf("an empty value in %q", s)
	}
	if strings.ContainsAny(v[:1], "'&*!|>%@`") {
		return nil, "", fmt.Errorf("the value %q uses YAML this reader does not read", v)
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n, s[end:], nil
	}
	return v, s[end:], nil
}
