package gitread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
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

func headReader(t *testing.T, f *fakeGitea) func(context.Context, string, string) (string, error) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	access := func(context.Context) (Access, error) {
		return Access{Base: srv.URL + "/", Org: "bifrost", User: "reader", Pass: "secret"}, nil
	}
	return BranchHead(access, "bifrost-agents", srv.Client())
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
		return string(out) // as git wrote it: an advertisement is bytes, not text
	}
	trim := func(out string) string { return strings.TrimSpace(out) }
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "one")
	git("tag", "-a", "-m", "v1", "v1")
	git("branch", "agent/T-7/a10")
	git("commit", "-q", "--allow-empty", "-m", "two")
	git("branch", "agent/T-7/a1")
	want := trim(git("rev-parse", "agent/T-7/a1"))
	// Any ref may point at an annotated tag, and git advertises its peeled
	// line (review 206 A2).
	git("update-ref", "refs/pull/9/head", trim(git("rev-parse", "v1")))
	if !strings.Contains(git("upload-pack", "--advertise-refs", "--stateless-rpc", dir), " refs/pull/9/head^{}\n") {
		t.Fatal("git did not advertise refs/pull/9/head^{}")
	}
	refs := git("upload-pack", "--advertise-refs", "--stateless-rpc", dir)
	f := &fakeGitea{adverts: agentsRepo(pkt("# service=git-upload-pack\n") + "0000" + refs)}
	head := headReader(t, f)
	if got, err := head(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1"); err != nil || got != want {
		t.Fatalf("head = %q, %v; want %s", got, err, want)
	}
	if _, err := head(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a2"); !errors.Is(err, eventlog.ErrState) {
		t.Fatalf("a branch git does not advertise: err = %v, want ErrState", err)
	}
	// HEAD pointing at an annotated tag: git advertises HEAD^{} right after
	// HEAD (review 207 B).
	git("symbolic-ref", "HEAD", "refs/tags/v1")
	f = &fakeGitea{adverts: agentsRepo(pkt("# service=git-upload-pack\n") + "0000" + git("upload-pack", "--advertise-refs", "--stateless-rpc", dir))}
	if got, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1"); err != nil || got != want {
		t.Fatalf("HEAD at a tag: head = %q, %v; want %s", got, err, want)
	}
	// An empty repository as git advertises it.
	dir = t.TempDir()
	git("init", "-q", "-b", "main")
	f = &fakeGitea{adverts: agentsRepo(pkt("# service=git-upload-pack\n") + "0000" + git("upload-pack", "--advertise-refs", "--stateless-rpc", dir))}
	if _, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1"); !errors.Is(err, eventlog.ErrState) {
		t.Fatalf("an empty repository: err = %v, want ErrState", err)
	}
}

func TestGiteaBranchHeadRefusesAMissingBranch(t *testing.T) {
	for name, body := range map[string]string{
		"other branches":   advert(otherSHA+" refs/heads/main", otherSHA+" refs/heads/agent/T-7/a2"),
		"empty repository": advert(),
		// git before 2.41 advertises an empty repository with no refs at all.
		"empty repository, older git": pkt("# service=git-upload-pack\n") + "0000" + "0000",
		// a1 is one branch: a10 next to it is not it.
		"a longer name": advert(headSHA + " refs/heads/agent/T-7/a10"),
		"a tag":         advert(headSHA + " refs/tags/agent/T-7/a1"),
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeGitea{adverts: agentsRepo(body)}
			_, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1")
			if !errors.Is(err, eventlog.ErrState) {
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
			if err == nil || errors.Is(err, eventlog.ErrState) {
				t.Fatalf("err = %v, want an error that is not ErrState", err)
			}
		})
	}
	t.Run("access fails", func(t *testing.T) {
		head := BranchHead(func(context.Context) (Access, error) {
			return Access{}, errors.New("no cluster")
		}, "bifrost-agents", http.DefaultClient)
		if _, err := head(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1"); err == nil || errors.Is(err, eventlog.ErrState) {
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
			"a NUL after the first line":    {adverts: agentsRepo(advert(otherSHA+" refs/heads/main", branch+"\x00bad"))},
			"a name with a space":           {adverts: agentsRepo(advert(branch, otherSHA+" refs/heads/bad name"))},
			"a zero id on a branch":         {adverts: agentsRepo(advert(branch, zeroSHA+" refs/heads/gone"))},
			"another ref twice":             {adverts: agentsRepo(advert(branch, otherSHA+" refs/heads/x", otherSHA+" refs/heads/x"))},
			"a peeled line without its ref": {adverts: agentsRepo(advert(branch, otherSHA+" refs/heads/x^{}"))},
			"a peeled line after another":   {adverts: agentsRepo(advert(otherSHA+" refs/tags/v1", branch, otherSHA+" refs/tags/v1^{}"))},
			// Review 206 A2: an empty repository's line is its only one.
			"the empty line, then refs": {adverts: agentsRepo(advert(zeroSHA+" capabilities^{}", branch))},
			"not under refs/":           {adverts: agentsRepo(advert(branch, otherSHA+" heads/x"))},
			"the empty line late":       {adverts: agentsRepo(advert(branch, zeroSHA+" capabilities^{}"))},
		}
		for name, f := range cases {
			t.Run(fmt.Sprintf("%s (branch there: %v)", name, there), func(t *testing.T) {
				got, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1")
				if err == nil || errors.Is(err, eventlog.ErrState) {
					t.Fatalf("head = %q, err = %v; want an error that is not ErrState", got, err)
				}
			})
		}
	}
}

// Review 206 A1: the last flush ending where a 4096-byte read buffer would
// end, then more. Nothing may follow it, wherever it falls.
func TestGiteaBranchHeadSeesWhatFollowsTheLastFlushAnywhere(t *testing.T) {
	for _, there := range []bool{true, false} {
		branch := otherSHA + " refs/heads/main"
		if there {
			branch = headSHA + " refs/heads/agent/T-7/a1"
		}
		var body string
		for pad := 0; ; pad++ {
			body = advert(branch, otherSHA+" refs/heads/filler/"+strings.Repeat("x", 3800+pad))
			if len(body) >= 4096 {
				break
			}
		}
		if len(body) != 4096 {
			t.Fatalf("the advertisement is %d bytes, want it to end at 4096", len(body))
		}
		for name, more := range map[string]string{"bytes": "garbage", "a ref": pkt(otherSHA + " refs/heads/late\n")} {
			f := &fakeGitea{adverts: agentsRepo(body + more)}
			got, err := headReader(t, f)(context.Background(), "bifrost-platform", "refs/heads/agent/T-7/a1")
			if err == nil || errors.Is(err, eventlog.ErrState) {
				t.Fatalf("%s after the flush at 4096 (branch there: %v): head = %q, err = %v", name, there, got, err)
			}
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
		if _, err := head(context.Background(), c.repo, c.ref); !errors.Is(err, eventlog.ErrState) {
			t.Fatalf("%q %q: err = %v, want ErrState", c.repo, c.ref, err)
		}
	}
	if len(f.paths) != 0 {
		t.Fatalf("paths = %v: nothing is asked of Gitea for these", f.paths)
	}
}

// The root tree of bifrost-platform at 057dcc1e, as git ls-tree lists it: git
// names that tree 315f7a37… (git rev-parse 057dcc1e^{tree}).
const platformRootTree = `100644 blob 6bae55cb70329356f1aa93f09d8753c071b60cbd .env.example
100644 blob d5437c3acef29b614da4d96b59866a4d20093f19 .gitignore
040000 tree c0c1a0224fd1a700d0c5beec0aee5553e16a6e4a .husky
100644 blob 209e3ef4b6247ce746048d5711befda46206d235 .nvmrc
100644 blob 7d6b23401bd55395714a07dec6f4b5d8dfa48229 CLAUDE.md
100644 blob 261eeb9e9f8b2b4b0d119366dda99c6fd7d35c64 LICENSE
100644 blob 72a1faabbb0f4a798b3f1830a146ca04dd9c9dbb Makefile
100644 blob a2531e382b6ec351091b809be384a3cb65cf354c PROGRESS-daily-ops-checklist-step2.md
100644 blob 355d7793dff6abfcbec4cc9197757a503b4cb0e9 PROGRESS-data-unification.md
100644 blob 1c63a6d21e028be11375fb37ecf30de405f78e01 PROGRESS-observability-hub.md
100644 blob 0dfb152fda39073d44c5365eb7d88062d02f4202 README.md
040000 tree 9ac478b2514964af20a0df1447b43ef082cdde83 agent
040000 tree 6e64bdf6c400c312376a12bdf7e2faa228400b86 api
040000 tree 2bce4888a21211bc9258f6a495eeba40a1293c92 config
040000 tree 9a161b48f1fef5691cc07533e9eb4f9167f153b9 console
040000 tree e3fc092e5d01bd8419bb3cbb089fedbffbe35fde mcp
040000 tree 71dbfa3003be3cf0fc2e4e167d5b353cc317a50a scripts
040000 tree 0ea7461831c51028ca3e8fb082af962dc73d8a8a site`

// The tree of a commit is computed from the entries of its root tree, read a
// page at a time and in any order; Gitea's own "sha" of the answer is the
// commit's id and is not used.
func TestGiteaCommitTree(t *testing.T) {
	t.Parallel()
	commit := "057dcc1ea9584ad95c3bf390434e146e15529c18"
	var all []map[string]string
	for _, line := range strings.Split(platformRootTree, "\n") {
		f := strings.Fields(line)
		all = append([]map[string]string{{"mode": f[0], "type": f[1], "sha": f[2], "path": f[3]}}, all...) // reversed
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, _ := r.BasicAuth(); user != "u" || pass != "p" || r.URL.Path != "/api/v1/repos/bifrost/bifrost-platform/git/trees/"+commit {
			http.Error(w, "no", http.StatusNotFound)
			return
		}
		page := all[:10]
		if r.URL.Query().Get("page") == "2" {
			page = all[10:]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": commit, "total_count": len(all), "tree": page})
	}))
	defer srv.Close()
	read := CommitTree(func(context.Context) (Access, error) {
		return Access{Base: srv.URL, Org: "bifrost", User: "u", Pass: "p"}, nil
	}, srv.Client())
	if got, err := read(context.Background(), "bifrost-platform", commit); err != nil || got != "315f7a3790d88e322e3ea855ebaa743b4b976876" {
		t.Fatalf("tree %q %v", got, err)
	}
	if _, err := read(context.Background(), "bifrost-ui", commit); err == nil {
		t.Fatal("a commit Gitea does not have gave a tree")
	}
	if _, err := read(context.Background(), "bifrost-platform", "main"); err == nil {
		t.Fatal("a branch name was read as a commit")
	}
	// git's order: a directory sorts as its name and "/" ("a.b" before "a/").
	got, err := TreeID([]TreeEntry{{Path: "a", Mode: "040000", Type: "tree", SHA: strings.Repeat("1", 40)}, {Path: "a.b", Mode: "100644", Type: "blob", SHA: strings.Repeat("2", 40)}})
	if err != nil || got != treeOfAAndAB {
		t.Fatalf("order: %q %v", got, err)
	}
}

// git mktree's id for a directory "a" and a file "a.b" with those ids.
const treeOfAAndAB = "f9a24078951ed457ecc90ac99b0abf5ecacb0f61"
