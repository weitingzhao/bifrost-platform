package planstore_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

// What the plan tests in this package take of the view and its bindings
// (copies of the helpers of store's planverification_test.go, which stays in
// store because it runs the controller).
func pvDocument(t *testing.T, version int) *planview.Document {
	t.Helper()
	raw, err := os.ReadFile("../planview/testdata/r4-v1.json")
	ok(t, err)
	d, err := planview.Parse(raw)
	ok(t, err)
	d.Root = root
	d.Version = version
	d.Items = d.Items[:1]
	n := &d.Items[0]
	n.Node = "2.1"
	n.Parent = root
	n.Dependencies = []planview.Edge{}
	n.Anchor = nil
	n.Milestones = []string{"accepted"}
	n.Acceptance = n.Acceptance[:1]
	n.Acceptance[0].Milestone = "accepted"
	n.Acceptance[0].ID = "result"
	n.Acceptance[0].Rule = "main_effective"
	for i := range n.Evidence {
		n.Evidence[i].Milestone = "accepted"
	}
	return d
}
func pvBinding(d *planview.Document, step, atom, source string) planview.Binding {
	return planview.Binding{V: 1, Kind: planview.BindingKind, ID: step + "-" + atom, Version: d.Version, Tenant: "dev", Root: root, Node: "2.1", Step: step, Acceptance: d.Items[0].Acceptance[0].ID, AcceptanceSHA256: planview.AcceptanceDigest(d.Items[0].Acceptance[0]), Atom: atom, Source: source}
}
func pvArtifact(t *testing.T, w *world, d *planview.Document, bs ...planview.Binding) string {
	t.Helper()
	d.Items[0].Bindings = []json.RawMessage{}
	for _, b := range bs {
		raw, err := json.Marshal(b)
		ok(t, err)
		d.Items[0].Bindings = append(d.Items[0].Bindings, raw)
	}
	raw, err := json.Marshal(d)
	ok(t, err)
	var id string
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var e error
		id, e = artifact.PutArtifact(ctx, tx, "controller", "other", "application/json", raw)
		return e
	}))
	return id
}
func pvPoll(t *testing.T, w *world, src planstore.PlanSources) {
	t.Helper()
	_, err := planstore.CollectPlanObservations(ctx, w.rt, src)
	ok(t, err)
}
func pvEvaluate(t *testing.T, w *world, step, want string) {
	t.Helper()
	_, err := planstore.EvaluateStep(ctx, w.rt, step, nil)
	ok(t, err)
	if got := w.stepState(t, step); got != want {
		t.Fatalf("step %s = %s; want %s", step, got, want)
	}
}
