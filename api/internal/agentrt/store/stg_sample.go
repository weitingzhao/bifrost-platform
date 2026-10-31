package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// StgSampleStream records the one finalizer sample that B5's first opening
// needs (TD-460, CANARY.md). It is its own stream: B5's repository stream
// never sees these records, so a sample never opens a batch, debits a budget
// or moves a member. Only the two Tier C sample actions write it.
const StgSampleStream = "stg-sample:bifrost-platform"

// StgSampleAbandonGrace applies to an allocation that never reached a send
// record: a GET 404 of its run this long after the allocation proves its
// Create never landed. Every POST is preceded by a committed "sending" record
// (PermitStgSampleSend), so an allocation without one has sent nothing, and
// an abandon committed first makes any later send permit, and so its POST,
// refused. The grace only absorbs clock skew between platform-api Pods.
const StgSampleAbandonGrace = 2 * time.Minute

// StgSampleAbandonAfterSend applies once an allocation has a send record: a
// GET 404 of its run counts as proof of absence only this long after its
// last send record. The Create's 10 second client deadline proves nothing
// about the server: a timed-out or lost POST may still be stored after the
// client gave up, which is why the shared delivery code
// (delivery.createNamedDeliverRun) keeps such a send occupied instead of
// trusting a 404. What bounds it is the apiserver: kube-apiserver ends every
// non-long-running request after --request-timeout (60 seconds by default),
// so a POST still uncommitted that long after it was sent is never stored.
// 30 minutes is far above that bound and any clock skew between
// platform-api Pods. Preconditions: the apiserver's --request-timeout stays
// far below 30 minutes and Pod clocks differ by far less. If either fails
// and a late run appears for an abandoned allocation, Finalize can still
// adopt, archive and release it (stgrelease.Sampler.Finalize).
const StgSampleAbandonAfterSend = 30 * time.Minute

// StgSampleError is a refusal; nothing was written.
type StgSampleError struct{ Code, Reason string }

func (e *StgSampleError) Error() string { return fmt.Sprintf("stg_sample %s: %s", e.Code, e.Reason) }

func sampleError(code, reason string) error { return &StgSampleError{Code: code, Reason: reason} }

// StgSample is one sample. Its batch number is the global event id of its
// allocation and its intent the allocation's sequence on StgSampleStream:
// both come from the durable record, never from a guess or the B5 ledger.
type StgSample struct {
	Approval     string
	Allocation   int64
	IntentSeq    int64
	AllocatedAt  time.Time
	P, U, Infra  string
	UID          string
	Status       string
	Archived     bool
	ArchivedBy   string
	Released     bool
	ReleasedBy   string
	Abandoned    bool
	AbandonedBy  string
	SentAt       time.Time // last "sending" record; zero before the first send
	SendingSeq   int64
	CreatedSeq   int64
	ArchivedSeq  int64
	ReleasedSeq  int64
	AbandonedSeq int64
}

// Open is a sample that still holds the stream: allocated, not released and
// not abandoned. At most one exists at a time.
func (s *StgSample) Open() bool { return s != nil && !s.Released && !s.Abandoned }

// AbandonableFrom is the earliest read time of a GET 404 that proves an
// uncreated allocation's run absent: StgSampleAbandonAfterSend after its
// last send record, or StgSampleAbandonGrace after the allocation when it
// never reached a send.
func (s *StgSample) AbandonableFrom() time.Time {
	if s.SendingSeq > 0 {
		return s.SentAt.Add(StgSampleAbandonAfterSend)
	}
	return s.AllocatedAt.Add(StgSampleAbandonGrace)
}

// StgSampleState is the replayed stream.
type StgSampleState struct {
	Samples map[string]*StgSample // by the allocating approval
	Order   []string
	LastSeq int64
}

// Open returns the open sample, or nil.
func (s StgSampleState) Open() *StgSample {
	for _, id := range s.Order {
		if s.Samples[id].Open() {
			return s.Samples[id]
		}
	}
	return nil
}

// StgSampleEntry is one stored event of StgSampleStream.
type StgSampleEntry struct {
	EventID, Seq int64
	At           time.Time
	Type, Actor  string
	Body         map[string]any
}

var stgSampleFields = map[string]string{
	"allocated": "approval P U infra",
	"sending":   "approval",
	"created":   "approval uid",
	"abandoned": "approval by run read_at",
	"archived":  "approval by uid status raw",
	"released":  "approval by uid",
}

func stgSampleRecord(body map[string]any) (StgRecord, error) {
	for key := range body {
		if key != "text" && key != "stg_sample" {
			return nil, sampleError("invalid", "one stg_sample record per note")
		}
	}
	raw, ok := body["stg_sample"].(map[string]any)
	if !ok {
		return nil, sampleError("invalid", "stg_sample must be an object")
	}
	r := StgRecord(raw)
	fields, known := stgSampleFields[r.String("kind")]
	if !known || r.Int("v") != 1 {
		return nil, sampleError("invalid", "unknown stg_sample kind or version")
	}
	if !stgKeys(raw, "v kind "+fields) {
		return nil, sampleError("invalid", r.String("kind")+" fields are "+fields)
	}
	for _, key := range []string{"approval", "by", "uid", "run"} {
		if _, has := raw[key]; has && !stgIDRE.MatchString(r.String(key)) {
			return nil, sampleError("invalid", key+" is not an identifier")
		}
	}
	for _, key := range []string{"P", "U", "infra"} {
		if _, has := raw[key]; has && !landingSHA.MatchString(r.String(key)) {
			return nil, sampleError("invalid", key+" must be a full commit")
		}
	}
	if _, has := raw["status"]; has && r.String("status") != "Succeeded" && r.String("status") != "Failed" {
		return nil, sampleError("invalid", "archived status must be terminal")
	}
	if _, has := raw["raw"]; has {
		if _, ok := raw["raw"].(map[string]any); !ok {
			return nil, sampleError("invalid", "raw must be an object")
		}
	}
	if _, has := raw["read_at"]; has && r.Time("read_at").IsZero() {
		return nil, sampleError("invalid", "read_at must be RFC 3339")
	}
	return r, nil
}

// StgSampleFold replays the stream. It does no I/O; a malformed or
// out-of-order record fails the read instead of showing an empty stream.
func StgSampleFold(entries []StgSampleEntry) (StgSampleState, error) {
	s := StgSampleState{Samples: map[string]*StgSample{}}
	for _, e := range entries {
		if err := s.apply(e); err != nil {
			return StgSampleState{}, err
		}
	}
	return s, nil
}

func (s *StgSampleState) apply(e StgSampleEntry) error {
	if e.Seq <= s.LastSeq || e.At.IsZero() || e.Type != "note" || e.Actor != "controller" {
		return sampleError("invalid", "controller notes in increasing sequence")
	}
	r, err := stgSampleRecord(e.Body)
	if err != nil {
		return err
	}
	k, approval := r.String("kind"), r.String("approval")
	x := s.Samples[approval]
	if k == "allocated" {
		if x != nil {
			return sampleError("conflict", "approval already allocated a sample")
		}
		if open := s.Open(); open != nil {
			return sampleError("exists", "sample of approval "+open.Approval+" is still open")
		}
		if e.EventID < 1 {
			return sampleError("invalid", "allocation needs its event id")
		}
		s.Samples[approval] = &StgSample{Approval: approval, Allocation: e.EventID, IntentSeq: e.Seq, AllocatedAt: e.At,
			P: r.String("P"), U: r.String("U"), Infra: r.String("infra")}
		s.Order = append(s.Order, approval)
		s.LastSeq = e.Seq
		return nil
	}
	// sending and abandoned need the open allocation. created, archived and
	// released also follow an abandon: a run that was stored after its
	// allocation was abandoned is adopted, archived and released by Finalize.
	if k == "sending" || k == "abandoned" {
		if !x.Open() {
			return sampleError("missing", "no open sample for approval "+approval)
		}
	} else if x == nil || x.Released {
		return sampleError("missing", "no unreleased sample for approval "+approval)
	}
	switch k {
	case "sending":
		if x.UID != "" {
			return sampleError("conflict", "sample already has a UID; nothing more is sent")
		}
		x.SentAt, x.SendingSeq = e.At, e.Seq
	case "created":
		if x.UID != "" {
			return sampleError("conflict", "sample already has a UID")
		}
		x.UID, x.CreatedSeq = r.String("uid"), e.Seq
	case "abandoned":
		read := r.Time("read_at")
		if x.UID != "" || r.String("by") == approval || read.Before(x.AbandonableFrom()) || read.After(e.At) {
			return sampleError("conflict", "only an uncreated allocation is abandoned, by another approval, after the grace (30 minutes from its last send)")
		}
		x.Abandoned, x.AbandonedBy, x.AbandonedSeq = true, r.String("by"), e.Seq
	case "archived":
		if x.UID == "" || r.String("uid") != x.UID || x.Archived {
			return sampleError("conflict", "archive needs the recorded UID, once")
		}
		x.Archived, x.ArchivedBy, x.Status, x.ArchivedSeq = true, r.String("by"), r.String("status"), e.Seq
	case "released":
		if !x.Archived || r.String("uid") != x.UID {
			return sampleError("conflict", "release follows the archive of the recorded UID")
		}
		x.Released, x.ReleasedBy, x.ReleasedSeq = true, r.String("by"), e.Seq
	}
	s.LastSeq = e.Seq
	return nil
}

// checkStgSampleAppend keeps the stream closed: only its own records go on
// it, and each one must apply to the replayed state under the stream lock
// Append already holds.
func checkStgSampleAppend(ctx context.Context, tx pgx.Tx, ev Event, seq int64, at time.Time) error {
	_, has := ev.Body["stg_sample"]
	if !has && ev.Stream != StgSampleStream {
		return nil
	}
	if ev.Stream != StgSampleStream || ev.Type != "note" || ev.Actor != "controller" || ev.TaskID != "" || ev.MissionID != "" || ev.AttemptID != "" || ev.PlanNode != "" {
		return sampleError("permission", "stg_sample records are controller notes on "+StgSampleStream+" without row bindings")
	}
	s, err := stgSampleState(ctx, tx)
	if err != nil {
		return err
	}
	body, err := detachedBody(ev.Body)
	if err != nil {
		return err
	}
	// The event id is assigned by the insert; any positive placeholder lets
	// the fold check the transition, and readers replay the stored id.
	return s.apply(StgSampleEntry{EventID: 1, Seq: seq, At: at, Type: ev.Type, Actor: ev.Actor, Body: body})
}

func detachedBody(body map[string]any) (map[string]any, error) {
	b, err := canon.Encode(body)
	if err != nil {
		return nil, err
	}
	v, err := canon.Parse(b)
	if err != nil {
		return nil, err
	}
	m, _ := v.(map[string]any)
	return m, nil
}

func stgSampleState(ctx context.Context, tx pgx.Tx) (StgSampleState, error) {
	rows, err := tx.Query(ctx, `SELECT event_id, seq, at, event_type, actor_id, body_json FROM agentrt.event WHERE event_stream_id=$1 ORDER BY seq`, StgSampleStream)
	if err != nil {
		return StgSampleState{}, err
	}
	defer rows.Close()
	var entries []StgSampleEntry
	for rows.Next() {
		var e StgSampleEntry
		var raw []byte
		if err := rows.Scan(&e.EventID, &e.Seq, &e.At, &e.Type, &e.Actor, &raw); err != nil {
			return StgSampleState{}, err
		}
		v, err := canon.Parse(raw)
		if err != nil {
			return StgSampleState{}, err
		}
		e.Body, _ = v.(map[string]any)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return StgSampleState{}, err
	}
	return StgSampleFold(entries)
}

// ReadStgSamples is a detached replay of the sample stream.
func ReadStgSamples(ctx context.Context, db DB) (StgSampleState, error) {
	var s StgSampleState
	err := InTx(ctx, db, func(tx pgx.Tx) error { var err error; s, err = stgSampleState(ctx, tx); return err })
	return s, err
}

// StgSampleOpenRule reports whether the signed rule in force enables B5 for
// bifrost-platform. The sample exists only for the first opening, so an
// enabled rule refuses every new sample and every Create.
func StgSampleOpenRule(ctx context.Context, db DB, tenant string) (bool, error) {
	var open bool
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		open, err = stgControlEnabled(ctx, tx, tenant, func() bool { return true })
		return err
	})
	return open, err
}

// StgSampleAllocation asks for the one sample of an approval. Abandon, when
// set, is the caller's GET 404 of an older, uncreated allocation's run.
type StgSampleAllocation struct {
	Tenant, Approval, P, U, Infra string
	Abandon                       *StgSampleAbsence
}

// StgSampleAbsence proves an older allocation never produced a run.
type StgSampleAbsence struct {
	Approval, Run string
	ReadAt        time.Time
}

// stgSampleGate reads whether B5 is still before its first opening: the
// rule in force is none and the B5 ledger has never opened a batch. The rule
// is read under the shared rule lock, so for the rest of the transaction it
// cannot open and B5 (which opens batches only under an enabled rule) cannot
// open one. refusal is a StgSampleError; err is a failed read.
func stgSampleGate(ctx context.Context, tx pgx.Tx, tenant string) (refusal, err error) {
	open, err := stgControlEnabled(ctx, tx, tenant, func() bool { return true })
	if err != nil {
		return nil, err
	}
	if open {
		return sampleError("rule_open", "landing:"+tenant+" enables stg_release for bifrost-platform; the sample is for the first opening only"), nil
	}
	b5, err := StgReleaseState(ctx, tx)
	if err != nil {
		return nil, err
	}
	if b5.LastBatch > 0 {
		return sampleError("not_first", "B5 has already opened a batch; its own held runs are the samples now"), nil
	}
	return nil, nil
}

// AllocateStgSample is idempotent for one approval. A created sample is
// returned as recorded, whatever has happened since. An allocation that has
// not been created yet, and a new one, both need stgSampleGate to pass, so a
// retry of a waiting approval cannot outlive the first opening; a new one
// also needs no other open sample.
func AllocateStgSample(ctx context.Context, db DB, req StgSampleAllocation) (StgSample, error) {
	var out StgSample
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		refusal, err := stgSampleGate(ctx, tx, req.Tenant)
		if err != nil {
			return err
		}
		if err = lockStream(ctx, tx, StgSampleStream); err != nil {
			return err
		}
		s, err := stgSampleState(ctx, tx)
		if err != nil {
			return err
		}
		x := s.Samples[req.Approval]
		if x != nil {
			if x.P != req.P || x.U != req.U || x.Infra != req.Infra {
				return sampleError("conflict", "approval already allocated a sample with other inputs")
			}
			if x.Abandoned {
				return sampleError("conflict", "approval's allocation was abandoned")
			}
			if x.UID != "" {
				out = *x
				return nil
			}
		}
		if refusal != nil {
			return refusal
		}
		if x != nil {
			out = *x
			return nil
		}
		if req.Abandon != nil {
			old := s.Open()
			if old == nil || old.Approval != req.Abandon.Approval || old.UID != "" {
				return sampleError("conflict", "abandon proof does not name the open, uncreated allocation")
			}
			if _, err = Append(ctx, tx, Event{Stream: StgSampleStream, Type: "note", Actor: "controller", Body: map[string]any{
				"text": "stg sample of " + old.Approval + " abandoned: its run was absent after the grace",
				"stg_sample": map[string]any{"v": 1, "kind": "abandoned", "approval": old.Approval, "by": req.Approval,
					"run": req.Abandon.Run, "read_at": req.Abandon.ReadAt.UTC().Format(time.RFC3339Nano)}}}); err != nil {
				return err
			}
		}
		if old := s.Open(); old != nil && req.Abandon == nil {
			return sampleError("exists", "sample of approval "+old.Approval+" is still open")
		}
		if _, err = Append(ctx, tx, Event{Stream: StgSampleStream, Type: "note", Actor: "controller", Body: map[string]any{
			"text":       "stg sample allocated for approval " + req.Approval,
			"stg_sample": map[string]any{"v": 1, "kind": "allocated", "approval": req.Approval, "P": req.P, "U": req.U, "infra": req.Infra}}}); err != nil {
			return err
		}
		s, err = stgSampleState(ctx, tx)
		if err != nil {
			return err
		}
		out = *s.Samples[req.Approval]
		return nil
	})
	return out, err
}

// PermitStgSampleSend is the last check before an allocation's POST; the
// caller runs it inside the Create's deadline. It passes only when
// stgSampleGate passes and the allocation is still open and uncreated, and
// then records "sending" under the stream lock. An abandon of the same
// allocation is therefore either committed first, and this refuses with
// code abandoned, or it must wait StgSampleAbandonAfterSend after this record.
func PermitStgSampleSend(ctx context.Context, db DB, tenant, approval string) (StgSample, error) {
	var out StgSample
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		refusal, err := stgSampleGate(ctx, tx, tenant)
		if err != nil {
			return err
		}
		if refusal != nil {
			return refusal
		}
		if err = lockStream(ctx, tx, StgSampleStream); err != nil {
			return err
		}
		s, err := stgSampleState(ctx, tx)
		if err != nil {
			return err
		}
		x := s.Samples[approval]
		switch {
		case x == nil:
			return sampleError("missing", "no sample was allocated by approval "+approval)
		case x.Abandoned:
			return sampleError("abandoned", "approval's allocation was abandoned by "+x.AbandonedBy+"; its run is never sent")
		case !x.Open() || x.UID != "":
			return sampleError("conflict", "sample of approval "+approval+" is already created")
		}
		if _, err = Append(ctx, tx, Event{Stream: StgSampleStream, Type: "note", Actor: "controller", Body: map[string]any{
			"text": "stg sample sending for approval " + approval, "stg_sample": map[string]any{"v": 1, "kind": "sending", "approval": approval}}}); err != nil {
			return err
		}
		if s, err = stgSampleState(ctx, tx); err != nil {
			return err
		}
		out = *s.Samples[approval]
		return nil
	})
	return out, err
}

// RecordStgSampleCreated stores the UID the Create returned. The same UID
// again is a no-op; a different one is a conflict.
func RecordStgSampleCreated(ctx context.Context, db DB, approval, uid string) (StgSample, error) {
	return stgSampleStep(ctx, db, approval, func(x *StgSample) (map[string]any, error) {
		if x.UID == uid {
			return nil, nil
		}
		return map[string]any{"v": 1, "kind": "created", "approval": approval, "uid": uid}, nil
	})
}

// ArchiveStgSample stores the run's terminal evidence before any finalizer
// is removed. An existing archive of the same UID is returned unchanged.
func ArchiveStgSample(ctx context.Context, db DB, approval, by, uid, status string, raw map[string]any) (StgSample, error) {
	return stgSampleStep(ctx, db, approval, func(x *StgSample) (map[string]any, error) {
		if x.Archived && x.UID == uid {
			return nil, nil
		}
		return map[string]any{"v": 1, "kind": "archived", "approval": approval, "by": by, "uid": uid, "status": status, "raw": raw}, nil
	})
}

// ReleaseStgSample closes the sample after its finalizer is gone.
func ReleaseStgSample(ctx context.Context, db DB, approval, by, uid string) (StgSample, error) {
	return stgSampleStep(ctx, db, approval, func(x *StgSample) (map[string]any, error) {
		if x.Released && x.UID == uid {
			return nil, nil
		}
		return map[string]any{"v": 1, "kind": "released", "approval": approval, "by": by, "uid": uid}, nil
	})
}

func stgSampleStep(ctx context.Context, db DB, approval string, next func(*StgSample) (map[string]any, error)) (StgSample, error) {
	var out StgSample
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if err := lockStream(ctx, tx, StgSampleStream); err != nil {
			return err
		}
		s, err := stgSampleState(ctx, tx)
		if err != nil {
			return err
		}
		x := s.Samples[approval]
		if x == nil {
			return sampleError("missing", "no sample was allocated by approval "+approval)
		}
		record, err := next(x)
		if err != nil {
			return err
		}
		if record == nil {
			out = *x
			return nil
		}
		if _, err = Append(ctx, tx, Event{Stream: StgSampleStream, Type: "note", Actor: "controller",
			Body: map[string]any{"text": "stg sample " + record["kind"].(string) + " for approval " + approval, "stg_sample": record}}); err != nil {
			return err
		}
		if s, err = stgSampleState(ctx, tx); err != nil {
			return err
		}
		out = *s.Samples[approval]
		return nil
	})
	return out, err
}
