package lineage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMainChanges(t *testing.T) {
	const task = "t_01M5B8BF2EM3SJQDKZ9E9TY2H8"
	for _, mode := range []string{"folded", "change-id", "patch", "reverted", "reverted-id", "reverted-folded"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC()
			main, pushed := sha40("a"), sha40("b")
			id := "I" + sha40("c")
			msg := "hand merge"
			switch mode {
			case "folded", "reverted-folded":
				msg = "squash\n\nAgentrt-Task: " + task + "\n\nFurther body."
			case "change-id", "reverted-id":
				msg = "squash\n\nChange-Id: " + id + "\n\nFurther body."
			}
			messages, diffs := 0, 0
			diff := "diff --git a/a b/a\nindex 1234567..7654321 100644\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-old\n+new\n"
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/repos/org/repo/commits", func(w http.ResponseWriter, r *http.Request) {
				commits := []any{map[string]any{"sha": main, "commit": map[string]any{"message": msg, "committer": map[string]any{"date": now}}}}
				if strings.HasPrefix(mode, "reverted") {
					commits = append([]any{map[string]any{"sha": sha40("d"), "commit": map[string]any{"message": `Revert "hand merge"` + "\n\nThis reverts commit " + main + ".", "committer": map[string]any{"date": now}}}}, commits...)
				}
				_ = json.NewEncoder(w).Encode(commits)
			})
			mux.HandleFunc("/api/v1/repos/org/repo/git/commits/", func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".diff") {
					diffs++
					if strings.Contains(r.URL.Path, sha40("d")) {
						_, _ = w.Write([]byte(strings.ReplaceAll(diff, "+new", "+different")))
						return
					}
					_, _ = w.Write([]byte(diff))
					return
				}
				messages++
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "attempt\n\nChange-Id: " + id, "committer": map[string]any{"date": now.Add(-time.Hour)}})
			})
			client := &http.Client{Transport: mainChangesTransport{handler: mux}}
			acc := Access{Base: "http://" + mode + ".test", Org: "org"}
			for pass := 0; pass < 2; pass++ {
				got, err := MainChanges(context.Background(), acc, client, "repo", now.Add(-24*time.Hour), 1, map[string][]string{task: {pushed}})
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(mode, "reverted") {
					if len(got[task]) != 0 {
						t.Fatalf("reverted result: %v", got)
					}
				} else if mode == "folded" {
					if got[task][main] != main {
						t.Fatalf("folded result: %v", got)
					}
					plain, err := MainResults(context.Background(), acc, client, "repo", now.Add(-24*time.Hour), 1)
					if err != nil || plain[task][main] != main {
						t.Fatalf("MainResults folded: %v, %v", plain, err)
					}
				} else if got[task][pushed] != main {
					t.Fatalf("result: %v", got)
				}
				if pass == 0 {
					firstMessages, firstDiffs := messages, diffs
					defer func() {
						if messages != firstMessages || diffs != firstDiffs {
							t.Errorf("immutable data read again: messages %d/%d diffs %d/%d", messages, firstMessages, diffs, firstDiffs)
						}
					}()
				}
			}
		})
	}
}

func TestMainChangesVerbatimPatchID(t *testing.T) {
	diff := "diff --git a/a b/a\nindex 1234567..7654321 100644\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-old\n+new\n"
	if got := verbatimPatchID(diff); got != "d7537690a793e6e98ed5201937d8d7c13c76f93c" {
		t.Fatal(got)
	}
	second := strings.ReplaceAll(strings.ReplaceAll(diff, "a/a", "a/b"), "b/a", "b/b")
	if got := verbatimPatchID(diff + second); got != "6efa597489e9f127146d33255341a9d6b10ed774" {
		t.Fatalf("per-file digest sum: %s", got)
	}
	if verbatimPatchID(diff) != verbatimPatchID(strings.ReplaceAll(diff, "@@ -1 +1 @@", "@@ -20 +40 @@")) {
		t.Fatal("hunk positions counted")
	}
	if verbatimPatchID(diff) == verbatimPatchID(strings.ReplaceAll(diff, "+new", "+new ")) {
		t.Fatal("whitespace lost")
	}
}

// An httptest Gitea handler, reached without opening a listening socket.
type mainChangesTransport struct{ handler http.Handler }

func (tr mainChangesTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	tr.handler.ServeHTTP(w, r)
	return w.Result(), nil
}
