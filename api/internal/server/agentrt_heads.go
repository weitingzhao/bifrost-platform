package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// The result check's Gitea half (design v5 §5.2: a result is persisted once
// its branch is on Gitea and the platform has checked it). Agent branches go
// to their own organization on Gitea, AGENTRT_GIT_ORG: the organization the
// releases clone from mirrors GitHub, takes no push and drops branches GitHub
// does not have. Unset, there is no reader and a pushed branch answers 503
// (store.ErrNoHead), as before.

// agentRuntimeHeads is the branch reader of this process, or nil.
func (s *Server) agentRuntimeHeads() store.BranchHead {
	org := strings.TrimSpace(os.Getenv("AGENTRT_GIT_ORG"))
	if org == "" || s.delivery == nil {
		return nil
	}
	return giteaBranchHead(s.delivery.GiteaAccess, org, &http.Client{Timeout: 10 * time.Second})
}

var (
	giteaName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	commitID  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// giteaBranchHead reads the commit a branch of <org>/<repo> points at, from
// the refs git itself advertises over smart HTTP (info/refs, what ls-remote
// reads): the view agentd's push had. Gitea's branches API answered 404 for a
// branch pushed the same second (10-15 on STG, attempt a1 of M-1's task), and
// a done result was refused for it. A branch that is not in a whole
// advertisement of that repository is a refused result (store.ErrState); a
// repository it cannot see (a wrong organization, a credential that does not
// reach it), an answer that is not an advertisement, or a Gitea it cannot
// reach is an error of the platform, which agentd retries later, so no result
// is lost to it.
func giteaBranchHead(access func(context.Context) (delivery.GiteaAccess, error), org string, client *http.Client) store.BranchHead {
	// A redirect would read another repository than the one asked for: it is
	// an answer of its own, an error of the platform (review 194 A2).
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &c
	return func(ctx context.Context, repo, ref string) (string, error) {
		branch, ok := strings.CutPrefix(ref, "refs/heads/")
		if !ok || branch == "" || !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") {
			return "", fmt.Errorf("%w: %q of %q is not a branch of a repository", store.ErrState, ref, repo)
		}
		for _, seg := range strings.Split(branch, "/") {
			if seg == "" || seg == "." || seg == ".." {
				return "", fmt.Errorf("%w: %q is not a branch name", store.ErrState, ref)
			}
		}
		acc, err := access(ctx)
		if err != nil {
			return "", fmt.Errorf("gitea access: %w", err)
		}
		u := strings.TrimRight(acc.Base, "/") + "/" + url.PathEscape(org) + "/" + url.PathEscape(repo) +
			".git/info/refs?service=git-upload-pack"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", err
		}
		if acc.User != "" || acc.Pass != "" {
			req.SetBasicAuth(acc.User, acc.Pass)
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("gitea: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		// Gitea answers 401 or 404 for a repository it does not show this
		// credential; only an advertisement of the one asked for refuses
		// (review 194 A1).
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("gitea: %s/%s refs: HTTP %d", org, repo, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != uploadPackAdvertisement {
			return "", fmt.Errorf("gitea: %s/%s refs: %q is not an upload-pack advertisement", org, repo, ct)
		}
		commit, found, err := advertisedRef(io.LimitReader(resp.Body, maxAdvertisement), ref)
		if err != nil {
			return "", fmt.Errorf("gitea: %s/%s refs: %w", org, repo, err)
		}
		if !found {
			return "", fmt.Errorf("%w: %s is not on %s/%s", store.ErrState, ref, org, repo)
		}
		return commit, nil
	}
}

const (
	uploadPackAdvertisement = "application/x-git-upload-pack-advertisement"
	// maxAdvertisement bounds what is read: an agent repository's refs are
	// its branches, far below this; more is an error, not a refusal.
	maxAdvertisement = 16 << 20
)

// advertisedRef reads a smart-HTTP advertisement of git-upload-pack (protocol
// v0: the service line, a flush, the refs, a flush) and returns the object
// ref points at. It reads the advertisement to its end: a ref is not there
// only when a whole advertisement leaves it out; a cut-off or malformed one
// is an error.
func advertisedRef(r io.Reader, ref string) (string, bool, error) {
	br := bufio.NewReader(r)
	line, flush, err := pktLine(br)
	if err != nil {
		return "", false, err
	}
	if flush || strings.TrimSuffix(string(line), "\n") != "# service=git-upload-pack" {
		return "", false, errors.New("not an upload-pack advertisement")
	}
	if _, flush, err = pktLine(br); err != nil || !flush {
		return "", false, errors.Join(errors.New("no flush after the service line"), err)
	}
	var got string
	found := false
	for first := true; ; first = false {
		line, flush, err := pktLine(br)
		if err != nil {
			return "", false, err
		}
		if flush {
			return got, found, nil
		}
		if first { // the first ref carries the capabilities after a NUL
			line, _, _ = bytes.Cut(line, []byte{0})
		}
		oid, name, ok := strings.Cut(strings.TrimSuffix(string(line), "\n"), " ")
		if !ok || !commitID.MatchString(oid) || name == "" {
			return "", false, fmt.Errorf("malformed ref line %q", line)
		}
		if name == ref {
			if found {
				return "", false, fmt.Errorf("%s advertised twice", ref)
			}
			got, found = oid, true
		}
	}
}

// pktLine reads one pkt-line: its payload, or flush for "0000".
func pktLine(r *bufio.Reader) ([]byte, bool, error) {
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
