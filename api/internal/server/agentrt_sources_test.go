package server

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

type fakeApprovals map[string]approvals.Approval

func (f fakeApprovals) Lookup(ref string) (approvals.Approval, bool, error) {
	if ref == "13" {
		return approvals.Approval{}, false, errors.New("store unreadable")
	}
	a, ok := f[ref]
	return a, ok, nil
}

// question answers as questions_wire.go's reader does, from plain fields.
func question(ctx context.Context, id string) (store.Question, bool) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.FixedZone("CDT", -5*3600))
	switch id {
	case "q_open":
		return questionFact(id, "open", false, "", "", "", time.Time{})
	case "q_done":
		return questionFact(id, "answered", true, "a", "go", "owner", at)
	case "q_textual":
		return questionFact(id, "answered", true, "", "just text", "owner", at)
	case "q_nobody":
		return questionFact(id, "answered", true, "a", "", "", at)
	case "q_untimed":
		return questionFact(id, "answered", true, "a", "", "owner", time.Time{})
	}
	return store.Question{}, false
}

type fakeCI struct{}

func (fakeCI) CIRun(_ context.Context, repo, sha string) (string, string, error) {
	switch repo + "@" + sha {
	case "bifrost-platform@ok":
		return "Succeeded", "ci-platform-1", nil
	case "bifrost-platform@broken":
		return "", "", errors.New("cluster unreachable")
	}
	return "", "", nil
}

type fakeReleases []releases.Record

func (f fakeReleases) List(context.Context) ([]releases.Record, error) { return f, nil }

var (
	sha1 = "1111111111111111111111111111111111111111"
	sha2 = "2222222222222222222222222222222222222222"
	sha3 = "3333333333333333333333333333333333333333"
)

func platformRec(run, env string, deploys bool, sha string) releases.Record {
	r := releases.Record{Run: run, Lane: "platform", Env: env, Deploys: deploys, Repos: map[string]releases.RepoBuild{}}
	if sha != "" {
		r.Repos["bifrost-platform"] = releases.RepoBuild{SHA: sha}
	}
	return r
}

// The controller's sources read the platform's stores (design v5 §5.2 外部事实).
func TestAgentRuntimeSources(t *testing.T) {
	src := agentRuntimeSources(
		fakeApprovals{"7": {Number: 7, Status: approvals.StatusExecuted}, "8": {Number: 8}},
		question,
		fakeCI{},
		fakeReleases{
			{Run: "deliver-stg-trade", Lane: "trade", Env: "stg", Deploys: true, Repos: map[string]releases.RepoBuild{"bifrost-platform": {SHA: sha3}}},
			platformRec("build-only", "stg", false, sha3),
			platformRec("deliver-platform-2", "stg", true, sha2),
			platformRec("deliver-platform-1", "stg", true, sha1),
			platformRec("deliver-platform-prod-1", "prod", true, sha1),
		},
	)
	ctx := context.Background()

	if s, ok := src.Approval(ctx, 7); !ok || s != "executed" {
		t.Errorf("approval 7 = %q %v", s, ok)
	}
	for _, n := range []int64{8, 9, 13} { // no status, missing, store unreadable
		if s, ok := src.Approval(ctx, n); ok {
			t.Errorf("approval %d = %q, read", n, s)
		}
	}

	if q, ok := src.Question(ctx, "q_open"); !ok || q.Status != "open" || q.Option != "" {
		t.Errorf("open question = %+v %v", q, ok)
	}
	want := store.Question{ID: "q_done", Status: "answered", Option: "a", Text: "go", AnsweredBy: "owner", AnsweredAt: "2026-10-14T17:00:00Z"}
	if q, ok := src.Question(ctx, "q_done"); !ok || q != want {
		t.Errorf("answered question = %+v %v", q, ok)
	}
	for _, id := range []string{"q_textual", "q_nobody", "q_untimed", "q_missing"} {
		if q, ok := src.Question(ctx, id); ok {
			t.Errorf("%s = %+v, read", id, q)
		}
	}

	if s, run, ok := src.CI(ctx, "bifrost-platform", "ok"); !ok || s != "Succeeded" || run != "ci-platform-1" {
		t.Errorf("ci = %q %q %v", s, run, ok)
	}
	for _, sha := range []string{"broken", "none"} {
		if _, _, ok := src.CI(ctx, "bifrost-platform", sha); ok {
			t.Errorf("ci %s read", sha)
		}
	}

	for env, want := range map[string]string{"stg": sha2 + " map[delivery_run:deliver-platform-2]", "prod": sha1 + " map[delivery_run:deliver-platform-prod-1]"} {
		commit, ref, ok := src.Deployed(ctx, env)
		if got := commit + " " + fmt.Sprint(ref); !ok || got != want {
			t.Errorf("deployed %s = %s %v", env, got, ok)
		}
	}
	if _, _, ok := src.Deployed(ctx, "dev"); ok {
		t.Error("dev has no deploying record and was read")
	}
	// The newest record of the lane that cannot be read is not passed over
	// for an older one.
	unreadable := agentRuntimeSources(nil, nil, nil, fakeReleases{platformRec("deliver-platform-3", "stg", true, ""), platformRec("deliver-platform-1", "stg", true, sha1)})
	if commit, _, ok := unreadable.Deployed(ctx, "stg"); ok {
		t.Errorf("an older deployment = %s", commit)
	}

	none := agentRuntimeSources(nil, nil, nil, nil)
	if _, ok := none.Approval(ctx, 7); ok {
		t.Error("no approvals store, read")
	}
	if _, ok := none.Question(ctx, "q_done"); ok {
		t.Error("no question store, read")
	}
	if _, _, ok := none.CI(ctx, "bifrost-platform", "ok"); ok {
		t.Error("no CI reader, read")
	}
	if _, _, ok := none.Deployed(ctx, "stg"); ok {
		t.Error("no release records, read")
	}
}

// A store read that does not take a context still ends with it.
func TestSourceReadEndsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	release := make(chan struct{})
	defer close(release)
	start := time.Now()
	_, ok, err := within(ctx, func() (int, bool, error) { <-release; return 1, true, nil })
	if ok || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("within = %v %v after %s", ok, err, time.Since(start))
	}
}
