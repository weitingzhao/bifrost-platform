package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// fakeCommit is one commit of a fake mirror's commit list.
type fakeCommit struct {
	sha     string
	files   []string
	noFiles bool // listed without a files key at all
}

// commitRanges answers GET .../commits the way Gitea 1.21 does: the key is
// "<sha>|<not>" (git log <sha> --not <not>), paged by limit and page, with
// the range's size in X-Total-Count. An unknown range is a 500, as a git log
// on an unknown commit is.
type commitRanges map[string][]fakeCommit

func (cr commitRanges) serve(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	commits, ok := cr[q.Get("sha")+"|"+q.Get("not")]
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"GetCommit"}`))
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, _ := strconv.Atoi(q.Get("page"))
	if limit <= 0 || page <= 0 || q.Get("files") != "true" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	lo := min((page-1)*limit, len(commits))
	hi := min(lo+limit, len(commits))
	out := make([]map[string]any, 0, hi-lo)
	for _, c := range commits[lo:hi] {
		m := map[string]any{"sha": c.sha}
		if !c.noFiles {
			files := make([]map[string]string, 0, len(c.files))
			for _, f := range c.files {
				files = append(files, map[string]string{"filename": f, "status": "modified"})
			}
			m["files"] = files
		}
		out = append(out, m)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Total-Count", strconv.Itoa(len(commits)))
	_ = json.NewEncoder(w).Encode(out)
}

// diffService is a delivery service whose mirror serves ranges for one repo
// and, like Gitea 1.21, has no compare route. asked collects the request paths.
func diffService(t *testing.T, ranges commitRanges, tweak func(http.ResponseWriter, *http.Request) bool) (*Service, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		if tweak != nil && tweak(w, r) {
			return
		}
		if r.URL.Path == "/api/v1/repos/bifrost/repo-app/commits" {
			ranges.serve(w, r)
			return
		}
		// The router's own answer for a route it does not have.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 page not found\n"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITEA_BASE", srv.URL)
	return NewService(&config.ClusterEntry{ID: "test"}), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), asked...)
	}
}

func shaN(n int) string { return fmt.Sprintf("%040x", n) }

func TestChangedFilesUnionsTheCommitsBetween(t *testing.T) {
	// 120 commits: three pages. Each touches its own file and a shared one;
	// the newest is a merge, whose list is what it brought in.
	var commits []fakeCommit
	for i := 120; i >= 1; i-- {
		commits = append(commits, fakeCommit{sha: shaN(i), files: []string{fmt.Sprintf("src/f%03d.go", i), "go.mod"}})
	}
	commits[0].files = []string{"api/internal/approvals/service.go", "merged/in.go"}
	s, asked := diffService(t, commitRanges{
		factSHA1 + "|" + factSHA2: commits,
		factSHA2 + "|" + factSHA1: nil,
	}, nil)
	files, err := s.changedFilesBetween(context.Background(), "repo-app", factSHA2, factSHA1, "", "")
	if err != nil {
		t.Fatalf("changedFilesBetween: %v", err)
	}
	if len(files) != 122 || files[0] != "api/internal/approvals/service.go" || files[1] != "go.mod" || files[2] != "merged/in.go" || files[121] != "src/f119.go" {
		t.Fatalf("files = %d %v", len(files), files)
	}
	for _, p := range asked() {
		if strings.Contains(p, "/compare/") {
			t.Fatalf("asked the compare route Gitea 1.21 does not have: %s", p)
		}
	}
}

func TestChangedFilesSameCommitIsEmptyAndAsksNothing(t *testing.T) {
	s, asked := diffService(t, commitRanges{}, nil)
	files, err := s.changedFilesBetween(context.Background(), "repo-app", factSHA1, factSHA1, "", "")
	if err != nil || len(files) != 0 || len(asked()) != 0 {
		t.Fatalf("files %v err %v asked %v", files, err, asked())
	}
}

// Whatever the walk cannot establish is an error, never a shorter list.
func TestChangedFilesFailsRatherThanGuess(t *testing.T) {
	one := []fakeCommit{{sha: factSHA1, files: []string{"a.go"}}}
	cases := []struct {
		name   string
		ranges commitRanges
		tweak  func(http.ResponseWriter, *http.Request) bool
		env    string
		want   string
	}{
		{
			name: "the old side has commits the new side lacks",
			ranges: commitRanges{
				factSHA1 + "|" + factSHA2: one,
				factSHA2 + "|" + factSHA1: {{sha: factSHA2, files: []string{"rolled/back.sql"}}},
			},
			want: "is not an ancestor of",
		},
		{
			name:   "the old commit is unknown to the mirror",
			ranges: commitRanges{factSHA1 + "|" + factSHA2: one},
			want:   "status 500",
		},
		{
			name: "a commit comes without its files",
			ranges: commitRanges{
				factSHA1 + "|" + factSHA2: {{sha: factSHA1, noFiles: true}},
				factSHA2 + "|" + factSHA1: nil,
			},
			want: "without its files",
		},
		{
			name: "the mirror did not apply not=",
			ranges: commitRanges{
				factSHA1 + "|" + factSHA2: {{sha: factSHA1, files: []string{"a.go"}}, {sha: factSHA2, files: []string{"old.go"}}},
				factSHA2 + "|" + factSHA1: nil,
			},
			want: "did not apply not=",
		},
		{
			name: "no commits between two different commits",
			ranges: commitRanges{
				factSHA1 + "|" + factSHA2: nil,
				factSHA2 + "|" + factSHA1: nil,
			},
			want: "lists no commits",
		},
		{
			name: "more commits than one walk reads",
			ranges: commitRanges{
				factSHA1 + "|" + factSHA2: {{sha: shaN(9), files: []string{"a"}}, {sha: shaN(8), files: []string{"b"}}, {sha: shaN(7), files: []string{"c"}}},
				factSHA2 + "|" + factSHA1: nil,
			},
			env:  "2",
			want: "more than 2 commits",
		},
		{
			name: "the mirror counts more commits than it lists",
			ranges: commitRanges{
				factSHA1 + "|" + factSHA2: one,
				factSHA2 + "|" + factSHA1: nil,
			},
			tweak: func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Query().Get("sha") != factSHA1 {
					return false
				}
				w.Header().Set("X-Total-Count", "7")
				if r.URL.Query().Get("page") == "1" {
					_, _ = w.Write([]byte(`[{"sha":"` + factSHA1 + `","files":[{"filename":"a.go"}]}]`))
				} else {
					_, _ = w.Write([]byte(`[]`))
				}
				return true
			},
			want: "counts 7 commits",
		},
		{
			name: "the commit list is not JSON",
			tweak: func(w http.ResponseWriter, _ *http.Request) bool {
				_, _ = w.Write([]byte("<html>"))
				return true
			},
			want: "json:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("PLATFORM_POLICY_DIFF_MAX_COMMITS", tc.env)
			}
			s, _ := diffService(t, tc.ranges, tc.tweak)
			files, err := s.changedFilesBetween(context.Background(), "repo-app", factSHA2, factSHA1, "", "")
			if err == nil || !strings.Contains(err.Error(), tc.want) || files != nil {
				t.Fatalf("files %v err %v, want an error containing %q", files, err, tc.want)
			}
		})
	}
}

// The mirror the cluster runs answers the compare route with the router's
// plain 404. Nothing may depend on that route again.
func TestCompareReadsThroughTheCommitList(t *testing.T) {
	s, asked := diffService(t, commitRanges{
		factSHA1 + "|main": {{sha: factSHA1, files: []string{"b.go", "a.go"}}},
		"main|" + factSHA1: nil,
	}, nil)
	t.Setenv(giteaReaderDirEnv, writeReader(t, map[string]string{"username": readerUserFixture + "\n", "password": readerTokenFixture + "\n"}))
	out := s.Compare(context.Background(), "repo-app", "main", factSHA1)
	if out.Detail != "2 files" || strings.Join(out.Files, ",") != "a.go,b.go" {
		t.Fatalf("Compare = %+v", out)
	}
	for _, p := range asked() {
		if strings.Contains(p, "/compare/") {
			t.Fatalf("asked the compare route: %s", p)
		}
	}
}
