package instruments

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
)

const unitsManifest = `version: 1
modules:
  - name: api
    kind: go
    root: api
    tasks: {test: {per: package}}
  - name: repo
    root: .
    tasks: {check: {}}
`

// planRepo is a small repository as Gitea serves it: every path with its
// type and git object id, and the blobs by id.
func planRepo() (entries []gitread.TreeEntry, blobs map[string]string) {
	files := map[string]string{
		"bifrost.units.yaml": unitsManifest,
		"api/go.mod":         "module example.com/m\n\ngo 1.25\n",
		"api/a/a.go":         "package a\n",
		"api/a/a_test.go":    "package a\n",
	}
	blobs = map[string]string{}
	for p, body := range files {
		id := gitBlobID(body)
		blobs[id] = body
		entries = append(entries, gitread.TreeEntry{Path: p, Mode: "100644", Type: "blob", SHA: id})
	}
	sub := func(dir string) string { // the id of a directory of blobs and the directories given
		var list []gitread.TreeEntry
		for _, e := range entries {
			if rest, ok := strings.CutPrefix(e.Path, dir+"/"); ok && !strings.Contains(rest, "/") {
				list = append(list, gitread.TreeEntry{Path: rest, Mode: e.Mode, Type: e.Type, SHA: e.SHA})
			}
		}
		id, err := gitread.TreeID(list)
		if err != nil {
			panic(err)
		}
		return id
	}
	entries = append(entries, gitread.TreeEntry{Path: "api/a", Mode: "040000", Type: "tree", SHA: sub("api/a")})
	entries = append(entries, gitread.TreeEntry{Path: "api", Mode: "040000", Type: "tree", SHA: sub("api")})
	return entries, blobs
}

// 2026-11-18, ISSUE-36: laws, Skills and MCP tools are audited on Ops; agents forgot rules that existed.
// The instruments' reader lists a commit's files (no directories, no submodules) and returns a blob's bytes from Gitea.
func TestInstrumentSourceReadsFilesAndBlobsFromGitea(t *testing.T) {
	const commit = "0123456789012345678901234567890123456789"
	entries, blobs := planRepo()
	entries = append(entries, gitread.TreeEntry{Path: "vendored", Mode: "160000", Type: "commit", SHA: commit})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/repos/bifrost/r/git/trees/"+commit:
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": commit, "page": 1, "total_count": len(entries), "truncated": false, "tree": entries})
		case strings.HasPrefix(r.URL.Path, "/api/v1/repos/bifrost/r/git/blobs/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/repos/bifrost/r/git/blobs/")
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": id, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(blobs[id]))})
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	source := GiteaSource{Trees: &gitread.Trees{Access: func(context.Context) (gitread.Access, error) {
		return gitread.Access{Base: srv.URL, Org: "bifrost"}, nil
	}, Client: srv.Client()}}
	files, err := source.Files(context.Background(), "r", commit)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := files["bifrost.units.yaml"]
	if !ok {
		t.Fatalf("the manifest is not among the files: %v", files)
	}
	for _, notFile := range []string{"vendored", "api", "api/a"} {
		if _, listed := files[notFile]; listed {
			t.Errorf("%s is listed as a file", notFile)
		}
	}
	raw, err := source.Blob(context.Background(), "r", id)
	if err != nil || string(raw) != unitsManifest {
		t.Fatalf("blob = %q, %v", raw, err)
	}
}

// gitBlobID is the git object id of a file's content, as Gitea names blobs.
func gitBlobID(content string) string {
	return fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(content), content))))
}
