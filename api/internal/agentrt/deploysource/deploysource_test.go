package deploysource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

type fakeCurrent map[string]releases.Record

func (f fakeCurrent) Current(_ context.Context, lane, env string) (releases.Record, error) {
	if lane != "platform" {
		return releases.Record{}, fmt.Errorf("lane %s", lane)
	}
	if env == "qa" {
		return releases.Record{}, releases.ErrUnsettled
	}
	r, ok := f[env]
	if !ok {
		return releases.Record{}, releases.ErrNoDeployment
	}
	return r, nil
}

// PodDigests: the pods run what the env's record says, but in "drift",
// where the workers pulled another image, and "podless", where no pod reads.
func (f fakeCurrent) PodDigests(_ context.Context, namespace, deployment, container string) ([]string, error) {
	env := strings.TrimPrefix(namespace, "bifrost-platform-")
	switch {
	case env == "drift" && deployment == "platform-workers":
		return []string{digestA, digestOther}, nil
	case env == "podless":
		return nil, errors.New("no pod")
	}
	return []string{f[env].Images[container]}, nil
}

var (
	sha1        = "1111111111111111111111111111111111111111"
	sha2        = "2222222222222222222222222222222222222222"
	digestA     = "sha256:" + strings.Repeat("a", 64)
	digestC     = "sha256:" + strings.Repeat("c", 64)
	digestOther = "sha256:" + strings.Repeat("e", 64)
)

func platformRec(run, sha string) releases.Record {
	r := releases.Record{Run: run, Lane: "platform", Deploys: true, Repos: map[string]releases.RepoBuild{},
		Images: map[string]string{"platform-api": digestA, "platform-console": digestC}}
	if sha != "" {
		r.Repos["bifrost-platform"] = releases.RepoBuild{SHA: sha}
	}
	return r
}

type productionEvidenceFake struct {
	fakeCurrent
	pods    []string
	podErr  error
	changed bool
	calls   int
}

func (f *productionEvidenceFake) Current(ctx context.Context, lane, env string) (releases.Record, error) {
	f.calls++
	r, err := f.fakeCurrent.Current(ctx, lane, env)
	if f.changed && f.calls > 2 {
		r.Run += "-new"
	}
	return r, err
}
func (f *productionEvidenceFake) PodDigests(_ context.Context, _, _, container string) ([]string, error) {
	if f.podErr != nil || f.pods != nil {
		return f.pods, f.podErr
	}
	return []string{f.fakeCurrent["stg"].Images[container]}, nil
}
func TestProductionDeliveryEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*productionEvidenceFake)
	}{
		{"valid promotion without PROD image results", nil},
		{"missing record", func(f *productionEvidenceFake) { delete(f.fakeCurrent, "prod") }},
		{"wrong version", func(f *productionEvidenceFake) {
			r := f.fakeCurrent["stg"]
			r.Revision = sha2
			r.Repos = map[string]releases.RepoBuild{Repo: {SHA: sha2}}
			f.fakeCurrent["stg"] = r
		}},
		{"missing digest", func(f *productionEvidenceFake) { delete(f.fakeCurrent["stg"].Images, "platform-console") }},
		{"empty pods", func(f *productionEvidenceFake) { f.pods = []string{} }},
		{"drift", func(f *productionEvidenceFake) { f.pods = []string{digestOther} }},
		{"unsettled", func(f *productionEvidenceFake) { f.podErr = releases.ErrUnsettled }},
		{"concurrent release", func(f *productionEvidenceFake) { f.changed = true }},
		{"later STG rebuild", func(f *productionEvidenceFake) {
			r := f.fakeCurrent["stg"]
			r.CompletedAt = r.CompletedAt.Add(time.Hour)
			f.fakeCurrent["stg"] = r
		}},
		{"wrong pipeline", func(f *productionEvidenceFake) {
			r := f.fakeCurrent["prod"]
			r.Pipeline = "ci-platform"
			f.fakeCurrent["prod"] = r
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
			f := &productionEvidenceFake{fakeCurrent: fakeCurrent{}}
			for i, env := range []string{"stg", "prod"} {
				r := platformRec("release-"+env, sha1)
				r.Env = env
				r.Revision = sha1
				r.Pipeline = "bifrost-deliver-platform"
				if env == "prod" {
					r.Pipeline += "-prod"
					r.Images = nil
				}
				r.StartedAt = at.Add(time.Duration(i) * 10 * time.Minute)
				r.CompletedAt = r.StartedAt.Add(time.Minute)
				f.fakeCurrent[env] = r
			}
			if tc.change != nil {
				tc.change(f)
			}
			got := ProductionEvidence(context.Background(), f)
			if tc.change == nil {
				if got.Reason != "" || got.Commit != sha1 || !got.CompletedAt.Equal(f.fakeCurrent["prod"].CompletedAt) {
					t.Fatalf("unexpected evidence: %+v", got)
				}
			} else if got.Reason == "" || got.Commit != "" {
				t.Fatalf("incomplete evidence accepted: %+v", got)
			}
		})
	}
}

// STG ahead of the newest PROD record: the settled PROD workloads tell a PROD
// that waits for the STG build (NotCommit names it) from one that already
// runs it while its release record is not written yet. Either way the
// evidence stays unknown, and nothing is said when a workload cannot be read.
func TestProductionDeliveryEvidenceWhenSTGIsAhead(t *testing.T) {
	at := time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		change    func(*productionEvidenceFake)
		notCommit string
		reason    string
	}{
		{"PROD runs another build", func(f *productionEvidenceFake) { f.pods = []string{digestOther} }, sha2, "does not match the promoted PROD revision"},
		{"PROD runs the STG build already", nil, "", "no PROD release record names it yet"},
		{"a rollout in progress", func(f *productionEvidenceFake) { f.pods = []string{digestA, digestOther} }, sha2, "does not match"},
		{"workloads unsettled", func(f *productionEvidenceFake) { f.podErr = releases.ErrUnsettled }, "", "does not match"},
		{"no pods", func(f *productionEvidenceFake) { f.pods = []string{} }, "", "does not match"},
		{"STG record without a digest", func(f *productionEvidenceFake) { delete(f.fakeCurrent["stg"].Images, "platform-console") }, "", "does not match"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &productionEvidenceFake{fakeCurrent: fakeCurrent{}}
			for env, sha := range map[string]string{"prod": sha1, "stg": sha2} {
				r := platformRec("release-"+env, sha)
				r.Env, r.Revision, r.Pipeline = env, sha, "bifrost-deliver-platform"
				r.StartedAt = at
				if env == "prod" {
					r.Pipeline += "-prod"
					r.Images = nil
				} else {
					r.StartedAt = at.Add(time.Hour)
				}
				r.CompletedAt = r.StartedAt.Add(time.Minute)
				f.fakeCurrent[env] = r
			}
			if tc.change != nil {
				tc.change(f)
			}
			got := ProductionEvidence(context.Background(), f)
			if got.Commit != "" || got.NotCommit != tc.notCommit || !strings.Contains(got.Reason, tc.reason) {
				t.Fatalf("evidence %+v; want unknown naming %q with NotCommit %q", got, tc.reason, tc.notCommit)
			}
		})
	}
}
