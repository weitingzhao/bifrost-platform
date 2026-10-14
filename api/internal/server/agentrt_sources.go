package server

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// The delivery lane and repository whose deployments the runtime's standing
// conditions (gates_passed) read. Round 2's work is the platform itself
// (W-31); release records are per lane and per repository, and the rule atom
// names only the environment. Trade joins when business work starts.
const (
	agentrtDeployLane = "platform"
	agentrtDeployRepo = "bifrost-platform"
)

// agentrtWorkloads are the platform's own workloads, the container each
// runs and the image the delivery built for it, as the delivery's rollout
// check verifies them (bifrost-trade-infra task-rollout-verify-platform:
// the workers run the API image). They live in bifrost-platform-<env>.
var agentrtWorkloads = []struct{ deployment, container, image string }{
	{"platform-api", "platform-api", "platform-api"},
	{"platform-workers", "platform-api", "platform-api"},
	{"platform-console", "platform-console", "platform-console"},
}

// The readers agentRuntimeSources takes; a nil one reads nothing.
type (
	approvalReader interface {
		Lookup(ref string) (approvals.Approval, bool, error)
	}
	ciIndexer interface {
		CIIndex(ctx context.Context) (map[string]delivery.CIOutcome, error)
	}
	currentDeployment interface {
		Current(ctx context.Context, lane, env string) (releases.Record, error)
		PodDigests(ctx context.Context, namespace, deployment, container string) ([]string, error)
	}
)

// agentRuntimeSources reads the records the runtime does not own from the
// platform's own stores (design v5 §5.2 外部事实), each without side effects:
// approvals and questions by Lookup (no sweep), CI runs from Tekton, and the
// deployment from the release records. A record that cannot be read now is
// ok=false: the controller records nothing for it, a fact seen before stays
// seen, and gates_passed on the deployment is unknown.
func agentRuntimeSources(ap approvalReader, question func(context.Context, string) (store.Question, bool), ci ciIndexer, rel currentDeployment) controller.Sources {
	src := controller.Sources{
		Approval: func(context.Context, int64) (string, bool) { return "", false },
		Question: func(context.Context, string) (store.Question, bool) { return store.Question{}, false },
		CI:       func(context.Context, string, string) (string, string, bool) { return "", "", false },
		Deployed: func(context.Context, string) (string, map[string]any, bool) { return "", nil, false },
	}
	if ap != nil {
		reads := newBoundedReads(2)
		src.Approval = func(ctx context.Context, number int64) (string, bool) {
			a, ok, err := within(ctx, reads, func() (approvals.Approval, bool, error) { return ap.Lookup(strconv.FormatInt(number, 10)) })
			if err != nil || !ok || a.Status == "" {
				return "", false
			}
			return a.Status, true
		}
	}
	if question != nil {
		src.Question = question
	}
	if ci != nil {
		snap := &ciSnapshot{index: ci.CIIndex, ttl: 5 * time.Second}
		src.CI = func(ctx context.Context, repo, commit string) (string, string, bool) {
			o, err := snap.get(ctx, repo+"@"+commit)
			if err != nil || o.Status == "" {
				return "", "", false
			}
			return o.Status, o.Run, true
		}
	}
	if rel != nil {
		src.Deployed = func(ctx context.Context, env string) (string, map[string]any, bool) {
			rec, err := rel.Current(ctx, agentrtDeployLane, env)
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
	return src
}

// ciSnapshot lists the CI runs once and answers every read from that list
// for ttl (five seconds; a facts pass over many commits mostly lists once,
// review 161 B3). One caller lists at a time; the others wait for it, each
// for no longer than its own context (review 162 B1). A failed list is not
// kept: the next read lists again.
type ciSnapshot struct {
	index func(context.Context) (map[string]delivery.CIOutcome, error)
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	at      time.Time
	idx     map[string]delivery.CIOutcome
	listing chan struct{} // closed when the list in progress ends
}

func (s *ciSnapshot) get(ctx context.Context, key string) (delivery.CIOutcome, error) {
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	for {
		if err := ctx.Err(); err != nil {
			return delivery.CIOutcome{}, err
		}
		s.mu.Lock()
		if s.idx != nil && now().Sub(s.at) < s.ttl {
			o := s.idx[key]
			s.mu.Unlock()
			return o, nil
		}
		if wait := s.listing; wait != nil {
			s.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return delivery.CIOutcome{}, ctx.Err()
			}
		}
		done := make(chan struct{})
		s.listing = done
		s.mu.Unlock()
		idx, err := s.index(ctx)
		s.mu.Lock()
		if err == nil {
			s.idx, s.at = idx, now()
		}
		s.listing = nil
		close(done)
		s.mu.Unlock()
		if err != nil {
			return delivery.CIOutcome{}, err
		}
	}
}

// questionFact is a question as the runtime records it. An answer given as
// text only is recorded with the option "none", the question store's own
// word for it ("none" needs text), the text kept. An answer without who or
// when is not read: question_seen refuses a partial answer (review 151 B2),
// so it stays unseen rather than refused on every pass.
func questionFact(id, status string, answered bool, option, text, by string, at time.Time) (store.Question, bool) {
	f := store.Question{ID: id, Status: status}
	if !answered {
		return f, true
	}
	if option == "" && text != "" {
		option = "none"
	}
	if option == "" || by == "" || at.IsZero() {
		return store.Question{}, false
	}
	f.Option, f.Text, f.AnsweredBy, f.AnsweredAt = option, text, by, at.UTC().Format(time.RFC3339)
	return f, true
}

// deployedFrom is what the record says the environment runs: its
// repository's commit, and as the reference the delivery run and the image
// digests it built (TD-343), so a gate check records exactly what ran and
// gates_passed compares it whole. A record without the digests of every
// workload's image (one older than the digests, or a pipeline that reports
// none) is not a known deployment.
func deployedFrom(rec releases.Record) (string, map[string]any, bool) {
	b, ok := rec.Repos[agentrtDeployRepo]
	if !ok || len(b.SHA) != 40 || rec.Run == "" {
		return "", nil, false
	}
	images := map[string]any{}
	for _, w := range agentrtWorkloads {
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
// known (TD-343; the Owner chose this on 2026-10-14).
func runsRecorded(ctx context.Context, rel currentDeployment, env string, rec releases.Record) bool {
	for _, w := range agentrtWorkloads {
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

// errBusy: as many reads of this store as allowed are still running.
var errBusy = errors.New("agentrt source: earlier reads have not returned")

// boundedReads caps the reads of one store in flight: a read that never
// returns keeps its slot, so a stuck store holds a bounded number of
// goroutines, and further reads answer unknown at once (review 161 B1).
type boundedReads struct{ slots chan struct{} }

func newBoundedReads(n int) *boundedReads { return &boundedReads{slots: make(chan struct{}, n)} }

// within runs a read that does not take a context (the approval and question
// stores read their files themselves) and gives up when ctx ends; the read's
// slot is freed only when the read returns.
func within[T any](ctx context.Context, reads *boundedReads, read func() (T, bool, error)) (T, bool, error) {
	var zero T
	select {
	case reads.slots <- struct{}{}:
	default:
		return zero, false, errBusy
	}
	type result struct {
		v   T
		ok  bool
		err error
	}
	out := make(chan result, 1)
	safego.Go("agentrt-source-read", func() {
		defer func() { <-reads.slots }()
		v, ok, err := read()
		out <- result{v, ok, err}
	})
	select {
	case r := <-out:
		return r.v, r.ok, r.err
	case <-ctx.Done():
		return zero, false, ctx.Err()
	}
}
