package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

const (
	headSHA  = "0123456789abcdef0123456789abcdef01234567"
	otherSHA = "89abcdef0123456789abcdef0123456789abcdef"
	zeroSHA  = "0000000000000000000000000000000000000000"
)

// pkt is one pkt-line.
func pkt(s string) string { return fmt.Sprintf("%04x", len(s)+4) + s }

// advert is what Gitea's info/refs answers for these "<oid> <ref>" lines: the
// service line, a flush, the refs (the first with capabilities after a NUL),
// a flush. No refs is an empty repository's advertisement.
func advert(refs ...string) string {
	var b strings.Builder
	b.WriteString(pkt("# service=git-upload-pack\n") + "0000")
	if len(refs) == 0 {
		refs = []string{zeroSHA + " capabilities^{}"}
	}
	for i, r := range refs {
		if i == 0 {
			r += "\x00multi_ack thin-pack side-band side-band-64k ofs-delta agent=git/2.43.0"
		}
		b.WriteString(pkt(r + "\n"))
	}
	b.WriteString("0000")
	return b.String()
}

func agentsRepo(body string) map[string]string {
	return map[string]string{"bifrost-agents/bifrost-platform": body}
}

// fakeGitea answers info/refs of the repositories it has, by escaped path.
type fakeGitea struct {
	mu          sync.Mutex
	adverts     map[string]string // "org/repo" -> the body of its info/refs
	contentType string            // when set, instead of the advertisement's
	redirect    map[string]string // escaped path -> where a 302 sends it
	status      int               // when set, every request gets it
	extraLength int               // declared beyond the body: the answer is cut short
	paths       []string
	protocols   []string // Git-Protocol headers sent
	queries     []string
	auth        []string
}

func (f *fakeGitea) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.EscapedPath())
	f.queries = append(f.queries, r.URL.RawQuery)
	f.protocols = append(f.protocols, r.Header.Get("Git-Protocol"))
	u, p, _ := r.BasicAuth()
	f.auth = append(f.auth, u+":"+p)
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	if to, ok := f.redirect[r.URL.EscapedPath()]; ok {
		http.Redirect(w, r, to, http.StatusFound)
		return
	}
	repo, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.EscapedPath(), "/"), ".git/info/refs")
	body, known := f.adverts[repo]
	if !ok || !known || r.URL.Query().Get("service") != "git-upload-pack" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	ct := uploadPackAdvertisement
	if f.contentType != "" {
		ct = f.contentType
	}
	w.Header().Set("Content-Type", ct)
	if f.extraLength > 0 {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)+f.extraLength))
	}
	_, _ = w.Write([]byte(body))
}

func headReader(t *testing.T, f *fakeGitea) store.BranchHead {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	access := func(context.Context) (delivery.GiteaAccess, error) {
		return delivery.GiteaAccess{Base: srv.URL + "/", Org: "bifrost", User: "reader", Pass: "secret"}, nil
	}
	return giteaBranchHead(access, "bifrost-agents", srv.Client())
}

func TestGiteaBranchHeadReadsTheAgentOrganization(t *testing.T) {
	f := &fakeGitea{adverts: agentsRepo(advert(otherSHA+" HEAD", otherSHA+" refs/heads/main", headSHA+" refs/heads/agent/T-7/a1"))}
	got, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1")
	if err != nil || got != headSHA {
		t.Fatalf("head = %q, %v; want %s", got, err, headSHA)
	}
	// The agent organization, not the delivery one (a pull mirror), git's
	// own advertisement (10-15: the branches API lagged a push), and the
	// delivery service's credential.
	if want := "/bifrost-agents/bifrost-platform.git/info/refs"; len(f.paths) != 1 || f.paths[0] != want {
		t.Fatalf("paths = %v, want [%s]", f.paths, want)
	}
	if f.queries[0] != "service=git-upload-pack" || f.auth[0] != "reader:secret" {
		t.Fatalf("query = %q, auth = %q", f.queries[0], f.auth[0])
	}
	// No Git-Protocol header: the answer is protocol v0, the one parsed.
	if f.protocols[0] != "" {
		t.Fatalf("Git-Protocol = %q", f.protocols[0])
	}
}

// The first ref of an advertisement carries the capabilities after a NUL: a
// repository whose only ref is the branch still reads.
func TestGiteaBranchHeadReadsTheFirstRef(t *testing.T) {
	f := &fakeGitea{adverts: agentsRepo(advert(headSHA + " refs/heads/agent/T-7/a1"))}
	if got, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1"); err != nil || got != headSHA {
		t.Fatalf("head = %q, %v; want %s", got, err, headSHA)
	}
}

// The advertisement real git writes, as Gitea serves it: git upload-pack's
// own refs after the service line, with HEAD, a peeled tag and branches that
// share a prefix.
func TestGiteaBranchHeadReadsWhatGitAdvertises(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t",
			"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
		cmd.Env = append(cmd.Environ(), "GIT_PROTOCOL=", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "one")
	git("tag", "-a", "-m", "v1", "v1")
	git("branch", "agent/T-7/a10")
	git("commit", "-q", "--allow-empty", "-m", "two")
	git("branch", "agent/T-7/a1")
	want := git("rev-parse", "agent/T-7/a1")
	refs := git("upload-pack", "--advertise-refs", "--stateless-rpc", dir)
	f := &fakeGitea{adverts: agentsRepo(pkt("# service=git-upload-pack\n") + "0000" + refs)}
	head := headReader(t, f)
	if got, err := head(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1"); err != nil || got != want {
		t.Fatalf("head = %q, %v; want %s", got, err, want)
	}
	if _, err := head(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a2"); !errors.Is(err, store.ErrState) {
		t.Fatalf("a branch git does not advertise: err = %v, want ErrState", err)
	}
	// An empty repository as git advertises it.
	dir = t.TempDir()
	git("init", "-q", "-b", "main")
	f = &fakeGitea{adverts: agentsRepo(pkt("# service=git-upload-pack\n") + "0000" + git("upload-pack", "--advertise-refs", "--stateless-rpc", dir))}
	if _, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1"); !errors.Is(err, store.ErrState) {
		t.Fatalf("an empty repository: err = %v, want ErrState", err)
	}
}

func TestGiteaBranchHeadRefusesAMissingBranch(t *testing.T) {
	for name, body := range map[string]string{
		"other branches":   advert(otherSHA+" refs/heads/main", otherSHA+" refs/heads/agent/T-7/a2"),
		"empty repository": advert(),
		// a1 is one branch: a10 next to it is not it.
		"a longer name": advert(headSHA + " refs/heads/agent/T-7/a10"),
		"a tag":         advert(headSHA + " refs/tags/agent/T-7/a1"),
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeGitea{adverts: agentsRepo(body)}
			_, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1")
			if !errors.Is(err, store.ErrState) {
				t.Fatalf("err = %v, want ErrState", err)
			}
		})
	}
}

// A repository the credential does not see, Gitea failing, or anything that
// is not a whole advertisement of that repository is the platform's problem:
// not ErrState, so agentd retries and keeps the result.
func TestGiteaBranchHeadRetriesWhatIsNotTheAgents(t *testing.T) {
	whole := advert(headSHA + " refs/heads/agent/T-7/a1")
	cases := map[string]*fakeGitea{
		"unseen repository":    {adverts: map[string]string{}},
		"gitea failing":        {status: http.StatusBadGateway},
		"asks for a login":     {status: http.StatusUnauthorized},
		"not an advertisement": {adverts: agentsRepo(whole), contentType: "text/html; charset=utf-8"},
		"another service": {
			adverts: agentsRepo(pkt("# service=git-receive-pack\n") + strings.TrimPrefix(whole, pkt("# service=git-upload-pack\n"))),
		},
		"no flush after the service line": {
			adverts: agentsRepo(pkt("# service=git-upload-pack\n") + pkt(headSHA+" refs/heads/agent/T-7/a1\n") + "0000"),
		},
		"cut off before the end":   {adverts: agentsRepo(strings.TrimSuffix(advert(otherSHA+" refs/heads/main"), "0000"))},
		"cut off after the branch": {adverts: agentsRepo(strings.TrimSuffix(whole, "0000"))},
		"cut off inside a line":    {adverts: agentsRepo(advert(otherSHA + " refs/heads/main")[:40])},
		"a malformed object id":    {adverts: agentsRepo(advert("main refs/heads/agent/T-7/a1"))},
		"a line without a ref":     {adverts: agentsRepo(advert(headSHA))},
		"the branch twice": {
			adverts: agentsRepo(advert(headSHA+" refs/heads/agent/T-7/a1", otherSHA+" refs/heads/agent/T-7/a1")),
		},
		"a bad length":        {adverts: agentsRepo(pkt("# service=git-upload-pack\n") + "0000zzzz0000")},
		"a protocol v2 delim": {adverts: agentsRepo(pkt("# service=git-upload-pack\n") + "0000" + "0001" + "0000")},
		// Review 194 A2: a redirect is not followed to another repository.
		"a redirect to another repository": {
			adverts: map[string]string{
				"bifrost-agents/bifrost-platform": advert(otherSHA + " refs/heads/main"),
				"bifrost/bifrost-platform":        whole,
			},
			redirect: map[string]string{
				"/bifrost-agents/bifrost-platform.git/info/refs": "/bifrost/bifrost-platform.git/info/refs?service=git-upload-pack",
			},
		},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1")
			if err == nil || errors.Is(err, store.ErrState) {
				t.Fatalf("err = %v, want an error that is not ErrState", err)
			}
		})
	}
	t.Run("access fails", func(t *testing.T) {
		head := giteaBranchHead(func(context.Context) (delivery.GiteaAccess, error) {
			return delivery.GiteaAccess{}, errors.New("no cluster")
		}, "bifrost-agents", http.DefaultClient)
		if _, err := head(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1"); err == nil || errors.Is(err, store.ErrState) {
			t.Fatalf("err = %v", err)
		}
	})
}

// Review 204: only a whole, well-formed advertisement, read to its end,
// answers either way. Each of these is an error of the platform whether the
// branch is in it or not: never a refusal, never a head.
func TestGiteaBranchHeadNeedsAWholeWellFormedAdvertisement(t *testing.T) {
	big := strings.Builder{}
	for i := 0; big.Len() <= maxAdvertisement; i++ {
		fmt.Fprintf(&big, "%s refs/heads/filler/%07d\n", otherSHA, i)
	}
	bigRefs := strings.Split(strings.TrimSuffix(big.String(), "\n"), "\n")
	for _, there := range []bool{true, false} {
		branch := otherSHA + " refs/heads/main"
		if there {
			branch = headSHA + " refs/heads/agent/T-7/a1"
		}
		cases := map[string]*fakeGitea{
			// A1: the body after the last flush.
			"a pkt-line after the last flush": {adverts: agentsRepo(advert(branch) + pkt(otherSHA+" refs/heads/late\n"))},
			"bytes after the last flush":      {adverts: agentsRepo(advert(branch) + "garbage")},
			"cut short after the last flush":  {adverts: agentsRepo(advert(branch)), extraLength: 10},
			"more than the bound":             {adverts: agentsRepo(advert(append([]string{branch}, bigRefs...)...))},
			// A2: every line is a ref line.
			"a NUL after the first line": {adverts: agentsRepo(advert(otherSHA+" refs/heads/main", branch+"\x00bad"))},
			"a name with a space":        {adverts: agentsRepo(advert(branch, otherSHA+" refs/heads/bad name"))},
			"a zero id on a branch":      {adverts: agentsRepo(advert(branch, zeroSHA+" refs/heads/gone"))},
			"another ref twice":          {adverts: agentsRepo(advert(branch, otherSHA+" refs/heads/x", otherSHA+" refs/heads/x"))},
			"a peeled branch":            {adverts: agentsRepo(advert(branch, otherSHA+" refs/heads/x^{}"))},
			"not under refs/":            {adverts: agentsRepo(advert(branch, otherSHA+" heads/x"))},
			"the empty line late":        {adverts: agentsRepo(advert(branch, zeroSHA+" capabilities^{}"))},
			"no refs at all":             {adverts: agentsRepo(pkt("# service=git-upload-pack\n") + "0000" + "0000")},
		}
		if !there {
			delete(cases, "no refs at all") // the same answer either way: run once
		}
		for name, f := range cases {
			t.Run(fmt.Sprintf("%s (branch there: %v)", name, there), func(t *testing.T) {
				got, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1")
				if err == nil || errors.Is(err, store.ErrState) {
					t.Fatalf("head = %q, err = %v; want an error that is not ErrState", got, err)
				}
			})
		}
	}
}

func TestGiteaBranchHeadRefusesWhatIsNotABranchOfARepository(t *testing.T) {
	f := &fakeGitea{status: http.StatusOK}
	head := headReader(t, f)
	for _, c := range []struct{ repo, ref string }{
		{"bifrost-platform", "agent/T-7/a1"},
		{"bifrost-platform", "refs/tags/v1"},
		{"bifrost-platform", "refs/heads/"},
		{"bifrost-platform", "refs/heads/agent//a1"},
		{"bifrost-platform", "refs/heads/agent/../main"},
		{"bifrost/other", "refs/heads/agent/T-7/a1"},
		{"bifrost-platform.git", "refs/heads/agent/T-7/a1"},
		{"", "refs/heads/agent/T-7/a1"},
	} {
		if _, err := head(context.Background(), c.repo, c.ref); !errors.Is(err, store.ErrState) {
			t.Fatalf("%q %q: err = %v, want ErrState", c.repo, c.ref, err)
		}
	}
	if len(f.paths) != 0 {
		t.Fatalf("paths = %v: nothing is asked of Gitea for these", f.paths)
	}
}

func TestAgentRuntimeHeadsNeedsTheOrganization(t *testing.T) {
	s := &Server{delivery: delivery.NewHandlerForTest(&delivery.Service{}, nil)}
	t.Setenv("AGENTRT_GIT_ORG", "")
	if s.agentRuntimeHeads() != nil {
		t.Fatal("a reader without AGENTRT_GIT_ORG")
	}
	t.Setenv("AGENTRT_GIT_ORG", " bifrost-agents ")
	if s.agentRuntimeHeads() == nil {
		t.Fatal("no reader with AGENTRT_GIT_ORG")
	}
	if (&Server{}).agentRuntimeHeads() != nil {
		t.Fatal("a reader without the delivery service")
	}
}
