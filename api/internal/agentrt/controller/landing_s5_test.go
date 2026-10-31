package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

func TestS5CandidateNeverFallsBackToLegacyCI(t *testing.T) {
	calls := 0
	src := DrillSources{CI: func(context.Context, string, string, time.Time) (landing.CIRun, error) {
		calls++
		return landing.CIRun{Status: "Succeeded"}, nil
	}}
	o := landing.Operation{Phase: "branch_pushed", R: strings.Repeat("a", 40), S5: landing.S5State{Required: true}}
	got, err := candidateCI(context.Background(), src, "bifrost-platform", o)
	if err != nil || got.Status != "Missing" || calls != 0 {
		t.Fatal(got, err, calls)
	}
	// A pre-S5 operation remains on the old reader when the requirement is absent.
	o.S5 = landing.S5State{}
	_, err = candidateCI(context.Background(), src, "bifrost-platform", o)
	if err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}
