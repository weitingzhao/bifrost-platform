//bifrost:owner verification

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

// The contract between the units routes and the Console's CI views: the
// bodies in testdata/contract/units are what these routes answer, and the
// Console's tests read the same files instead of this package's source.
// Origin: the Owner, 2026-11-14: "CI has its own front end and back end now;
// what it asks of the products that run on it, it asks of itself", and the
// declaration standard's rule that a contract is one copy, kept by the side
// that provides it, compared on every run. Each body is computed by the code
// the route runs and compared with its file; UNITS_CONTRACT_WRITE=1 writes
// the files instead, for a change of the contract made on purpose (the
// Console's tests then run against the new files).
func unitsContract(t *testing.T, name string, body any) {
	t.Helper()
	got, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "contract", "units", name)
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

func TestUnitsContractRuns(t *testing.T) {
	t.Parallel()
	// The kinds of run seen on 2026-11-14: a landing's run that left a module
	// out, the run of main's push that reused it, a daily full run, a run
	// still going, and a run of a pipeline that does not ask.
	runs := []landingci.RunView{
		{Name: "ci-5", Pipeline: "ci-pipeline", Revision: strings.Repeat("5", 40), Status: "Running", Trigger: "gitea-webhook", CreatedAt: "2026-11-14T16:40:00Z",
			Asked: true, RunModules: "*", ScopeWhy: "the platform says every module runs", Tasks: []landingci.TaskView{{Task: "changes", Status: "Succeeded", Seconds: 9, Waited: 6}, {Task: "test-web", Status: "Running", Waited: 5}}},
		{Name: "ci-full-444444444444-20261114", Pipeline: "ci-pipeline", Revision: strings.Repeat("4", 40), Status: "Failed", Trigger: landingci.FullCITrigger, Full: true,
			CreatedAt: "2026-11-14T09:00:00Z", Seconds: 215, Asked: true, RunModules: "*", ScopeWhy: "not asked: the changes against main were not read",
			Tasks: []landingci.TaskView{{Task: "changes", Status: "Succeeded", Seconds: 8, Waited: 6}, {Task: "test-svc", Status: "Succeeded", Seconds: 130, Waited: 7}, {Task: "test-web", Status: "Failed", Seconds: 199, Waited: 6}}},
		{Name: "ci-3", Pipeline: "ci-pipeline", Revision: strings.Repeat("3", 40), Status: "Succeeded", Trigger: "gitea-webhook", CreatedAt: "2026-11-14T08:05:00Z", Seconds: 25,
			TestedBy: "ci-2", Asked: true, RunModules: "*", ScopeWhy: "not asked: the changes against main were not read", Tasks: []landingci.TaskView{{Task: "changes", Status: "Succeeded", Seconds: 8, Waited: 6}}},
		{Name: "ci-2", Pipeline: "ci-pipeline", Revision: strings.Repeat("3", 40), Status: "Succeeded", Trigger: "gitea-webhook", CreatedAt: "2026-11-14T08:00:00Z", Seconds: 136,
			Asked: true, RunModules: " svc repo ", ScopeWhy: "not run, their keys are the base's: web",
			Tasks: []landingci.TaskView{{Task: "changes", Status: "Succeeded", Seconds: 11, Waited: 6}, {Task: "test-svc", Status: "Succeeded", Seconds: 123, Waited: 7}, {Task: "test-web", Status: "Succeeded", Seconds: 12, Waited: 5}}},
		{Name: "ci-1", Pipeline: "another-pipeline", Revision: strings.Repeat("2", 40), Status: "Succeeded", Trigger: "platform-api", CreatedAt: "2026-11-13T20:00:00Z", Seconds: 300, Tasks: []landingci.TaskView{}},
	}
	var cache cirunview.Cache
	out, err := cache.Read(context.Background(), func(context.Context, string, int) ([]landingci.RunView, error) { return runs, nil },
		time.Date(2026, 11, 14, 16, 45, 0, 0, time.UTC), "r", unitRunsDefault)
	if err != nil {
		t.Fatal(err)
	}
	unitsContract(t, "runs.json", out)
	empty, err := cache.Read(context.Background(), func(context.Context, string, int) ([]landingci.RunView, error) { return []landingci.RunView{}, nil },
		time.Date(2026, 11, 14, 16, 45, 0, 0, time.UTC), "nobody", unitRunsDefault)
	if err != nil {
		t.Fatal(err)
	}
	unitsContract(t, "runs-none.json", empty)
}
