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

// TemplateAnswers reads a template's "answers" field: the name of the template whose claims this
// template's claims answer (an answer claim is about the request claim, nk:about), or "" when it
// answers none. A capture-result template answers capture-requested; the request and its answers
// make a job a host can follow without knowing the package.
func TemplateAnswers(template []byte) (string, error) {
	var tpl map[string]json.RawMessage
	if err := json.Unmarshal(template, &tpl); err != nil {
		return "", fmt.Errorf("the template is not a JSON object: %v", err)
	}
	raw, ok := tpl["answers"]
	if !ok {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || s == "" {
		return "", fmt.Errorf("answers is %s; it is the name of a template of the package", string(raw))
	}
	return s, nil
}

// TemplateProblems checks what a template says about other templates: answers names another
// template of the same revision.
func (r *Revision) TemplateProblems() []string {
	ts, err := r.Templates()
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	for _, n := range sortedKeys(ts) {
		a, err := TemplateAnswers(ts[n])
		switch {
		case err != nil:
			out = append(out, fmt.Sprintf("template %q: %v", n, err))
		case a == n:
			out = append(out, fmt.Sprintf("template %q answers itself", n))
		case a != "":
			if _, ok := ts[a]; !ok {
				out = append(out, fmt.Sprintf("template %q answers %q, which is not a template of the package", n, a))
			}
		}
	}
	return out
}
