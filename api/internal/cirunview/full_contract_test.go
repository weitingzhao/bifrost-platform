package cirunview

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
)

// unitsContract holds a body to its file of the contract the Console's CI
// views are tested against; the files stay with the other units routes'.
func unitsContract(t *testing.T, name string, body any) {
	t.Helper()
	got, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("..", "server", "testdata", "contract", "units", name)
	if os.Getenv("UNITS_CONTRACT_WRITE") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (UNITS_CONTRACT_WRITE=1 writes it)", name, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is not what the route answers now. The Console's CI views are tested against that file: change both on purpose (UNITS_CONTRACT_WRITE=1), not one.\n--- now\n%s", name, got)
	}
}

func TestUnitsContractFullCI(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	unitsContract(t, "full-ci-none.json", FullCIAnswer([]evidence.FullCINote{}))
	notes := []struct {
		text string
		body evidence.FullCI
	}{
		{"Full CI started: ci-full-444444444444-20261113.", evidence.FullCI{Started: []string{"ci-full-444444444444-20261113"}}},
		{"Full CI started: ci-full-111111111111-20261114.", evidence.FullCI{Started: []string{"ci-full-111111111111-20261114"}}},
		{"Full CI run ci-full-111111111111-20261114 of 1111111111111111111111111111111111111111 ended red; failed tasks: test-web. 1 commit(s) landed since the last green full run ci-full-444444444444-20261113 with a module left out of their own CI run: 33333333 ( svc repo ); the CI run of 1 more could not be read.",
			evidence.FullCI{Run: "ci-full-111111111111-20261114", Commit: strings.Repeat("1", 40), FailedTasks: []string{"test-web"},
				Suspects: []evidence.FullCISuspect{{Commit: strings.Repeat("3", 40), Subject: "feat: a change to svc alone", Run: "ci-2", Modules: " svc repo "}}, Unread: 1}},
		{"Full CI run ci-full-222222222222-20261115 of 2222222222222222222222222222222222222222 ended red; no failed task could be read. Which landed commits had a module left out of their own CI run was not reconciled: no full run in the last 3 days was green.",
			evidence.FullCI{Run: "ci-full-222222222222-20261115", Commit: strings.Repeat("2", 40), FailedKey: "TestA TestB", Unreconciled: "no full run in the last 3 days was green"}},
	}
	for _, n := range notes {
		if err := evidence.NoteFullCI(ctx, pool, n.text, n.body); err != nil {
			t.Fatal(err)
		}
	}
	read, err := evidence.FullCINotes(ctx, pool, fullCINotes)
	if err != nil || len(read) != len(notes) {
		t.Fatalf("notes: %d %v", len(read), err)
	}
	// The time a note was written is the database's: one fixed instant a note, newest first, for the file.
	for i := range read {
		read[i].At = time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Hour)
	}
	unitsContract(t, "full-ci.json", FullCIAnswer(read))
}
