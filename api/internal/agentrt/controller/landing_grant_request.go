package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The platform requests the Owner's landing card itself (R5:5.7 goal 3):
// once an operation waits in awaiting_grant, with no usable grant and no
// request of its own, the controller reads its material as the material
// route does and asks for the action and parameters that route gives the
// coordinator, stg_release true as that route's default: the Owner's one
// approval covers main and STG, so B5 can send the landed commit to STG
// (R5:5.7). The approval service's create check and rules apply unchanged.
// The approvals are the record: a pass that sees one open for the
// operation, or one answered for its R, asks nothing.

// grantAskWait is how long an operation whose card could not be requested
// (its material refuses one, or a read failed) waits before it is read again.
const grantAskWait = 2 * time.Minute

// grantAskSTGRelease is the stg_release the controller asks with. The
// material is read with it too: it is part of the material digest.
const grantAskSTGRelease = true

// grantWaiting is an operation in awaiting_grant, the latest of its task.
type grantWaiting struct {
	Op, Task, R string
}

// grantRequestReads are the database and material reads of the pass; a
// variable so the tests can see what the pass asks without a landing world.
var grantRequestReads = struct {
	state    func(ctx context.Context, db store.DB, repo string) (landing.State, error)
	usable   func(ctx context.Context, db store.DB, op string) (string, error)
	held     func(ctx context.Context, db store.DB, task string) (bool, error)
	material func(ctx context.Context, db store.DB, src store.GrantSources, op string, stg bool) (*store.LandingGrantMaterial, error)
	main     func(ctx context.Context, src store.GrantSources, repo string) (string, error)
}{
	state: func(ctx context.Context, db store.DB, repo string) (s landing.State, err error) {
		err = store.InTx(ctx, db, func(tx pgx.Tx) error {
			s, err = store.LandingState(ctx, tx, "landing:"+repo)
			return err
		})
		return s, err
	},
	usable:   store.UsableLandingGrant,
	held:     store.TaskStopHeld,
	material: store.ReadLandingGrantMaterial,
	main: func(ctx context.Context, src store.GrantSources, repo string) (string, error) {
		return src.MainOf(ctx, repo)
	},
}

// waitingForGrant lists a live stream's operations in awaiting_grant, each
// the latest of its task.
func waitingForGrant(s landing.State) []grantWaiting {
	if s.LiveAt.IsZero() {
		return nil
	}
	var out []grantWaiting
	for id, o := range s.Operations {
		if o.Phase == "awaiting_grant" && o.Task != "" && s.LatestOperation(o.Task) == id {
			out = append(out, grantWaiting{Op: id, Task: o.Task, R: o.R})
		}
	}
	slices.SortFunc(out, func(a, b grantWaiting) int { return strings.Compare(a.Op, b.Op) })
	return out
}

// grantAsked: the approvals already stand for the operation. One pending,
// approved or executing (or unknown) is its open request; any other for the
// same R is the Owner's answer or its outcome, which the answer path and
// the grant itself take on from there.
func grantAsked(requests []store.LandingGrantRequest, w grantWaiting) bool {
	for _, r := range requests {
		if r.Op != w.Op {
			continue
		}
		switch r.Status {
		case "pending", "approved", "running", "unknown":
			return true
		}
		if r.R == w.R {
			return true
		}
	}
	return false
}

// requestLandingGrants runs at the end of LandingRequestsPass, on the
// approvals that pass read. One operation's error does not stop the others.
func (c *Controller) requestLandingGrants(ctx context.Context, requests []store.LandingGrantRequest) error {
	r := c.Reconcile
	if r.RequestGrant == nil || r.Grants == nil {
		return nil
	}
	repos := r.Repos
	if len(repos) == 0 {
		repos = []string{"bifrost-platform"}
	}
	var errs []error
	for _, repo := range repos {
		if !rules.GrantSupported(repo) {
			continue
		}
		s, err := grantRequestReads.state(ctx, c.DB, repo)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, w := range waitingForGrant(s) {
			if err := c.requestLandingGrant(ctx, requests, w); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", w.Op, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (c *Controller) requestLandingGrant(ctx context.Context, requests []store.LandingGrantRequest, w grantWaiting) error {
	r := c.Reconcile
	if grantAsked(requests, w) {
		return nil
	}
	key := w.Op + "|" + w.R
	r.mu.Lock()
	until := r.askWaits[key]
	r.mu.Unlock()
	if c.clock().Before(until) {
		return nil
	}
	err := c.askLandingGrant(ctx, w)
	r.mu.Lock()
	if r.askWaits == nil {
		r.askWaits = map[string]time.Time{}
	}
	for k, at := range r.askWaits {
		if !c.clock().Before(at) {
			delete(r.askWaits, k)
		}
	}
	if !errors.Is(err, errGrantAsked) {
		r.askWaits[key] = c.clock().Add(grantAskWait)
	}
	r.mu.Unlock()
	if errors.Is(err, errGrantAsked) || errors.Is(err, errGrantNotNow) {
		return nil
	}
	return err
}

var (
	errGrantAsked  = errors.New("grant requested")
	errGrantNotNow = errors.New("no grant request now")
)

func (c *Controller) askLandingGrant(ctx context.Context, w grantWaiting) error {
	r := c.Reconcile
	if held, err := grantRequestReads.held(ctx, c.DB, w.Task); err != nil {
		return err
	} else if held {
		return errGrantNotNow // a stop holds the task: nothing is asked until the Owner lifts it
	}
	if grant, err := grantRequestReads.usable(ctx, c.DB, w.Op); err != nil {
		return err
	} else if grant != "" {
		return errGrantNotNow // the final gate takes it from here
	}
	src := r.Grants()
	m, err := grantRequestReads.material(ctx, c.DB, src, w.Op, grantAskSTGRelease)
	if err != nil {
		return err
	}
	if len(m.Problems) > 0 || m.Candidate.R != w.R {
		return errGrantNotNow // the create check would refuse it: read again later
	}
	// main_is_M0 false: a grant the final gate could only throw away; the
	// operation replays (replayAwaitingGrant). Unreadable main counts so too.
	if head, err := grantRequestReads.main(ctx, src, m.Candidate.Repo); err != nil || head == "" || head != m.Candidate.M0 {
		return errGrantNotNow
	}
	// A batch's lead asks for the whole batch: its material's action is the
	// one every member's soft checks together take.
	action := store.ActionGuardrailLanding
	if len(m.Candidate.HardCorePaths) > 0 || m.Action == store.ActionCoreLanding {
		action = store.ActionCoreLanding
	}
	params := map[string]any{"env": "prod", "repo": m.Candidate.Repo, "task": m.Candidate.Task, "op": w.Op,
		"R": m.Candidate.R, "material_digest": m.MaterialDigest, "stg_release": grantAskSTGRelease}
	why := fmt.Sprintf("its change fails soft checks %s", strings.Join(m.SoftAtRequest, ", "))
	if action == store.ActionCoreLanding {
		why = "its change touches trusted core paths"
	}
	if b := m.Batch; b != nil && len(b.Members) > 1 && b.Members[0].Op == w.Op {
		why += fmt.Sprintf("; it leads batch %s of %d, and this one grant lands every member", b.Batch, len(b.Members))
	}
	reason := fmt.Sprintf("Task %s, landing operation %s, waits on the Owner's grant (awaiting_grant): %s.", m.Candidate.Task, w.Op, why)
	if _, err := r.RequestGrant(ctx, action, params, reason); err != nil {
		return err
	}
	return errGrantAsked
}
