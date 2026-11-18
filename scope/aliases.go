package scope

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The alias file of a vocabulary (kton.dev/aliases/v0): the prefixes, terms and template short
// names its templates use. It is its own part of a revision, not a template.
const (
	RoleAliases   = "aliases"
	AliasFile     = "aliases.json"
	AliasesSchema = "https://kton.dev/aliases/v0"
)

// ParseAliases checks that body is a kton.dev/aliases/v0 file: a JSON object whose schema, when
// given, is AliasesSchema, and whose prefixes, terms and templates are maps of non-empty names to
// strings. Other top-level fields (a note) are allowed, as nekton allows them.
func ParseAliases(body []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return fmt.Errorf("alias file: %w", err)
	}
	if raw, ok := top["schema"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || s != AliasesSchema {
			return fmt.Errorf("alias file: schema %s, want %q", bytes.TrimSpace(raw), AliasesSchema)
		}
	}
	for _, k := range []string{"prefixes", "terms", "templates"} {
		raw, ok := top[k]
		if !ok || string(bytes.TrimSpace(raw)) == "null" {
			continue
		}
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err != nil {
			return fmt.Errorf("alias file: %s must map names to strings: %w", k, err)
		}
		for name, v := range m {
			if name == "" || v == "" {
				return fmt.Errorf("alias file: %s has an empty name or value (%q: %q)", k, name, v)
			}
		}
	}
	return nil
}

// AddAliases lays the alias file into the revision, after checking that it parses.
func (r *Revision) AddAliases(body []byte) (string, error) {
	if err := ParseAliases(body); err != nil {
		return "", err
	}
	h, err := r.P.PutBlob(body)
	if err != nil {
		return "", err
	}
	if _, err := r.Claim(h, PartOf, member(RoleAliases, nil)); err != nil {
		return "", err
	}
	return h, nil
}

// Aliases are the bytes of the revision's alias file, nil when it has none. A revision with more
// than one alias file is an error: which one counts would be a guess.
func (r *Revision) Aliases() ([]byte, error) {
	claims, err := r.Claims()
	if err != nil {
		return nil, err
	}
	var hashes []string
	for _, c := range claims {
		if c.Predicate == PartOf && str(value(c)["role"]) == RoleAliases {
			hashes = append(hashes, c.Subject)
		}
	}
	switch len(hashes) {
	case 0:
		return nil, nil
	case 1:
		return r.P.Blob(hashes[0])
	default:
		return nil, fmt.Errorf("revision %s has more than one alias file", r.Label)
	}
}

// addTemplateOrAliases is the put for a templates folder: aliases.json there is the alias file.
func (r *Revision) addTemplateOrAliases(name string, body []byte) (string, error) {
	if name+".json" == AliasFile {
		return r.AddAliases(body)
	}
	return r.AddTemplate(name, body)
}
