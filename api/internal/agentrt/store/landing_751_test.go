package store_test

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
)

// Called by the serial S6-5 PG scenario after real claim and Git preparation.
// Another repository consumes the shared quota through the real final gate;
// only the UTC day changes when the parked infra operation becomes eligible.
func s65QuotaRecovery751(t *testing.T, w *world, ctl *controller.Controller, src controller.DrillSources, op string, prepared landing.Operation, at time.Time, fake *dynfake.FakeDynamicClient, gvr schema.GroupVersionResource) time.Time {
	t.Helper()
	if prepared.Phase != "branch_pushed" || prepared.Kicked {
		t.Fatal("invalid starting preparation", prepared)
	}
	other := s66Root(t, w, "M-3", platformRepo)
	otherOp, d, err := store.ClaimLanding(ctx, w.rt, s66Source(), "prod", other, at)
	ok(t, err)
	if !d.Allowed || otherOp == "" {
		t.Fatal("platform could not claim the available quota", d)
	}
	prepareS6(t, w, platformRepo, other, otherOp, at)
	store.FreezeEventClock(at.Add(2 * time.Minute))
	_, d, err = store.AuthorizeLandingPush(ctx, w.rt, s66Source(), otherOp, observed(at), platformRepo)
	ok(t, err)
	if !d.Allowed {
		t.Fatal("platform did not consume the shared quota", d)
	}
	oldDay := "landing-quota:dev:" + at.Format("2006-01-02")
	if q := landingState(t, w, oldDay); len(q.Reservations) != 1 || q.Reservations[other] != otherOp {
		t.Fatal("missing platform quota reservation", q)
	}
	for _, minutes := range []int{11, 12} {
		store.FreezeEventClock(at.Add(time.Duration(minutes) * time.Minute))
		action, err := ctl.LandingDrillCI(ctx, src, op, at, infraRepo)
		ok(t, err)
		got := landingState(t, w, infraStream).Operations[op]
		if action.Kind != "parked" || action.Why != "limit" || got.Phase != "parked" || got.Resume != "branch_pushed" || got.Kicked {
			t.Fatal("quota did not produce the reported park", action, got)
		}
		s65SamePreparation751(t, prepared, got)
	}
	// The day advances by only 21 minutes from the root's claim, inside W=6h.
	resumedAt := at.Add(21 * time.Minute)
	store.FreezeEventClock(resumedAt)
	if q := landingState(t, w, "landing-quota:dev:"+resumedAt.Format("2006-01-02")); len(q.Reservations) != 0 {
		t.Fatal("new day has no free quota", q)
	}
	restarted := controller.Controller{DB: w.rt, Env: "prod", Live: true, LiveRepos: []string{infraRepo}}
	action, err := restarted.LandingDrillCI(ctx, src, op, at, infraRepo)
	ok(t, err) // 12d1ce12: parked -> ci_kick is illegal
	got := landingState(t, w, infraStream).Operations[op]
	if action.Kind != "unparked" || got.Phase != "branch_pushed" || got.Kicked {
		t.Fatal("first recovery pass must only unpark", action, got)
	}
	s65SamePreparation751(t, prepared, got)
	runs, err := fake.Resource(gvr).Namespace("cicd").List(ctx, metav1.ListOptions{})
	ok(t, err)
	if len(runs.Items) != 0 {
		t.Fatal("CI created before the next recovery pass")
	}
	var parked, unparked, kicks int
	ok(t, w.rt.QueryRow(ctx, `SELECT
      count(*) FILTER (WHERE body_json->'landing'->>'kind'='parked'),
      count(*) FILTER (WHERE body_json->'landing'->>'kind'='unparked'),
      count(*) FILTER (WHERE body_json->'landing'->>'kind'='ci_kick')
      FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing'->>'op'=$2`, infraStream, op).Scan(&parked, &unparked, &kicks))
	if parked != 1 || unparked != 1 || kicks != 0 {
		t.Fatal("unexpected quota recovery records", parked, unparked, kicks)
	}
	return resumedAt.Add(time.Minute)
}

func s65SamePreparation751(t *testing.T, want, got landing.Operation) {
	t.Helper()
	if got.R != want.R || got.M0 != want.M0 || got.Ref != want.Ref || got.PlatformRevision != want.PlatformRevision ||
		got.Generation != want.Generation || !got.PreparedAt.Equal(want.PreparedAt) || !got.CommitterDate.Equal(want.CommitterDate) {
		t.Fatal("quota recovery changed the preparation key", want, got)
	}
}
