// Package deploysource reads what the platform's own deployment is, from the
// release records and the pods that run: the commit and images the runtime's
// standing conditions compare, and the PROD delivery evidence.
package deploysource

import (
	"context"
	"reflect"
	"regexp"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

// Lane and Repo are the delivery lane and repository whose deployments the runtime's standing
// conditions (gates_passed) read. Round 2's work is the platform itself
// (W-31); release records are per lane and per repository, and the rule atom
// names only the environment. Trade joins when business work starts.
const (
	// Lane is the delivery lane.
	Lane = "platform"
	// Repo is the repository.
	Repo = "bifrost-platform"
)

// workloads are the platform's own workloads, the container each
// runs and the image the delivery built for it, as the delivery's rollout
// check verifies them (bifrost-trade-infra task-rollout-verify-platform:
// the workers run the API image). They live in bifrost-platform-<env>.
var workloads = []struct{ deployment, container, image string }{
	{"platform-api", "platform-api", "platform-api"},
	{"platform-workers", "platform-api", "platform-api"},
	{"platform-console", "platform-console", "platform-console"},
}

// Deployments is what the deployed-state reads need of the release records.
type Deployments interface {
	Current(ctx context.Context, lane, env string) (releases.Record, error)
	PodDigests(ctx context.Context, namespace, deployment, container string) ([]string, error)
}

// Deployed reads the deployment of an environment for the runtime's standing
// conditions: its repository's commit and the reference the gate check
// records. ok is false for what cannot be read now (see deployedFrom).
func Deployed(rel Deployments) func(context.Context, string) (string, map[string]any, bool) {
	return func(ctx context.Context, env string) (string, map[string]any, bool) {
		// The controller's facts and rules pass asks this every fifteen
		// seconds for each deploy watch and step (SeeDeploy, EvaluateStep),
		// and each answer lists the deploying pipeline's runs
		// (releases.Current). It had no name: PROD, 2026-11-17 02:09 CST,
		// counted 80 unnamed LISTs of PipelineRuns in ten minutes, which is
		// one read a pass at two pages; the daily full CI's reads, taken
		// for them, list only while a red run is first handled.
		ctx = kubemetrics.WithSite(ctx, "agentrt.deployed")
		rec, err := rel.Current(ctx, Lane, env)
		if err != nil {
			return "", nil, false
		}
		commit, ref, ok := deployedFrom(rec)
		if !ok || !runsRecorded(ctx, rel, env, rec) {
			return "", nil, false
		}
		return commit, ref, true
	}
}

// deployedFrom is what the record says the environment runs: its
// repository's commit, and as the reference the delivery run and the image
// digests it built (TD-343), so a gate check records exactly what ran and
// gates_passed compares it whole. A record without the digests of every
// workload's image (one older than the digests, or a pipeline that reports
// none) is not a known deployment.
func deployedFrom(rec releases.Record) (string, map[string]any, bool) {
	b, ok := rec.Repos[Repo]
	if !ok || len(b.SHA) != 40 || rec.Run == "" {
		return "", nil, false
	}
	images := map[string]any{}
	for _, w := range workloads {
		d, has := rec.Images[w.image]
		if !has || d == "" {
			return "", nil, false
		}
		images[w.image] = d
	}
	return b.SHA, map[string]any{"delivery_run": rec.Run, "images": images}, true
}

// runsRecorded: every pod of every workload runs the digest the record says
// was delivered. A pod that pulled another image under the same tag, a
// rollout under way, or a pod that cannot be read makes the deployment not
// known (TD-343; 决定 2026-10-14).
func runsRecorded(ctx context.Context, rel Deployments, env string, rec releases.Record) bool {
	for _, w := range workloads {
		digests, err := rel.PodDigests(ctx, "bifrost-platform-"+env, w.deployment, w.container)
		if err != nil {
			return false
		}
		for _, d := range digests {
			if d != rec.Images[w.image] {
				return false
			}
		}
	}
	return true
}

// ProductionEvidence joins existing release records with live settled
// workloads. It writes no legacy cycle or synthetic deployment event.
func ProductionEvidence(ctx context.Context, rel Deployments) deliveryview.DeliveryProduction {
	unknown := func(reason string) deliveryview.DeliveryProduction {
		return deliveryview.DeliveryProduction{Reason: reason}
	}
	prod, err := rel.Current(ctx, Lane, "prod")
	if err != nil {
		return unknown("current PROD release is unavailable or unsettled")
	}
	stg, err := rel.Current(ctx, Lane, "stg")
	if err != nil {
		return unknown("matching STG release is unavailable or unsettled")
	}
	sha := prod.Repos[Repo].SHA
	for env, rec := range map[string]releases.Record{"prod": prod, "stg": stg} {
		pipeline := "bifrost-deliver-platform"
		if env == "prod" {
			pipeline += "-prod"
		}
		if rec.Run == "" || rec.Lane != Lane || rec.Env != env || !rec.Deploys || rec.Pipeline != pipeline ||
			!regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(rec.Repos[Repo].SHA) || rec.Revision != rec.Repos[Repo].SHA ||
			rec.StartedAt.IsZero() || rec.CompletedAt.IsZero() || rec.CompletedAt.Before(rec.StartedAt) {
			return unknown("release identity or completion evidence is incomplete")
		}
	}
	if stgSHA := stg.Repos[Repo].SHA; stgSHA != sha {
		// STG is ahead of the newest PROD record. The settled PROD workloads
		// say whether its build already runs there (a PROD release whose
		// record is not written yet) or does not (PROD waits for it).
		out := unknown("current STG evidence does not match the promoted PROD revision")
		switch runs, read := prodRunsBuild(ctx, rel, stg); {
		case !read:
		case runs:
			out.Reason = "PROD workloads run the newest STG build, and no PROD release record names it yet"
		default:
			out.NotCommit = stgSHA
		}
		return out
	}
	if stg.CompletedAt.After(prod.StartedAt) {
		return unknown("current STG evidence does not match the promoted PROD revision")
	}
	for _, w := range workloads {
		expected := stg.Images[w.image]
		if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(expected) {
			return unknown("matching STG image digest is missing")
		}
		digests, err := rel.PodDigests(ctx, "bifrost-platform-prod", w.deployment, w.container)
		if err != nil || len(digests) == 0 {
			return unknown("PROD workloads are unavailable or unsettled")
		}
		for _, digest := range digests {
			if digest != expected {
				return unknown("PROD workload digest differs from the matching STG release")
			}
		}
	}
	// Refuse a read spanning a concurrent release, including same-commit rebuilds.
	for env, before := range map[string]releases.Record{"prod": prod, "stg": stg} {
		after, err := rel.Current(ctx, Lane, env)
		if err != nil || !reflect.DeepEqual(before, after) {
			return unknown("release evidence changed while checking PROD workloads")
		}
	}
	return deliveryview.DeliveryProduction{Commit: sha, StartedAt: prod.StartedAt, CompletedAt: prod.CompletedAt}
}

// prodRunsBuild reports whether every settled PROD workload runs the image
// the STG release rec built for it. read is false when a digest of rec or a
// workload cannot be read: then nothing is said either way.
func prodRunsBuild(ctx context.Context, rel Deployments, rec releases.Record) (runs, read bool) {
	runs = true
	for _, w := range workloads {
		expected := rec.Images[w.image]
		if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(expected) {
			return false, false
		}
		digests, err := rel.PodDigests(ctx, "bifrost-platform-prod", w.deployment, w.container)
		if err != nil || len(digests) == 0 {
			return false, false
		}
		for _, digest := range digests {
			if digest != expected {
				runs = false
			}
		}
	}
	return runs, true
}
