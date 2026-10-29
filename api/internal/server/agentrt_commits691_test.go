package server

import (
	"context"
	gitsha1 "crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

type commits691Transport func(*http.Request) (*http.Response, error)

func (f commits691Transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func Test691CommitEvidenceComplete(t *testing.T) {
	t.Parallel()
	blobID := func(s string) string {
		return fmt.Sprintf("%x", gitsha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(s), s))))
	}
	sha, parent := strings.Repeat("b", 40), strings.Repeat("a", 40)
	full := "diff --git a/docs/note.txt b/docs/note.txt\n--- a/docs/note.txt\n+++ b/docs/note.txt\n@@ -1,2 +1,2 @@\n-old1\n-old2\n+new1\n+new2\n" +
		"diff --git a/api/internal/agentrt/store/pin.go b/api/internal/agentrt/store/pin.go\n--- a/api/internal/agentrt/store/pin.go\n+++ b/api/internal/agentrt/store/pin.go\n@@ -1 +1 @@\n-old\n+new\n"
	for _, name := range []string{"complete", "mid hunk", "file boundary", "error in hunk", "error after diff", "extra hunk line", "tree incomplete", "tree duplicate", "tree total changes", "tree identity changes", "tree missing page", "blob short", "missing later hunk", "oversized"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			diff := full
			switch name {
			case "mid hunk":
				diff = strings.Split(full, "-old2")[0]
			case "file boundary":
				diff = strings.Split(full, "diff --git a/api/")[0]
			case "error in hunk":
				diff = strings.Split(full, "-old2")[0] + `{"message":"git failed"}`
			case "error after diff":
				diff += `{"message":"git failed"}`
			case "extra hunk line":
				diff += "+unexpected\n"
			case "missing later hunk":
				diff = full
			case "oversized":
				diff = strings.Repeat("x", maxCommitDiff+1)
			}
			client := &http.Client{Transport: commits691Transport(func(r *http.Request) (*http.Response, error) {
				body := fmt.Sprintf(`{"sha":%q,"parents":[{"sha":%q}],"commit":{"message":"fixture"}}`, sha, parent)
				switch {
				case strings.HasSuffix(r.URL.Path, ".diff"):
					body = diff
				case strings.Contains(r.URL.Path, "/git/blobs/"):
					content := "old1\nold2\n"
					if strings.HasSuffix(r.URL.Path, blobID("old\n")) {
						content = "old\n"
					}
					identity := blobID(content)
					if name == "blob short" {
						content = "old"
					}
					b, _ := json.Marshal(map[string]any{"sha": identity, "size": len(content), "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))})
					body = string(b)
				case strings.Contains(r.URL.Path, "/git/trees/"):
					page := 1
					path := "docs/note.txt"
					if r.URL.Query().Get("page") == "2" {
						page = 2
						path = "api/internal/agentrt/store/pin.go"
					}
					content := "old1\nold2\n"
					if page == 2 {
						content = "old\n"
					}
					if strings.HasSuffix(r.URL.Path, sha) {
						content = "new1\nnew2\n"
						if page == 2 {
							content = "new\n"
						}
						if name == "missing later hunk" && page == 1 {
							content += "omitted change\n"
						}
					}
					blob := blobID(content)
					entry := map[string]any{"path": path, "mode": "100644", "type": "blob", "sha": blob}
					doc := map[string]any{"sha": parent, "page": page, "total_count": 2, "truncated": true, "tree": []any{entry}}
					if page == 2 {
						switch name {
						case "tree incomplete":
							doc["tree"] = []any{}
						case "tree duplicate":
							entry["path"] = "docs/note.txt"
						case "tree total changes":
							doc["total_count"] = 3
						case "tree identity changes":
							doc["sha"] = strings.Repeat("c", 40)
						case "tree missing page":
							delete(doc, "page")
						}
					}
					b, _ := json.Marshal(doc)
					body = string(b)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			reader := giteaCommits(func(context.Context) (delivery.GiteaAccess, error) {
				return delivery.GiteaAccess{Base: "http://offline.invalid"}, nil
			}, "bifrost", client)
			facts, err := reader(context.Background(), "bifrost-platform", sha)
			if name == "complete" {
				if err != nil || len(facts.Paths) != 2 || facts.Lines != 6 {
					t.Fatalf("complete paged evidence: %+v %v", facts, err)
				}
			} else if err == nil {
				t.Fatalf("incomplete evidence accepted: %+v", facts)
			}
		})
	}
}

func Test691BinaryReviewConservative(t *testing.T) {
	t.Parallel()
	paths, lines, err := diffFacts([]byte("diff --git a/bin.png b/bin.png\nindex a..b 100644\nBinary files a/bin.png and b/bin.png differ\n"))
	if err != nil || len(paths) != 1 || lines < 1200 {
		t.Fatalf("unknown binary size must get maximum review time: %v %d %v", paths, lines, err)
	}
}

// Exercise complete patches produced by real Git, including cases whose line
// endings, mode-only changes or rename headers differ from hand-written fixtures.
func Test691ActualGitPatchShapes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
		return string(b)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	write("old", "rename\n")
	write("gone", "delete\n")
	write("crlf", "old\r\n")
	write("no-newline", "old")
	write("mode", "same\n")
	write("binary", "\x00old")
	git("add", "old", "gone", "crlf", "no-newline", "mode", "binary")
	git("commit", "-qm", "base")
	base := strings.TrimSpace(git("rev-parse", "HEAD"))
	git("mv", "old", "renamed file")
	git("rm", "gone")
	write("crlf", "new\r\n")
	write("no-newline", "new")
	write("new", "added\n")
	write("binary", "\x00new")
	if err := os.Chmod(filepath.Join(dir, "mode"), 0700); err != nil {
		t.Fatal(err)
	}
	git("add", "crlf", "no-newline", "new", "binary", "mode")
	git("commit", "-qm", "change")
	tree := func(ref string) map[string]commitTreeEntry {
		out := map[string]commitTreeEntry{}
		for _, line := range strings.Split(strings.TrimSuffix(git("ls-tree", "-rz", ref), "\x00"), "\x00") {
			fields, name, _ := strings.Cut(line, "\t")
			v := strings.Fields(fields)
			out[name] = commitTreeEntry{Path: name, Mode: v[0], Type: v[1], SHA: v[2]}
		}
		return out
	}
	before, after := tree(base), tree("HEAD")
	diff := []byte(git("diff", "--find-renames", base, "HEAD"))
	paths, _, err := diffFacts(diff)
	if err != nil {
		t.Fatal(err)
	}
	if err = completeDiffPaths(before, after, paths); err != nil {
		t.Fatal(err)
	}
	get := func(sha string, _ int64) ([]byte, error) {
		body := git("cat-file", "blob", sha)
		return json.Marshal(map[string]any{"sha": sha, "encoding": "base64", "size": len(body), "content": base64.StdEncoding.EncodeToString([]byte(body))})
	}
	if err = completeDiffContent(get, "", before, after, diff); err != nil {
		t.Fatalf("real Git diff: %v\n%s", err, diff)
	}
}
