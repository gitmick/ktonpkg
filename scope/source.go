package scope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Where a revision comes from, and which packages it needs (ui-design's G7, for MH's architecture
// view). Both are claims about the revision, like its profile, so they are signed and sealed with
// it and a reader gets them from the revision's chain.

// SourcePredicate says what a revision was built from: prov:hadPrimarySource.
const SourcePredicate = "http://www.w3.org/ns/prov#hadPrimarySource"

// Source is the git state a revision was built from. Commit is the head; Path is the source folder
// inside the repository ("" for its root); Dirty says the folder had changes not in that commit, so
// the head alone does not give the bytes back (the revision's members still do). Repo is the remote
// the head can be fetched from, without any user or password; "" when there is none.
type Source struct {
	Repo   string `json:"repo,omitempty"`
	Commit string `json:"commit"`
	Path   string `json:"path,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`
}

var gitObjectID = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// Problems checks a source's form.
func (s Source) Problems() []string {
	var out []string
	if !gitObjectID.MatchString(s.Commit) {
		out = append(out, fmt.Sprintf("source commit %q is not a git object id (40 or 64 lower-case hex)", s.Commit))
	}
	if s.Path != "" {
		if s.Path != filepath.ToSlash(filepath.Clean(s.Path)) || strings.HasPrefix(s.Path, "/") || s.Path == "." ||
			s.Path == ".." || strings.HasPrefix(s.Path, "../") {
			out = append(out, fmt.Sprintf("source path %q is not a clean path inside the repository", s.Path))
		}
	}
	if s.Repo != "" {
		if u, err := url.Parse(s.Repo); err == nil && u.User != nil {
			out = append(out, "source repo carries a user or password")
		}
		if strings.ContainsAny(s.Repo, " \t\n\r") {
			out = append(out, fmt.Sprintf("source repo %q has white space", s.Repo))
		}
	}
	return out
}

// SetSource records the source of the revision. Only one: a second is refused.
func (r *Revision) SetSource(s Source) error {
	if probs := s.Problems(); len(probs) > 0 {
		return fmt.Errorf("%s", strings.Join(probs, "; "))
	}
	if old, err := r.Source(); err != nil {
		return err
	} else if old != nil {
		return fmt.Errorf("the revision %s already names its source", r.Label)
	}
	_, err := r.Claim(r.ID, SourcePredicate, map[string]any{"value": toMap(s)})
	return err
}

// Source is the source the revision names; nil when it names none (a revision built before v0.10,
// or from a folder outside git).
func (r *Revision) Source() (*Source, error) {
	claims, err := r.Claims()
	if err != nil {
		return nil, err
	}
	for _, c := range claims {
		if c.Predicate == SourcePredicate && c.Subject == r.ID {
			var s Source
			b, _ := json.Marshal(c.Object["value"])
			if err := json.Unmarshal(b, &s); err != nil {
				return nil, fmt.Errorf("the source claim %s: %v", short(c.ID), err)
			}
			return &s, nil
		}
	}
	return nil, nil
}

// GitSource reads the git state of a source folder: the head commit, the folder's path in the
// repository, whether it has changes (tracked or untracked) not in the head, and the remote origin
// without credentials. nil, nil when the folder is not in a git work tree or git is not there.
func GitSource(dir string) (*Source, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, nil
	}
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
		}
		return strings.TrimSpace(out.String()), nil
	}
	if inside, err := git("rev-parse", "--is-inside-work-tree"); err != nil || inside != "true" {
		return nil, nil
	}
	head, err := git("rev-parse", "--verify", "HEAD")
	if err != nil {
		return nil, nil // a repository without a commit: nothing to name
	}
	prefix, err := git("rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	status, err := git("status", "--porcelain", "--untracked-files=all", "--", ".")
	if err != nil {
		return nil, err
	}
	s := &Source{Commit: head, Path: strings.TrimSuffix(prefix, "/"), Dirty: status != ""}
	if remote, err := git("config", "--get", "remote.origin.url"); err == nil {
		s.Repo = withoutCredentials(remote)
	}
	return s, nil
}

// withoutCredentials drops a user and password from a remote URL. An scp-like remote
// (git@host:path) keeps its form: its user is the ssh login, not a secret, but it is dropped too, so
// the same repository reads alike whoever built it.
func withoutCredentials(remote string) string {
	if u, err := url.Parse(remote); err == nil && u.Scheme != "" && u.Host != "" {
		u.User = nil
		return u.String()
	}
	if at := strings.Index(remote, "@"); at >= 0 && strings.Contains(remote[at:], ":") && !strings.Contains(remote[:at], "/") {
		return remote[at+1:]
	}
	return remote
}
