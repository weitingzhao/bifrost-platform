package stgrelease

import (
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
)

func TestReleaseCIObservationPreservesRefusal(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	e := landingci.ReleaseEvidence{Evidence: landingci.Evidence{CIRun: landing.CIRun{Run: "ci", UID: "ci-uid", R: strings.Repeat("a", 40), Status: "Succeeded", CreatedAt: at, PlatformCommit: strings.Repeat("a", 40), UICommit: strings.Repeat("b", 40)}, InfraCommit: strings.Repeat("c", 40)}, PipelineRef: "bifrost-ci-platform", RepoURL: landingci.PlatformRepoURL, ReadAt: at}
	if _, err := ReleaseCIObservation(e).Record(); err != nil {
		t.Fatal("success control", err)
	}
	// R5:5.7-2: a green chosen over newer undecided runs is accepted as is;
	// the count is carried for the check after the send.
	passed := e
	passed.Undecided = []string{"ci-platform-main"}
	if o := ReleaseCIObservation(passed); o.Undecided != 1 {
		t.Fatal("undecided count lost", o.Undecided)
	} else if _, err := o.Record(); err != nil {
		t.Fatal("passed-over green refused", err)
	}
	for _, status := range []string{"Failed", "Running", "Unknown", "Missing"} {
		bad := e
		bad.Status = status
		o := ReleaseCIObservation(bad)
		if _, err := o.Record(); err == nil || o.Status != status || o.UID != e.UID || !o.CreatedAt.Equal(at) {
			t.Fatal("unsuccessful CI became accepted", o, err)
		}
	}
}
