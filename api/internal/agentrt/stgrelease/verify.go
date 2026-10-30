package stgrelease

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
	"k8s.io/apimachinery/pkg/types"
)

const rolloutGrace = 15 * time.Minute

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func restored(t *store.StgTry) (delivery.STGRunObservation, error) {
	id := delivery.STGRunIdentity{Namespace: object(t.Snapshot.Record, "inputs").String("namespace"), Name: t.Started.Record.String("run"), UID: types.UID(t.RunEvidence.Record.String("uid"))}
	return delivery.RestoreSTGRun(id, t.RunEvidence.Record.String("status"), map[string]any(object(t.RunEvidence.Record, "raw")))
}
func expected(t *store.StgTry) delivery.STGContent {
	in := object(t.Snapshot.Record, "inputs")
	return delivery.STGContent{P: in.String("revision"), U: in.String("uiRevision"), Infra: in.String("infraRevision")}
}
func (c *Controller) unknown(ctx context.Context, s store.StgState, id string, b *store.StgBatch) error {
	if r := diagnostic(b, id, "verify_unknown", 1); r != nil {
		return c.write(ctx, s, r)
	}
	return nil
}
func (c *Controller) failVerify(ctx context.Context, s store.StgState, id string, b *store.StgBatch, why string) error {
	return c.write(ctx, s, batchRecord("verify", id, b.Try, "outcome", "failed", "why", why, "evidence", ""))
}
func (c *Controller) verify(ctx context.Context, s store.StgState, id string, b *store.StgBatch, t *store.StgTry) error {
	archived, err := restored(t)
	if err != nil {
		return c.unknown(ctx, s, id, b)
	}
	// A terminal failure is retryable only after its actual clones have
	// matched the frozen inputs. Builds may be incomplete at this point.
	err = archived.CheckInputs(expected(t))
	if errors.Is(err, delivery.ErrSTGWrongInput) {
		return c.failVerify(ctx, s, id, b, "wrong_input")
	}
	if err != nil {
		return c.unknown(ctx, s, id, b)
	}
	if archived.Status == "Failed" {
		return c.failVerify(ctx, s, id, b, "run_failed")
	}
	content, err := archived.Content(expected(t))
	if errors.Is(err, delivery.ErrSTGWrongInput) {
		return c.failVerify(ctx, s, id, b, "wrong_input")
	}
	if err != nil || c.Runs == nil {
		return c.unknown(ctx, s, id, b)
	}
	workloads, workErr := c.Runs.ObserveWorkloads(ctx)
	if workErr != nil && !definite(workErr) {
		return c.unknown(ctx, s, id, b)
	}
	own := workErr == nil && workloadsMatch(workloads, content.APIDigest, content.ConsoleDigest)
	if !own && workErr == nil {
		check, err := c.checkReplacement(ctx, s, t, workloads)
		if err != nil {
			return c.unknown(ctx, s, id, b)
		}
		if check != nil {
			return c.write(ctx, s, check, batchRecord("verify", id, b.Try, "outcome", "superseded", "why", "external_release", "evidence", check["evidence"]))
		}
	}
	pinErr := error(nil)
	if own {
		_, pinErr = c.Runs.ReadPin(ctx, content.APIDigest, content.ConsoleDigest)
	}
	if pinErr != nil && !definite(pinErr) {
		return c.unknown(ctx, s, id, b)
	}
	why := ""
	var health delivery.STGHealth
	if !own || pinErr != nil {
		why = "rollout"
	} else {
		health = c.Runs.ReadHealth(ctx)
		if health.Outcome == "unknown" {
			return c.unknown(ctx, s, id, b)
		}
		if health.Outcome != "passed" {
			why = "health"
		}
	}
	if why != "" {
		if c.now().Sub(t.RunEvidence.Record.Time("at")) < rolloutGrace {
			return nil
		}
		return c.failVerify(ctx, s, id, b, why)
	}
	evidenceID := fmt.Sprintf("%s/%s/%s", c.Cluster, archived.Identity.Namespace, archived.Identity.UID)
	evidence := store.StgRecord{"v": "1", "kind": "evidence", "scope": "ext", "evidence": evidenceID, "source": "automatic", "batch": id, "run": archived.Identity.Name, "P": content.P, "U": content.U, "infra": content.Infra, "content": map[string]any{"api_digest": content.APIDigest, "console_digest": content.ConsoleDigest}, "deployed": map[string]any{"pin_sync_ok": true, "workloads": workloads, "at": c.now().Format(time.RFC3339Nano)}, "health": health.Evidence}
	rs := []store.StgRecord{}
	if s.Evidence[evidenceID].Seq == 0 {
		rs = append(rs, evidence)
	}
	rs = append(rs, batchRecord("verify", id, b.Try, "outcome", "verified", "why", "verified", "evidence", evidenceID))
	return c.write(ctx, s, rs...)
}
func definite(err error) bool {
	return errors.Is(err, delivery.ErrSTGEvidence) || errors.Is(err, releases.ErrUnsettled)
}
func workloadsMatch(workloads map[string]any, api, console string) bool {
	if !digestPattern.MatchString(api) || !digestPattern.MatchString(console) || len(workloads) != 3 {
		return false
	}
	for _, name := range []string{"platform-api", "platform-workers", "platform-console"} {
		w := object(store.StgRecord(workloads), name)
		want := api
		if name == "platform-console" {
			want = console
		}
		if w.String("uid") == "" || w.Int("generation") < 1 || w.String("digest") != want {
			return false
		}
	}
	return true
}
func effectClass(workloads map[string]any, base store.StgRecord, api, console string) string {
	if len(workloads) != 3 {
		return "unknown"
	}
	class := "unchanged"
	for _, name := range []string{"platform-api", "platform-workers", "platform-console"} {
		w := object(store.StgRecord(workloads), name)
		before, after := base.String("api_digest"), api
		if name == "platform-console" {
			before, after = base.String("console_digest"), console
		}
		d := w.String("digest")
		if w.String("uid") == "" || w.Int("generation") < 1 || !digestPattern.MatchString(d) {
			return "unknown"
		}
		if d == before {
			continue
		}
		if after != "" && d == after {
			if class != "other" {
				class = "this_try"
			}
			continue
		}
		class = "other"
	}
	return class
}
func (c *Controller) conclude(ctx context.Context, s store.StgState, id string, b *store.StgBatch, t *store.StgTry) error {
	switch t.Verify.Record.String("outcome") {
	case "verified":
		return c.write(ctx, s, finish(s, id, b, t.Verify.Seq, true)...)
	case "superseded":
		return c.concludeRemaining(ctx, s, id, b, t, true)
	}
	base := object(t.Snapshot.Record, "base")
	if t.Verify.Record.String("why") == "wrong_input" {
		return c.blockAndFinish(ctx, s, id, b, "wrong_input", base, nil)
	}
	if t.Effect.Seq == 0 || t.Effect.Record.String("class") == "unknown" {
		workloads := map[string]any{"reason": "unreadable"}
		class := "unknown"
		if c.Runs != nil {
			if read, err := c.Runs.ObserveWorkloads(ctx); err == nil {
				workloads = read
				var api, console string
				if archived, restoreErr := restored(t); restoreErr == nil {
					api, console = archived.BuildDigests()
				}
				class = effectClass(workloads, base, api, console)
			}
		}
		if class == "unknown" && t.Effect.Seq > 0 {
			return c.unknown(ctx, s, id, b)
		}
		return c.write(ctx, s, batchRecord("effect", id, b.Try, "class", class, "workloads", workloads, "at", c.now().Format(time.RFC3339Nano)))
	}
	if t.Effect.Record.String("class") == "other" {
		return c.concludeOther(ctx, s, id, b, t)
	}
	return c.concludeRemaining(ctx, s, id, b, t, false)
}

// Replacement deployments always hand off; unchanged/this_try may retry.
// Both use the same member outcomes and budget decision, with any fresh
// deployment check committed atomically alongside the resulting settlement.
func (c *Controller) concludeRemaining(ctx context.Context, s store.StgState, id string, b *store.StgBatch, t *store.StgTry, handoff bool, checks ...store.StgRecord) error {
	// Consume all known delivery facts before deciding whether any still
	// pending member needs a budget interval. A map's iteration order must
	// never let an already covered/exhausted member decide for its peers.
	exhausted := false
	for m, gen := range b.Members {
		if s.Withheld[m].Seq > 0 || s.Members[m].To != "" || cover(s, m) != "" || reversal(s, m) > 0 || uncoveredEvidence(s, m) != "" {
			handoff = true
			continue
		}
		exhausted = exhausted || s.Budget[store.StgBudgetKey{R: m.R, Gen: gen}] >= 3
	}
	if exhausted {
		return c.blockAndFinish(ctx, s, id, b, "budget_exhausted", object(t.Snapshot.Record, "base"), nil, checks...)
	}
	if handoff {
		return c.write(ctx, s, append(checks, finishMembers(s, id, b, t.Verify.Seq, false, true)...)...)
	}
	due := c.now()
	for m := range b.Members {
		if s.Debits[m.R].NotBefore.After(due) {
			due = s.Debits[m.R].NotBefore
		}
	}
	return c.write(ctx, s, batchRecord("retry", id, b.Try+1, "not_before", due.Format(time.RFC3339Nano)))
}
