package scope

import (
	"encoding/json"
	"fmt"
)

// What a template's claims are (ADR-018 §5, MH 20.11.): versions, where a newer claim about the same
// subject replaces an older one and "the latest per subject" is the current state (the default);
// or rows, where every claim is a row of its own (coverage, evidence) and none replaces another.
const (
	ClaimsVersions = "versions"
	ClaimsRows     = "rows"
)

// TemplateClaims reads a template's "claims" field: versions when it has none, rows or versions when
// it says so. Anything else is an error, so a host never guesses which kind a claim is.
func TemplateClaims(template []byte) (string, error) {
	var tpl map[string]json.RawMessage
	if err := json.Unmarshal(template, &tpl); err != nil {
		return "", fmt.Errorf("the template is not a JSON object: %v", err)
	}
	raw, ok := tpl["claims"]
	if !ok {
		return ClaimsVersions, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || (s != ClaimsVersions && s != ClaimsRows) {
		return "", fmt.Errorf("claims is %s; it is %q or %q", string(raw), ClaimsVersions, ClaimsRows)
	}
	return s, nil
}
