// Package gitread reads Gitea over HTTP for the agent runtime: the head of a
// branch (what git itself advertises), and the tree id of a commit.
package gitread

import (
	"bytes"
	"context"
	gitsha1 "crypto/sha1" // #nosec G505 -- git object ids are sha1
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Access is where Gitea is and the read-only credential to read it with.
type Access struct {
	Base string // e.g. http://gitea.cicd.svc.cluster.local:3000 (no /api/v1)
	Org  string
	User string
	Pass string
}

// TreeEntry is one entry of a Gitea tree answer.
type TreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

var (
	Name     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	CommitID = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// BranchHead reads the commit a branch of <org>/<repo> points at, from
// the refs git itself advertises over smart HTTP (info/refs, what ls-remote
// reads): the view agentd's push had. Gitea's branches API answered 404 for a
// branch pushed the same second (10-15 on STG, attempt a1 of M-1's task), and
// a done result was refused for it. A branch that is not in a whole
// advertisement of that repository is a refused result (eventlog.ErrState); a
// repository it cannot see (a wrong organization, a credential that does not
// reach it), an answer that is not an advertisement, or a Gitea it cannot
// reach is an error of the platform, which agentd retries later, so no result
// is lost to it.
func BranchHead(access func(context.Context) (Access, error), org string, client *http.Client) func(context.Context, string, string) (string, error) {
	// A redirect would read another repository than the one asked for: it is
	// an answer of its own, an error of the platform (review 194 A2).
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &c
	return func(ctx context.Context, repo, ref string) (string, error) {
		branch, ok := strings.CutPrefix(ref, "refs/heads/")
		if !ok || branch == "" || !Name.MatchString(repo) || strings.HasSuffix(repo, ".git") {
			return "", fmt.Errorf("%w: %q of %q is not a branch of a repository", eventlog.ErrState, ref, repo)
		}
		for _, seg := range strings.Split(branch, "/") {
			if seg == "" || seg == "." || seg == ".." {
				return "", fmt.Errorf("%w: %q is not a branch name", eventlog.ErrState, ref)
			}
		}
		body, err := readAdvertisement(ctx, access, org, repo, client)
		if err != nil {
			return "", err
		}
		commit, found, err := advertisedRef(body, ref)
		if err != nil {
			return "", fmt.Errorf("gitea: %s/%s refs: %w", org, repo, err)
		}
		if !found {
			return "", fmt.Errorf("%w: %s is not on %s/%s", eventlog.ErrState, ref, org, repo)
		}
		return commit, nil
	}
}

// readAdvertisement is <org>/<repo>'s smart-HTTP advertisement of
// git-upload-pack (info/refs), whole. client must not follow redirects.
func readAdvertisement(ctx context.Context, access func(context.Context) (Access, error), org, repo string,
	client *http.Client) ([]byte, error) {
	acc, err := access(ctx)
	if err != nil {
		return nil, fmt.Errorf("gitea access: %w", err)
	}
	u := strings.TrimRight(acc.Base, "/") + "/" + url.PathEscape(org) + "/" + url.PathEscape(repo) +
		".git/info/refs?service=git-upload-pack"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if acc.User != "" || acc.Pass != "" {
		req.SetBasicAuth(acc.User, acc.Pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitea: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Gitea answers 401 or 404 for a repository it does not show this
	// credential; only an advertisement of the one asked for refuses
	// (review 194 A1).
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitea: %s/%s refs: HTTP %d", org, repo, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != uploadPackAdvertisement {
		return nil, fmt.Errorf("gitea: %s/%s refs: %q is not an upload-pack advertisement", org, repo, ct)
	}
	// The whole body, read to its end, or none of it (review 204 A1): an
	// answer cut short or longer than the bound is not an advertisement.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAdvertisement+1))
	if err != nil {
		return nil, fmt.Errorf("gitea: %s/%s refs: %w", org, repo, err)
	}
	if len(body) > maxAdvertisement {
		return nil, fmt.Errorf("gitea: %s/%s refs: more than %d bytes", org, repo, maxAdvertisement)
	}
	return body, nil
}

const (
	uploadPackAdvertisement = "application/x-git-upload-pack-advertisement"
	// maxAdvertisement bounds what is read: an agent repository's refs are
	// its branches, far below this; more is an error, not a refusal.
	maxAdvertisement = 16 << 20
)

// advertisedRef reads a smart-HTTP advertisement of git-upload-pack (protocol
// v0: the service line, a flush, the refs, a flush, and nothing after) and
// returns the object ref points at. A ref is not there only when a whole,
// well-formed advertisement leaves it out; one cut off, followed by more, or
// with a line that is not a ref (review 204 A2) is an error.
func advertisedRef(body []byte, ref string) (string, bool, error) {
	refs, err := advertisedRefs(body)
	if err != nil {
		return "", false, err
	}
	oid, found := refs[ref]
	return oid, found, nil
}

// advertisedRefs is every ref of a whole, well-formed advertisement and the
// object it points at (advertisedRef's checks, all of them).
func advertisedRefs(body []byte) (map[string]string, error) {
	// No buffer between the body and the parser: what is left after the last
	// flush is what the body has left (review 206 A1).
	br := bytes.NewReader(body)
	line, flush, err := pktLine(br)
	if err != nil {
		return nil, err
	}
	if flush || strings.TrimSuffix(string(line), "\n") != "# service=git-upload-pack" {
		return nil, errors.New("not an upload-pack advertisement")
	}
	if _, flush, err = pktLine(br); err != nil || !flush {
		return nil, errors.Join(errors.New("no flush after the service line"), err)
	}
	refs := map[string]string{}
	empty, seen, prev := false, map[string]bool{}, ""
	for n := 0; ; n++ {
		line, flush, err := pktLine(br)
		if err != nil {
			return nil, err
		}
		if flush {
			// No refs at all is an empty repository as git before 2.41 (the
			// CI image's 2.39) advertises it; later git sends the one
			// capabilities^{} line instead.
			if br.Len() > 0 {
				return nil, errors.New("more after the last flush")
			}
			return refs, nil
		}
		text := strings.TrimSuffix(string(line), "\n")
		if n == 0 { // the first ref carries the capabilities after a NUL
			text, _, _ = strings.Cut(text, "\x00")
		}
		oid, name, ok := strings.Cut(text, " ")
		// An empty repository's line is its only one (review 206 A2).
		if !ok || !CommitID.MatchString(oid) || !advertisedName(name, oid, n) || seen[name] || empty {
			return nil, fmt.Errorf("not a ref line: %q", line)
		}
		// git writes a peeled line right after the ref it peels.
		if peeled, ok := strings.CutSuffix(name, "^{}"); ok && name != "capabilities^{}" && peeled != prev {
			return nil, fmt.Errorf("a peeled line not after its ref: %q", line)
		}
		seen[name], prev = true, name
		empty = name == "capabilities^{}"
		if name != "capabilities^{}" && !strings.HasSuffix(name, "^{}") {
			refs[name] = oid
		}
	}
}

// advertisedName is whether name may stand in an advertisement with oid, at
// line n of its refs: HEAD; a ref by git's rules (check-ref-format); the
// peeled line (^{}) of an annotated tag, which any ref may point at (review
// 206 A2: refs/pull/9/head^{} from real git); or the one line of an empty
// repository. Only that line has the zero object id.
func advertisedName(name, oid string, n int) bool {
	zero := oid == strings.Repeat("0", 40)
	switch {
	case name == "capabilities^{}":
		return zero && n == 0
	case zero:
		return false
	case name == "HEAD":
		return true
	}
	if peeled, ok := strings.CutSuffix(name, "^{}"); ok {
		// HEAD itself may point at an annotated tag (review 207 B).
		return peeled == "HEAD" || strings.HasPrefix(peeled, "refs/") && refName(peeled)
	}
	return strings.HasPrefix(name, "refs/") && refName(name)
}

// refName holds name to git's rules for a ref name (git check-ref-format).
func refName(name string) bool {
	if name == "" || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") ||
		strings.Contains(name, "..") || strings.Contains(name, "@{") || name == "@" {
		return false
	}
	for _, c := range name {
		if c < 0x20 || c == 0x7f || strings.ContainsRune(" ~^:?*[\\", c) {
			return false
		}
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || strings.HasPrefix(seg, ".") || strings.HasSuffix(seg, ".lock") {
			return false
		}
	}
	return true
}

// pktLine reads one pkt-line: its payload, or flush for "0000".
func pktLine(r io.Reader) ([]byte, bool, error) {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return nil, false, fmt.Errorf("the advertisement is cut off: %w", err)
	}
	n, err := strconv.ParseUint(string(size[:]), 16, 16)
	switch {
	case err != nil:
		return nil, false, fmt.Errorf("pkt-line length %q", size[:])
	case n == 0:
		return nil, true, nil
	case n <= 4 || n > 65520:
		return nil, false, fmt.Errorf("pkt-line length %d", n)
	}
	payload := make([]byte, n-4)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, false, fmt.Errorf("the advertisement is cut off: %w", err)
	}
	return payload, false, nil
}

// CommitTree reads the git tree of a commit of <org>/<repo> from Gitea,
// with the delivery service's read-only credential. Gitea names no tree id
// for a commit (the tree sha of its commit and trees answers is the commit's
// own id, seen on STG 2026-11-13), so the id is computed as git does, from
// the entries of the commit's root tree.
func CommitTree(access func(context.Context) (Access, error), client *http.Client) func(context.Context, string, string) (string, error) {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return func(ctx context.Context, repo, commit string) (string, error) {
		if !Name.MatchString(repo) || strings.HasSuffix(repo, ".git") || !CommitID.MatchString(commit) {
			return "", fmt.Errorf("gitea: %q of %q is not a commit of a repository", commit, repo)
		}
		acc, err := access(ctx)
		if err != nil {
			return "", fmt.Errorf("gitea access: %w", err)
		}
		base := strings.TrimRight(acc.Base, "/") + "/api/v1/repos/" + url.PathEscape(acc.Org) + "/" + url.PathEscape(repo) + "/git/trees/" + commit
		var entries []TreeEntry
		for page := 1; ; page++ {
			req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s?per_page=1000&page=%d", base, page), nil)
			if reqErr != nil {
				return "", reqErr
			}
			if acc.User != "" || acc.Pass != "" {
				req.SetBasicAuth(acc.User, acc.Pass)
			}
			var doc struct {
				Total *int        `json:"total_count"`
				Tree  []TreeEntry `json:"tree"`
			}
			err = func() error {
				resp, doErr := c.Do(req)
				if doErr != nil {
					return doErr
				}
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("HTTP %d", resp.StatusCode)
				}
				return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&doc)
			}()
			if err != nil || doc.Total == nil {
				return "", fmt.Errorf("gitea: %s/%s tree of %s: %v", acc.Org, repo, commit, err)
			}
			entries = append(entries, doc.Tree...)
			if len(entries) == *doc.Total {
				break
			}
			if len(doc.Tree) == 0 || len(entries) > *doc.Total || page >= 100 {
				return "", fmt.Errorf("gitea: %s/%s tree of %s: %d entries read of %d", acc.Org, repo, commit, len(entries), *doc.Total)
			}
		}
		tree, err := TreeID(entries)
		if err != nil {
			return "", fmt.Errorf("gitea: %s/%s tree of %s: %w", acc.Org, repo, commit, err)
		}
		return tree, nil
	}
}

// TreeID is the id git gives a tree with these entries (one directory,
// not its subtrees): the sha1 of "tree <size>\0" and, for each entry in git's
// order (by name, a directory as if its name ended in "/"), "<mode> <name>\0"
// and the 20 bytes of its id.
func TreeID(entries []TreeEntry) (string, error) {
	type entry struct {
		key, mode, name string
		id              []byte
	}
	list := make([]entry, 0, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {
		id, err := hex.DecodeString(e.SHA)
		mode := strings.TrimLeft(e.Mode, "0")
		ok := map[string]bool{"blob 100644": true, "blob 100755": true, "blob 120000": true, "tree 40000": true, "commit 160000": true}[e.Type+" "+mode]
		if err != nil || len(id) != 20 || !ok || e.Path == "" || strings.ContainsAny(e.Path, "/\x00") || seen[e.Path] {
			return "", fmt.Errorf("invalid tree entry %q", e.Path)
		}
		seen[e.Path] = true
		key := e.Path
		if e.Type == "tree" {
			key += "/"
		}
		list = append(list, entry{key, mode, e.Path, id})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].key < list[j].key })
	var body bytes.Buffer
	for _, e := range list {
		body.WriteString(e.mode + " " + e.name + "\x00")
		body.Write(e.id)
	}
	h := gitsha1.New() // #nosec G401 -- git's object id, not a security hash
	_, _ = fmt.Fprintf(h, "tree %d\x00", body.Len())
	h.Write(body.Bytes())
	return hex.EncodeToString(h.Sum(nil)), nil
}
