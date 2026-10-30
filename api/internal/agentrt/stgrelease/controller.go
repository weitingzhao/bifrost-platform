package stgrelease

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/clock"
)

// Ledger commits all records of a decision atomically against the read stream
// sequence. Production uses PostgresLedger; a fake must use the same Parse /
// Fold validation, including rejecting a stale expected sequence.
type Ledger interface {
	Read(context.Context) (store.StgState, error)
	Commit(context.Context, int64, []store.StgRecord) (bool, error)
}

type PostgresLedger struct {
	DB     store.DB
	Tenant string
}

func (l PostgresLedger) Read(ctx context.Context) (store.StgState, error) {
	return store.ReadStgRelease(ctx, l.DB)
}
func (l PostgresLedger) Commit(ctx context.Context, seq int64, records []store.StgRecord) (bool, error) {
	return store.CommitStgControl(ctx, l.DB, l.Tenant, func() bool { return true }, seq, records)
}
func (l PostgresLedger) HasRunEvidence(ctx context.Context, id delivery.STGRunIdentity) (bool, error) {
	s, err := l.Read(ctx)
	if err != nil {
		return false, err
	}
	for _, b := range s.Batches {
		for _, t := range b.Tries {
			if t.RunEvidence.Seq > 0 && t.RunEvidence.Record.String("uid") == string(id.UID) && t.Started.Record.String("run") == id.Name && object(t.Snapshot.Record, "inputs").String("namespace") == id.Namespace {
				return true, nil
			}
		}
	}
	return false, nil
}

// Execution is the existing delivery adapter, injected so the controller
// neither constructs a Kubernetes client nor knows a cluster credential.
type Execution interface {
	Runs
	ObserveRun(context.Context, delivery.STGRunIdentity) (delivery.STGRunObservation, error)
	RemoveArchivedFinalizer(context.Context, delivery.NamedDeliverInput, types.UID) error
	ObserveWorkloads(context.Context) (map[string]any, error)
	ReadPin(context.Context, string, string) (map[string]any, error)
	ReadHealth(context.Context) delivery.STGHealth
}

type Sources struct {
	Head         func(context.Context) (string, error)
	CI           func(context.Context, string) (store.StgCIObservation, error)
	Range        func(context.Context, string, string) ([]landing.ReleaseCommit, error)
	MainEvidence store.MainEvidenceFunc
}

// Controller is a single repository reconciler. Everything needed after a
// crash is in Ledger; holder/cleanup cache only avoid unnecessary I/O. Pass
// is serialized per instance, while the ledger CAS and B5-c gate arbitrate
// different processes. Close stops and joins the current window holder.
type Controller struct {
	DB              store.DB
	Tenant, Cluster string
	Ledger          Ledger
	Runs            Execution
	Windows         Windows
	Sources         Sources
	// Policy is I/O-free and concurrency-safe, also used by the renewer.
	Policy func() WindowPolicy
	Clock  clock.WithTicker
	// Lifetime must outlive a bounded Pass. Cancellation also stops renewal.
	Lifetime            context.Context
	FinalizerGuaranteed bool
	mu                  sync.Mutex
	holder              *WindowHolder
	losses              []WindowLoss
	cleaned             map[string]bool
}

func (c *Controller) now() time.Time {
	if c.Clock != nil {
		return c.Clock.Now().UTC()
	}
	return store.DecisionAt()
}
func (c *Controller) configured() bool {
	if c.Policy == nil {
		return false
	}
	p := c.Policy()
	if p.Environment != "prod" || p.Rule == nil || p.Rule.Landing == nil || c.now().Before(p.Rule.Effective) {
		return false
	}
	repo, ok := p.Rule.Landing.Repo("bifrost-platform")
	return ok && repo.STGRelease == delivery.STGReleasePipeline
}
func (c *Controller) ledger() Ledger {
	if c.Ledger != nil {
		return c.Ledger
	}
	return PostgresLedger{DB: c.DB, Tenant: c.Tenant}
}
func (c *Controller) write(ctx context.Context, s store.StgState, rs ...store.StgRecord) error {
	if len(rs) == 0 || !c.configured() {
		return nil
	}
	_, err := c.ledger().Commit(ctx, s.LastSeq, rs)
	return err
}
func (c *Controller) Close() error { c.mu.Lock(); defer c.mu.Unlock(); return c.release() }
func (c *Controller) release() error {
	if c.holder == nil {
		return nil
	}
	h := c.holder
	c.holder = nil
	err := h.Release()
	for loss := range h.Losses() {
		c.losses = append(c.losses, loss)
	}
	return err
}
func (c *Controller) hold(ctx context.Context, s store.StgState, id string, n int64) error {
	if c.holder != nil {
		if c.holder.batch == id && c.holder.try == n && c.holder.Generation() > 0 {
			return nil
		}
		if err := c.release(); err != nil {
			return err
		}
	}
	lifetime := c.Lifetime
	if lifetime == nil {
		lifetime = context.WithoutCancel(ctx)
	}
	h, err := HoldWindow(lifetime, WindowConfig{Windows: c.Windows, Policy: c.Policy, Clock: c.Clock}, s, id, n)
	if err == nil {
		c.holder = h
	}
	return err
}

// Pass returns immediately for none, before ledger, Git, CI or window I/O.
// With a non-none rule, live off still observes previously started runs.
func (c *Controller) Pass(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.configured() {
		return c.release()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	s, err := c.ledger().Read(ctx)
	if err != nil {
		return err
	}
	// Archive cleanup is retried even for closed batches and older tries.
	// Failure does not hold verification or change its result.
	cleanupErr := c.cleanup(ctx, s)
	id, b := openBatch(s)
	if c.holder != nil && (b == nil || c.holder.batch != id || c.holder.try != max(b.Try, 1) || !c.Policy().Live) {
		cleanupErr = errors.Join(cleanupErr, c.release())
	}
	if c.holder != nil {
		select {
		case loss, ok := <-c.holder.Losses():
			if ok {
				c.losses = append(c.losses, loss)
			}
		default:
		}
	}
	if len(c.losses) > 0 {
		loss := c.losses[0]
		if b == nil || loss.Batch != id || loss.Try != max(b.Try, 1) {
			c.losses = c.losses[1:]
		} else {
			r := diagnostic(b, id, "lease_lost", loss.Generation)
			if r != nil {
				if err := c.write(ctx, s, r); err != nil {
					return errors.Join(cleanupErr, err)
				}
				return cleanupErr
			}
			c.losses = c.losses[1:]
		}
	}
	if b != nil {
		return errors.Join(cleanupErr, c.batch(ctx, s, id, b))
	}
	if rs := unblockRecorded(s); len(rs) > 0 {
		return errors.Join(cleanupErr, c.write(ctx, s, rs...))
	}
	for _, block := range s.Blocks {
		if block.IsOpen() {
			return errors.Join(cleanupErr, c.readBlock(ctx, s, block))
		}
	}
	if c.DB != nil && c.Policy().Live {
		_, err = store.OpenStgBatch(ctx, c.DB, c.Tenant, func() bool { return c.Policy().Live })
	}
	return errors.Join(cleanupErr, err)
}
func openBatch(s store.StgState) (string, *store.StgBatch) {
	for id, b := range s.Batches {
		if b.Phase != "CLOSED" {
			return id, b
		}
	}
	return "", nil
}
func (c *Controller) cleanup(ctx context.Context, s store.StgState) error {
	if c.Runs == nil {
		return nil
	}
	if c.cleaned == nil {
		c.cleaned = map[string]bool{}
	}
	var errs []error
	var alerts []store.StgRecord
	for id, b := range s.Batches {
		for n, t := range b.Tries {
			uid := t.RunEvidence.Record.String("uid")
			if t.RunEvidence.Seq == 0 || c.cleaned[uid] {
				continue
			}
			err := c.Runs.RemoveArchivedFinalizer(ctx, input(id, n, t.Started.Seq, object(t.Snapshot.Record, "inputs")), types.UID(uid))
			if err != nil {
				errs = append(errs, fmt.Errorf("STG finalizer %s: %w", uid, err))
				key := fmt.Sprintf("finalizer_stuck/%d/1", n)
				if c.now().Sub(t.Bound.At) >= 30*24*time.Hour && b.Diagnostics[key].Seq == 0 {
					alerts = append(alerts, batchRecord("diag", id, n, "what", "finalizer_stuck", "gen", 1))
				}
			} else {
				c.cleaned[uid] = true
			}
		}
	}
	if len(alerts) > 0 {
		errs = append(errs, c.write(ctx, s, alerts...))
	}
	return errors.Join(errs...)
}
func batchRecord(kind, id string, n int64, kv ...any) store.StgRecord {
	r := store.StgRecord{"v": "1", "scope": "batch", "kind": kind, "batch": id}
	if n > 0 {
		r["try"] = n
	}
	for i := 0; i < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	return r
}
func diagnostic(b *store.StgBatch, id, what string, gen int64) store.StgRecord {
	key := fmt.Sprintf("%s/%d/%d", what, b.Try, gen)
	if b.Diagnostics[key].Seq > 0 {
		return nil
	}
	r := batchRecord("diag", id, b.Try, "what", what, "gen", gen)
	r["try"] = b.Try
	return r
}
