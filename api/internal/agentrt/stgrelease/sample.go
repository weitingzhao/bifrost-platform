package stgrelease

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// TD-460: the first opening of B5 needs a held sample that the platform
// created under a deterministic name (CANARY.md). These are the two actions
// behind it. They are not part of the B5 controller: they write their own
// stream (store.StgSampleStream), take the window as an ordinary holder, and
// reuse the production constructor, Create, observation, archive check and
// UID/RV-guarded finalizer removal of delivery.STGReleaseAdapter.
const (
	SampleCreateAction   = "stg_release_sample_create"
	SampleFinalizeAction = "stg_release_sample_finalize"
	// SampleTenant is the runtime tenant whose landing rule gates B5.
	SampleTenant = "dev"
)

var (
	// ErrSampleBusy: nothing was sent because another release holds the
	// window. The approval can run again later with the same allocation.
	ErrSampleBusy = errors.New("STG release window is held by another release")
	// ErrSampleUncertain: a Create may have been stored. Check by hand by the
	// run name; never create again under a new name.
	ErrSampleUncertain = errors.New("STG sample outcome unknown")
	sampleSHA          = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// A sample run is always try 1 of its allocation (SampleInput); the uid
	// is the apiserver's, a lowercase UUID.
	sampleRun = regexp.MustCompile(`^stgrel-[0-9]+-1-[0-9a-f]{8}$`)
	sampleUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// SampleRuns is the part of delivery.STGReleaseAdapter the actions use. The
// adapter's Archive must be the sample ledger, so RemoveArchivedFinalizer
// checks the sample archive rather than B5's.
type SampleRuns interface {
	HeldRuns(context.Context) ([]string, error)
	ReadRun(context.Context, string) (*unstructured.Unstructured, error)
	HoldSampleWindow(context.Context, string, string) (time.Time, error)
	EndSampleWindow(context.Context, string, string) (bool, error)
	CreatePermittedNamedDeliverRun(context.Context, delivery.NamedDeliverInput, func(context.Context) error) (cluster.ActuationResponse, *unstructured.Unstructured, error)
	ObserveRun(context.Context, delivery.STGRunIdentity) (delivery.STGRunObservation, error)
	RemoveArchivedFinalizer(context.Context, delivery.NamedDeliverInput, types.UID) error
}

// SampleLedger is the sample stream. PostgresSampleLedger is production.
type SampleLedger interface {
	Read(context.Context) (store.StgSampleState, error)
	RuleOpen(context.Context) (bool, error)
	Allocate(context.Context, store.StgSampleAllocation) (store.StgSample, error)
	// Sending is the send permit: it refuses unless B5 is still before its
	// first opening and the approval's allocation is open and uncreated, and
	// records the send that restarts the abandon grace.
	Sending(context.Context, string) (store.StgSample, error)
	Created(context.Context, string, string) (store.StgSample, error)
	Archive(context.Context, string, string, string, string, map[string]any) (store.StgSample, error)
	Release(context.Context, string, string, string) (store.StgSample, error)
	HasRunEvidence(context.Context, delivery.STGRunIdentity) (bool, error)
}

type PostgresSampleLedger struct {
	DB     store.DB
	Tenant string
}

func (l PostgresSampleLedger) Read(ctx context.Context) (store.StgSampleState, error) {
	return store.ReadStgSamples(ctx, l.DB)
}
func (l PostgresSampleLedger) RuleOpen(ctx context.Context) (bool, error) {
	return store.StgSampleOpenRule(ctx, l.DB, l.Tenant)
}
func (l PostgresSampleLedger) Allocate(ctx context.Context, req store.StgSampleAllocation) (store.StgSample, error) {
	req.Tenant = l.Tenant
	return store.AllocateStgSample(ctx, l.DB, req)
}
func (l PostgresSampleLedger) Sending(ctx context.Context, approval string) (store.StgSample, error) {
	return store.PermitStgSampleSend(ctx, l.DB, l.Tenant, approval)
}
func (l PostgresSampleLedger) Created(ctx context.Context, approval, uid string) (store.StgSample, error) {
	return store.RecordStgSampleCreated(ctx, l.DB, approval, uid)
}
func (l PostgresSampleLedger) Archive(ctx context.Context, approval, by, uid, status string, raw map[string]any) (store.StgSample, error) {
	return store.ArchiveStgSample(ctx, l.DB, approval, by, uid, status, raw)
}
func (l PostgresSampleLedger) Release(ctx context.Context, approval, by, uid string) (store.StgSample, error) {
	return store.ReleaseStgSample(ctx, l.DB, approval, by, uid)
}

// HasRunEvidence is the archive receipt RemoveArchivedFinalizer requires:
// a committed archived record of exactly this run and UID. Unlike B5's
// receipt it does not require the archived raw evidence to list child
// TaskRuns: a sample that failed before any TaskRun started is still
// archived and closed, and the probe, not this receipt, is what checks a
// sample's clone/build/pin-sync evidence (run_and_task_evidence).
func (l PostgresSampleLedger) HasRunEvidence(ctx context.Context, id delivery.STGRunIdentity) (bool, error) {
	s, err := l.Read(ctx)
	if err != nil {
		return false, err
	}
	return archivedIn(s, id), nil
}

func archivedIn(s store.StgSampleState, id delivery.STGRunIdentity) bool {
	for _, x := range s.Samples {
		in := SampleInput(*x)
		named, err := in.Identity()
		if err == nil && x.Archived && x.UID == string(id.UID) && named.Name == id.Name && id.Namespace == delivery.STGReleaseNamespace {
			return true
		}
	}
	return false
}

// SampleInput is the frozen B5 input of a sample: batch from the allocation
// event id, try 1, intent from the allocation sequence.
//
// Its run name stgrel-<n>-1-<P8> can share B5's name space only in theory.
// B5 numbers batches 1, 2, 3, ...; a sample's n is a global event id,
// allocated before B5 opened any batch (not_first refuses one after), so B5
// reaches the same n only after n batches, and then only with a P sharing
// the sample P's first 8 hex digits while the sample run still exists in
// cicd. Even then the run is not adopted: MatchNamedDeliverRun also compares
// the intent annotation (the sample's stream sequence, not B5's started
// sequence) and labels, so B5 records a collision instead.
func SampleInput(x store.StgSample) delivery.NamedDeliverInput {
	return delivery.NamedDeliverInput{Batch: x.Allocation, Try: 1, IntentSeq: x.IntentSeq, P: x.P, U: x.U, Infra: x.Infra}
}

type Sampler struct {
	Runs   SampleRuns
	Ledger SampleLedger
	// CI is the exact release CI reader B5 uses (landingci.Client.ReadRelease).
	CI  func(context.Context, string) (landingci.ReleaseEvidence, error)
	Now func() time.Time
}

func (s *Sampler) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

type SampleCreateRequest struct {
	Approval, Env, P, U, Infra string
}

type SampleResult struct {
	Run             string `json:"run"`
	UID             string `json:"uid,omitempty"`
	Who             string `json:"who"`
	Batch           int64  `json:"batch"`
	Try             int64  `json:"try"`
	IntentSeq       int64  `json:"intent_seq"`
	Revision        string `json:"revision"`
	UIRevision      string `json:"ui_revision"`
	InfraRevision   string `json:"infra_revision"`
	SampleApproval  string `json:"sample_approval"`
	Created         bool   `json:"created,omitempty"`
	WindowExpiresAt string `json:"window_expires_at,omitempty"`
	Status          string `json:"status,omitempty"`
	Archived        bool   `json:"archived,omitempty"`
	Released        bool   `json:"released,omitempty"`
	Abandoned       bool   `json:"abandoned,omitempty"`
	WindowEnded     bool   `json:"window_ended,omitempty"`
	WindowNote      string `json:"window_note,omitempty"`
}

func sampleResult(x store.StgSample, id delivery.NamedDeliverIdentity) SampleResult {
	return SampleResult{Run: id.Name, UID: x.UID, Who: id.Who, Batch: x.Allocation, Try: 1, IntentSeq: x.IntentSeq,
		Revision: x.P, UIRevision: x.U, InfraRevision: x.Infra, SampleApproval: x.Approval,
		Status: x.Status, Archived: x.Archived, Released: x.Released, Abandoned: x.Abandoned}
}

// CheckSampleCreate is the static part of a create request, run before the
// approval row is written and again at execution.
func CheckSampleCreate(env, p, u, infra string) error {
	if env != "stg" {
		return fmt.Errorf("%s targets env stg only", SampleCreateAction)
	}
	if !sampleSHA.MatchString(p) || !sampleSHA.MatchString(u) || !sampleSHA.MatchString(infra) {
		return fmt.Errorf("revision, ui_revision and infra_revision must be full 40-hex commits")
	}
	return nil
}

// CheckSampleFinalize is the static part of a finalize request.
func CheckSampleFinalize(env, run, uid string) error {
	if env != "stg" {
		return fmt.Errorf("%s targets env stg only", SampleFinalizeAction)
	}
	if run == "" || uid == "" {
		return fmt.Errorf("run and uid are required; uid comes from the create approval's result")
	}
	if !sampleRun.MatchString(run) {
		return fmt.Errorf("run must be a sample run name stgrel-<n>-1-<P8>")
	}
	if !sampleUID.MatchString(uid) {
		return fmt.Errorf("uid must be the run's UUID from the create approval's result")
	}
	return nil
}

// Create allocates (once per approval), holds the window as the sample's
// who, and creates the held run with the production constructor. A retry of
// the same approval returns the recorded sample or reads back the same name.
func (s *Sampler) Create(ctx context.Context, req SampleCreateRequest) (SampleResult, error) {
	if err := CheckSampleCreate(req.Env, req.P, req.U, req.Infra); err != nil {
		return SampleResult{}, err
	}
	if req.Approval == "" {
		return SampleResult{}, fmt.Errorf("%s runs only as an approval's execution", SampleCreateAction)
	}
	state, err := s.Ledger.Read(ctx)
	if err != nil {
		return SampleResult{}, err
	}
	mine := state.Samples[req.Approval]
	if mine != nil && mine.UID != "" {
		id, err := SampleInput(*mine).Identity()
		if err != nil {
			return SampleResult{}, err
		}
		return sampleResult(*mine, id), nil
	}
	// A plain early read, for a clear refusal before any other read. The
	// binding checks are Allocate's and the send permit's, both under the
	// shared rule lock and both run on a retry of a waiting approval too.
	open, err := s.Ledger.RuleOpen(ctx)
	if err != nil {
		return SampleResult{}, err
	}
	if open {
		return SampleResult{}, fmt.Errorf("landing:%s already enables stg_release for bifrost-platform; the sample is for the first opening only", SampleTenant)
	}
	if s.CI == nil {
		return SampleResult{}, fmt.Errorf("release CI reader unavailable")
	}
	ci, err := s.CI(ctx, req.P)
	if err != nil {
		return SampleResult{}, fmt.Errorf("exact release CI of %s: %w", req.P, err)
	}
	if _, err = ci.Record(); err != nil || ci.UICommit != req.U || ci.InfraCommit != req.Infra {
		return SampleResult{}, fmt.Errorf("revision has no accepted release CI with these ui_revision and infra_revision (status=%s ui=%s infra=%s)", ci.Status, ci.UICommit, ci.InfraCommit)
	}
	own := ""
	if mine != nil {
		id, err := SampleInput(*mine).Identity()
		if err != nil {
			return SampleResult{}, err
		}
		own = id.Name
	}
	held, err := s.Runs.HeldRuns(ctx)
	if err != nil {
		return SampleResult{}, err
	}
	for _, name := range held {
		if name != own {
			return SampleResult{}, fmt.Errorf("a held STG release run already exists: %s; finalize it first", name)
		}
	}
	var abandon *store.StgSampleAbsence
	if old := state.Open(); mine == nil && old != nil {
		if old.UID != "" {
			return SampleResult{}, fmt.Errorf("sample %s of approval %s is still open; finalize it first", old.UID, old.Approval)
		}
		id, err := SampleInput(*old).Identity()
		if err != nil {
			return SampleResult{}, err
		}
		// A 404 proves absence only from AbandonableFrom: 30 minutes after
		// the old allocation's last send record (the apiserver bound, not
		// the client deadline), or the short grace when it never sent. The
		// fold checks it again under the stream lock, against a send that
		// may be recorded after this read.
		now := s.now()
		if _, err = s.Runs.ReadRun(ctx, id.Name); !apierrors.IsNotFound(err) || now.Before(old.AbandonableFrom()) {
			return SampleResult{}, fmt.Errorf("approval %s allocated sample %s and its outcome is not proven absent; check that run by name", old.Approval, id.Name)
		}
		abandon = &store.StgSampleAbsence{Approval: old.Approval, Run: id.Name, ReadAt: now}
	}
	x, err := s.Ledger.Allocate(ctx, store.StgSampleAllocation{Approval: req.Approval, P: req.P, U: req.U, Infra: req.Infra, Abandon: abandon})
	if err != nil {
		return SampleResult{}, err
	}
	in := SampleInput(x)
	id, err := in.Identity()
	if err != nil {
		return SampleResult{}, err
	}
	out := sampleResult(x, id)
	if x.UID != "" {
		return out, nil
	}
	reason := "TD-460 STG sample of approval " + req.Approval
	expires, err := s.Runs.HoldSampleWindow(ctx, id.Who, reason)
	var busy *delivery.WindowHeld
	if errors.As(err, &busy) {
		return out, fmt.Errorf("%w: %v", ErrSampleBusy, err)
	}
	if err != nil {
		return out, s.endUnsent(ctx, id.Who, reason, err)
	}
	out.WindowExpiresAt = expires.UTC().Format(time.RFC3339)
	// posted turns true only when the permit passes, immediately before the
	// POST. Until then every error proves nothing was sent by this execution.
	posted := false
	permit := func(ctx context.Context) error {
		if _, err := s.Ledger.Sending(ctx, req.Approval); err != nil {
			return fmt.Errorf("sample not sent: %w", err)
		}
		posted = true
		return nil
	}
	resp, obj, err := s.Runs.CreatePermittedNamedDeliverRun(ctx, in, permit)
	if err != nil && !posted {
		// The permit refused (abandoned, rule_open, not_first, conflict) or
		// failed, or a read before it did (Pipeline, window, existing run).
		return out, s.endUnsent(ctx, id.Who, reason, err)
	}
	if err != nil {
		// The POST may have been sent: the window stays with this sample.
		return out, fmt.Errorf("%w: %s %v", ErrSampleUncertain, id.Name, err)
	}
	uid := string(obj.GetUID())
	x, err = s.Ledger.Created(ctx, req.Approval, uid)
	if err != nil {
		return out, fmt.Errorf("%w: run %s was created with uid %s but recording it failed: %v", ErrSampleUncertain, id.Name, uid, err)
	}
	out = sampleResult(x, id)
	out.Created, out.WindowExpiresAt = resp.Changed, expires.UTC().Format(time.RFC3339)
	return out, nil
}

// endUnsent ends the window this execution holds for an allocation it has
// not sent, so nothing waits for its TTL. It is called only on paths that
// prove no POST of this execution; a POST that may have been sent keeps the
// window until finalize. Only the same who with the same reason is ended, and
// a failure to end it is appended as a note without changing err's class.
func (s *Sampler) endUnsent(ctx context.Context, who, reason string, err error) error {
	if _, e := s.Runs.EndSampleWindow(ctx, who, reason); e != nil {
		return fmt.Errorf("%w (window not ended: %v)", err, e)
	}
	return err
}

type SampleFinalizeRequest struct {
	Approval, Env, Run, UID string
}

// Finalize archives the terminal run evidence first, then removes only the
// B5 hold with the UID/resourceVersion-guarded patch, then closes the sample
// and ends the sample's window if it still holds it. A retry resumes after
// the last durable step. The patch carries a test of the resourceVersion of
// the GET just before it: a write to the run in between (Tekton or anyone)
// fails that test (422) and this approval fails with the archive kept; the
// remedy is a new finalize approval, which reads the run again.
func (s *Sampler) Finalize(ctx context.Context, req SampleFinalizeRequest) (SampleResult, error) {
	if err := CheckSampleFinalize(req.Env, req.Run, req.UID); err != nil {
		return SampleResult{}, err
	}
	if req.Approval == "" {
		return SampleResult{}, fmt.Errorf("%s runs only as an approval's execution", SampleFinalizeAction)
	}
	state, err := s.Ledger.Read(ctx)
	if err != nil {
		return SampleResult{}, err
	}
	// Abandoned allocations are included. An abandon needs a GET 404 well
	// past the apiserver's request bound after the last send, and no send
	// follows it, so a run of one should never exist; if a POST is stored
	// later anyway, this is its way out: adopted below only when the live
	// run carries this allocation's exact identity and the hold. Run names
	// come from the allocation's event id, so at most one sample matches.
	var x *store.StgSample
	var id delivery.NamedDeliverIdentity
	for _, approval := range state.Order {
		candidate := state.Samples[approval]
		named, err := SampleInput(*candidate).Identity()
		if err == nil && named.Name == req.Run {
			x, id = candidate, named
		}
	}
	if x == nil {
		return SampleResult{}, fmt.Errorf("no sample named %s was created by %s", req.Run, SampleCreateAction)
	}
	if x.Released {
		if x.ReleasedBy == req.Approval && x.UID == req.UID {
			return sampleResult(*x, id), nil
		}
		return SampleResult{}, fmt.Errorf("sample %s was already finalized by approval %s", req.Run, x.ReleasedBy)
	}
	in := SampleInput(*x)
	if x.UID == "" {
		// The create approval ended unknown after a successful Create, or the
		// allocation was abandoned and its POST was stored later. Adopt the
		// UID only if the live run of this exact identity (name, intent
		// annotation, params, labels) has it; a late run of an abandoned
		// allocation must also still carry the hold.
		obj, err := s.Runs.ReadRun(ctx, req.Run)
		if err != nil {
			return SampleResult{}, err
		}
		if string(obj.GetUID()) != req.UID || delivery.MatchNamedDeliverRun(obj, in, x.Abandoned) != nil {
			return SampleResult{}, fmt.Errorf("run %s does not match the sample identity with uid %s", req.Run, req.UID)
		}
		if *x, err = s.Ledger.Created(ctx, x.Approval, req.UID); err != nil {
			return SampleResult{}, err
		}
	}
	if x.UID != req.UID {
		return SampleResult{}, fmt.Errorf("uid %s is not the recorded sample uid %s", req.UID, x.UID)
	}
	uid := types.UID(x.UID)
	if !x.Archived {
		obs, err := s.Runs.ObserveRun(ctx, delivery.STGRunIdentity{Namespace: delivery.STGReleaseNamespace, Name: id.Name, UID: uid})
		if err != nil {
			return SampleResult{}, err
		}
		if err = delivery.MatchNamedDeliverRun(obs.Run, in, false); err != nil {
			return SampleResult{}, err
		}
		if obs.Status != "Succeeded" && obs.Status != "Failed" {
			return SampleResult{}, fmt.Errorf("run %s is %s, not terminal; request finalize after it finishes", id.Name, orUnknown(obs.Status))
		}
		if *x, err = s.Ledger.Archive(ctx, x.Approval, req.Approval, x.UID, obs.Status, obs.Raw); err != nil {
			return SampleResult{}, err
		}
	}
	if err = s.Runs.RemoveArchivedFinalizer(ctx, in, uid); err != nil {
		return sampleResult(*x, id), fmt.Errorf("archived; finalizer not removed: %w", err)
	}
	if *x, err = s.Ledger.Release(ctx, x.Approval, req.Approval, x.UID); err != nil {
		return SampleResult{}, err
	}
	out := sampleResult(*x, id)
	out.WindowEnded, err = s.Runs.EndSampleWindow(ctx, id.Who, "")
	if err != nil {
		out.WindowNote = "window not ended: " + err.Error()
	}
	return out, nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
