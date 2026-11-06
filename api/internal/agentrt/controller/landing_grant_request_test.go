package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const grantTestTask = "t_00000000000000000000000001"

var (
	grantTestOp = "land:" + grantTestTask + ":1"
	grantTestR  = strings.Repeat("a", 40)
)

type grantAsk struct {
	action, reason string
	params         map[string]any
}

// grantRequestWorld is a controller whose Requests and RequestGrant are
// fakes: the approvals it answers are the ones it was asked for.
type grantRequestWorld struct {
	c         *Controller
	phase     string
	hardCore  bool
	usable    string
	held      bool
	approvals []store.LandingGrantRequest
	asks      []grantAsk
	requestFn func(ctx context.Context, action string, params map[string]any, reason string) (string, error)
}

func newGrantRequestWorld(t *testing.T) *grantRequestWorld {
	w := &grantRequestWorld{phase: "awaiting_grant"}
	w.requestFn = func(_ context.Context, action string, params map[string]any, reason string) (string, error) {
		w.asks = append(w.asks, grantAsk{action, reason, params})
		r, _ := params["R"].(string)
		w.approvals = append(w.approvals, store.LandingGrantRequest{Op: params["op"].(string), R: r, ApprovalID: "approval_1", ApprovalNumber: 1, Status: "pending"})
		return "approval_1", nil
	}
	w.c = &Controller{Env: "prod", Reconcile: &LandingReconcile{
		Repos: []string{"bifrost-platform"},
		Requests: func(context.Context) ([]store.LandingGrantRequest, error) {
			return append([]store.LandingGrantRequest(nil), w.approvals...), nil
		},
		Grants: func() store.GrantSources { return store.GrantSources{} },
		RequestGrant: func(ctx context.Context, action string, params map[string]any, reason string) (string, error) {
			return w.requestFn(ctx, action, params, reason)
		},
	}}
	old, oldRecord := grantRequestReads, recordLandingGrantRequest
	t.Cleanup(func() { grantRequestReads, recordLandingGrantRequest = old, oldRecord })
	recordLandingGrantRequest = func(context.Context, store.DB, store.LandingGrantRequest) (bool, error) { return true, nil }
	grantRequestReads.state = func(_ context.Context, _ store.DB, repo string) (landing.State, error) {
		if repo != "bifrost-platform" {
			t.Fatalf("read %s", repo)
		}
		return landing.State{LiveAt: time.Now().Add(-time.Hour), Operations: map[string]landing.Operation{
			grantTestOp: {Phase: w.phase, Task: grantTestTask, R: grantTestR, ClaimNumber: 1},
		}}, nil
	}
	grantRequestReads.usable = func(context.Context, store.DB, string) (string, error) { return w.usable, nil }
	grantRequestReads.held = func(context.Context, store.DB, string) (bool, error) { return w.held, nil }
	grantRequestReads.material = func(_ context.Context, _ store.DB, _ store.GrantSources, op string) (*store.LandingGrantMaterial, error) {
		m := &store.LandingGrantMaterial{Candidate: store.GrantCandidate{Repo: "bifrost-platform", Task: grantTestTask, Op: op, R: grantTestR},
			MaterialDigest: "sha256:" + strings.Repeat("b", 64), SoftAtRequest: []string{"files"}, Action: store.ActionGuardrailLanding}
		if w.hardCore {
			m.Candidate.HardCorePaths = []store.GrantHardPath{{Path: "api/internal/approvals/service.go", Entry: "approvals"}}
			m.SoftAtRequest, m.Action = []string{"core"}, store.ActionCoreLanding
		}
		return m, nil
	}
	return w
}

func (w *grantRequestWorld) pass(t *testing.T) {
	t.Helper()
	if err := w.c.LandingRequestsPass(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestGrantRequestAwaitingGrantAsksOnce(t *testing.T) {
	for _, core := range []bool{false, true} {
		w := newGrantRequestWorld(t)
		w.hardCore = core
		w.pass(t)
		if len(w.asks) != 1 {
			t.Fatalf("core=%t: %d requests, want 1", core, len(w.asks))
		}
		a := w.asks[0]
		want := store.ActionGuardrailLanding
		if core {
			want = store.ActionCoreLanding
		}
		if a.action != want {
			t.Fatalf("action %s, want %s", a.action, want)
		}
		p, err := store.DecodeGrantParams(a.params)
		if err != nil {
			t.Fatalf("params %v: %v", a.params, err)
		}
		if p.Op != grantTestOp || p.Task != grantTestTask || p.R != grantTestR || p.Repo != "bifrost-platform" || p.STGRelease ||
			p.MaterialDigest != "sha256:"+strings.Repeat("b", 64) {
			t.Fatalf("params %+v", p)
		}
		if !strings.Contains(a.reason, grantTestTask) || !strings.Contains(a.reason, grantTestOp) || !strings.Contains(a.reason, "awaiting_grant") {
			t.Fatalf("reason %q", a.reason)
		}
		// The next pass reads the pending request and asks nothing.
		w.pass(t)
		if len(w.asks) != 1 {
			t.Fatalf("core=%t: second pass asked again (%d)", core, len(w.asks))
		}
	}
}

func TestGrantRequestExistingApprovalAsksNothing(t *testing.T) {
	for _, status := range []string{"pending", "approved", "running", "rejected", "expired"} {
		w := newGrantRequestWorld(t)
		w.approvals = []store.LandingGrantRequest{{Op: grantTestOp, R: grantTestR, ApprovalID: "approval_9", ApprovalNumber: 9, Status: status}}
		w.pass(t)
		if len(w.asks) != 0 {
			t.Fatalf("%s: asked %d", status, len(w.asks))
		}
	}
	// An answer for an earlier R does not answer this one; an open request
	// for the op stands whatever its R.
	w := newGrantRequestWorld(t)
	w.approvals = []store.LandingGrantRequest{{Op: grantTestOp, R: strings.Repeat("c", 40), ApprovalID: "approval_9", ApprovalNumber: 9, Status: "rejected"}}
	w.pass(t)
	if len(w.asks) != 1 {
		t.Fatalf("rejected for another R: asked %d", len(w.asks))
	}
	w = newGrantRequestWorld(t)
	w.approvals = []store.LandingGrantRequest{{Op: grantTestOp, R: strings.Repeat("c", 40), ApprovalID: "approval_9", ApprovalNumber: 9, Status: "pending"}}
	w.pass(t)
	if len(w.asks) != 0 {
		t.Fatalf("pending for another R: asked %d", len(w.asks))
	}
}

func TestGrantRequestOnlyAwaitingGrant(t *testing.T) {
	w := newGrantRequestWorld(t)
	w.phase = "branch_pushed"
	w.pass(t)
	if len(w.asks) != 0 {
		t.Fatalf("branch_pushed: asked %d", len(w.asks))
	}
	w = newGrantRequestWorld(t)
	w.usable = "grant_1"
	w.pass(t)
	if len(w.asks) != 0 {
		t.Fatalf("usable grant: asked %d", len(w.asks))
	}
}

// A task a stop holds asks the Owner nothing: on 11-06 three harvested,
// stopped tasks still had operations in awaiting_grant.
func TestGrantRequestHeldTaskAsksNothing(t *testing.T) {
	w := newGrantRequestWorld(t)
	w.held = true
	w.pass(t)
	if len(w.asks) != 0 {
		t.Fatalf("held task: asked %d", len(w.asks))
	}
	w.held = false
	w.c.Reconcile.askWaits = nil
	w.pass(t)
	if len(w.asks) != 1 {
		t.Fatalf("stop lifted: asked %d, want 1", len(w.asks))
	}
}

func TestGrantRequestNotWiredAsksNothing(t *testing.T) {
	w := newGrantRequestWorld(t)
	w.c.Reconcile.RequestGrant = nil
	grantRequestReads.state = func(context.Context, store.DB, string) (landing.State, error) {
		t.Fatal("read without RequestGrant")
		return landing.State{}, nil
	}
	w.pass(t)
	if len(w.asks) != 0 {
		t.Fatalf("asked %d", len(w.asks))
	}
}

func TestGrantRequestErrorAndRefusedMaterial(t *testing.T) {
	// A refused request is reported and read again only after a while.
	w := newGrantRequestWorld(t)
	now := time.Now()
	w.c.Now = func() time.Time { return now }
	w.requestFn = func(context.Context, string, map[string]any, string) (string, error) {
		w.asks = append(w.asks, grantAsk{})
		return "", errors.New("refused at creation")
	}
	if err := w.c.LandingRequestsPass(context.Background()); err == nil {
		t.Fatal("refusal not reported")
	}
	w.pass(t)
	if len(w.asks) != 1 {
		t.Fatalf("asked %d inside the wait", len(w.asks))
	}
	now = now.Add(grantAskWait)
	_ = w.c.LandingRequestsPass(context.Background())
	if len(w.asks) != 2 {
		t.Fatalf("asked %d after the wait", len(w.asks))
	}
	// A material with problems is not requested.
	w = newGrantRequestWorld(t)
	material := grantRequestReads.material
	grantRequestReads.material = func(ctx context.Context, db store.DB, src store.GrantSources, op string) (*store.LandingGrantMaterial, error) {
		m, err := material(ctx, db, src, op)
		m.Problems = []string{"ci_not_green"}
		return m, err
	}
	w.pass(t)
	if len(w.asks) != 0 {
		t.Fatalf("problems: asked %d", len(w.asks))
	}
}
