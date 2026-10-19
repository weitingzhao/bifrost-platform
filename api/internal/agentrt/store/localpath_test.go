package store_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrations"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// H8: the closed rule finds every local path form in the artifacts that are
// scanned, and nothing in the ones that are not or in look-alikes.
func TestLocalPath(t *testing.T) {
	t.Parallel()
	for _, hit := range []string{
		"see /Users/someone/x", "in /home/ci/build", "/private/tmp/a", "at /var/folders/xy/T/z", "wrote /tmp/out.txt",
		"open ~/agent-work", "C:\\Users\\x", "D:/data", "path=E:/x",
	} {
		if _, found := store.LocalPath("report", []byte(hit)); !found {
			t.Errorf("missed %q", hit)
		}
	}
	for _, miss := range []string{
		"https://example.com/a", "see foo:/bar", "ratio 3:/4", "the home directory", "users/alice", "tmp files", "a~b",
	} {
		if p, found := store.LocalPath("report", []byte(miss)); found {
			t.Errorf("%q matched %q", miss, p)
		}
	}
	// Every kind the schema has is scanned except the four H8 lets through;
	// a kind not named anywhere is scanned too (review 156 A2).
	through := map[string]bool{"log_tail": true, "checkpoint_patch": true, "task_spec": true, "brief": true}
	for kind := range store.ArtifactKinds {
		if _, found := store.LocalPath(kind, []byte("/Users/someone")); found == through[kind] {
			t.Errorf("kind %s: scanned %v", kind, found)
		}
	}
	if len(store.ArtifactKinds) != 12 {
		t.Errorf("%d artifact kinds; the schema has 12", len(store.ArtifactKinds))
	}
	if _, found := store.LocalPath("a_kind_added_later", []byte("/home/ci")); !found {
		t.Error("an unnamed kind was not scanned")
	}
}

// ArtifactKinds is the schema's list, read from the migration itself.
func TestArtifactKindsAreTheSchemas(t *testing.T) {
	t.Parallel()
	sql, err := migrations.FS.ReadFile("0001_core.sql")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)artifact_kind\s+text NOT NULL CHECK \(artifact_kind IN \((.*?)\)\)`).FindSubmatch(sql)
	if m == nil {
		t.Fatal("no artifact_kind CHECK in 0001_core.sql")
	}
	var schema, ours []string
	for _, k := range regexp.MustCompile(`'([a-z_]+)'`).FindAllSubmatch(m[1], -1) {
		schema = append(schema, string(k[1]))
	}
	for k := range store.ArtifactKinds {
		ours = append(ours, k)
	}
	sort.Strings(schema)
	sort.Strings(ours)
	if strings.Join(schema, ",") != strings.Join(ours, ",") {
		t.Fatalf("schema %v, store %v", schema, ours)
	}
}
