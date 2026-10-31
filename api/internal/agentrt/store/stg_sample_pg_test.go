package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

var (
	sampleCommitP = strings.Repeat("a", 40)
	sampleCommitU = strings.Repeat("b", 40)
	sampleCommitI = strings.Repeat("c", 40)
)

func sampleWorldPG(t *testing.T, stgRelease string) *world {
	t.Helper()
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	body := strings.Replace(landingRuleBody, "stg_release: bifrost-deliver-platform", "stg_release: "+stgRelease, 1)
	_, err := s.sign(w, ruleText("landing:dev", 1, "initial", since, until, body))
	ok(t, err)
	return w
}

func sampleAlloc(approval string) store.StgSampleAllocation {
	return store.StgSampleAllocation{Tenant: "dev", Approval: approval, P: sampleCommitP, U: sampleCommitU, Infra: sampleCommitI}
}

func wantSampleCode(t *testing.T, err error, code string) {
	t.Helper()
	var typed *store.StgSampleError
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("want stg_sample %s, got %v", code, err)
	}
}

// Serial: FreezeEventClock. The whole life of a sample on PostgreSQL, with
// the allocation's global event id as the batch number and every refusal of
// the stream leaving it unchanged.
func TestStgSamplePGLifecycle(t *testing.T) {
	w := sampleWorldPG(t, "none")
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	defer store.FreezeEventClock(at)()
	open, err := store.StgSampleOpenRule(ctx, w.rt, "dev")
	ok(t, err)
	if open {
		t.Fatal("none rule reads as open")
	}
	x, err := store.AllocateStgSample(ctx, w.rt, sampleAlloc("A1"))
	ok(t, err)
	var eventID int64
	ok(t, w.rt.QueryRow(ctx, `SELECT event_id FROM agentrt.event WHERE event_stream_id=$1 AND seq=1`, store.StgSampleStream).Scan(&eventID))
	if x.Allocation != eventID || x.IntentSeq != 1 || x.UID != "" {
		t.Fatalf("allocation %+v event %d", x, eventID)
	}
	again, err := store.AllocateStgSample(ctx, w.rt, sampleAlloc("A1"))
	ok(t, err)
	if again.Allocation != x.Allocation {
		t.Fatal("allocation is idempotent per approval", again)
	}
	other := sampleAlloc("A1")
	other.U = strings.Repeat("d", 40)
	_, err = store.AllocateStgSample(ctx, w.rt, other)
	wantSampleCode(t, err, "conflict")
	_, err = store.AllocateStgSample(ctx, w.rt, sampleAlloc("A2"))
	wantSampleCode(t, err, "exists")
	_, err = store.ArchiveStgSample(ctx, w.rt, "A1", "F1", "uid-1", "Succeeded", map[string]any{})
	wantSampleCode(t, err, "conflict")

	x, err = store.RecordStgSampleCreated(ctx, w.rt, "A1", "uid-1")
	ok(t, err)
	if x.UID != "uid-1" {
		t.Fatal(x)
	}
	_, err = store.RecordStgSampleCreated(ctx, w.rt, "A1", "uid-1")
	ok(t, err)
	_, err = store.RecordStgSampleCreated(ctx, w.rt, "A1", "uid-2")
	wantSampleCode(t, err, "conflict")
	_, err = store.ReleaseStgSample(ctx, w.rt, "A1", "F1", "uid-1")
	wantSampleCode(t, err, "conflict")
	raw := map[string]any{"results": []any{}, "child_refs": []any{map[string]any{"name": "tr", "task_run": map[string]any{"metadata": map[string]any{"uid": "child", "generation": 3}}}}, "conditions": []any{}}
	x, err = store.ArchiveStgSample(ctx, w.rt, "A1", "F1", "uid-1", "Succeeded", raw)
	ok(t, err)
	_, err = store.ArchiveStgSample(ctx, w.rt, "A1", "F2", "uid-1", "Succeeded", raw)
	ok(t, err)
	x, err = store.ReleaseStgSample(ctx, w.rt, "A1", "F2", "uid-1")
	ok(t, err)
	if !x.Archived || !x.Released || x.ArchivedBy != "F1" || x.ReleasedBy != "F2" || x.ArchivedSeq >= x.ReleasedSeq {
		t.Fatalf("archive then release %+v", x)
	}
	s, err := store.ReadStgSamples(ctx, w.rt)
	ok(t, err)
	if s.Open() != nil || s.LastSeq != 4 {
		t.Fatalf("closed stream %+v", s)
	}
	y, err := store.AllocateStgSample(ctx, w.rt, sampleAlloc("A2"))
	ok(t, err)
	if y.Allocation <= x.Allocation || y.IntentSeq != 5 {
		t.Fatal("a released sample frees the stream", y)
	}
}

// The stream is closed to every other writer: a direct Append must pass the
// same checks the writers do.
func TestStgSamplePGAppendGuards(t *testing.T) {
	t.Parallel()
	w := sampleWorldPG(t, "none")
	good := func() store.Event {
		return store.Event{Stream: store.StgSampleStream, Type: "note", Actor: "controller", Body: map[string]any{"text": "t",
			"stg_sample": map[string]any{"v": 1, "kind": "allocated", "approval": "A1", "P": sampleCommitP, "U": sampleCommitU, "infra": sampleCommitI}}}
	}
	for name, mutate := range map[string]func(*store.Event){
		"owner actor":     func(e *store.Event) { e.Actor = "owner" },
		"other stream":    func(e *store.Event) { e.Stream = "landing:bifrost-platform" },
		"plain note":      func(e *store.Event) { delete(e.Body, "stg_sample") },
		"mission binding": func(e *store.Event) { e.MissionID = "M-1" },
		"created before alloc": func(e *store.Event) {
			e.Body["stg_sample"] = map[string]any{"v": 1, "kind": "created", "approval": "A1", "uid": "u"}
		},
	} {
		ev := good()
		mutate(&ev)
		err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error { _, err := store.Append(ctx, tx, ev); return err })
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	s, err := store.ReadStgSamples(ctx, w.rt)
	ok(t, err)
	if s.LastSeq != 0 {
		t.Fatal("refused appends wrote the stream")
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { _, err := store.Append(ctx, tx, good()); return err }))
}

// An enabled rule refuses every new sample; the sample is for the first
// opening only.
// Serial: FreezeEventClock
func TestStgSamplePGRuleOpenRefuses(t *testing.T) {
	w := sampleWorldPG(t, "bifrost-deliver-platform")
	defer store.FreezeEventClock(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))()
	open, err := store.StgSampleOpenRule(ctx, w.rt, "dev")
	ok(t, err)
	if !open {
		t.Fatal("enabled rule reads as none")
	}
	_, err = store.AllocateStgSample(ctx, w.rt, sampleAlloc("A1"))
	wantSampleCode(t, err, "rule_open")
	s, err := store.ReadStgSamples(ctx, w.rt)
	ok(t, err)
	if s.LastSeq != 0 {
		t.Fatal("refusal wrote the stream")
	}
}

// Serial: FreezeEventClock. Once B5 has opened a batch, a later none does
// not make the sample usable again: B5's own held runs are the samples.
func TestStgSamplePGRefusesAfterB5Opened(t *testing.T) {
	x := b5Ready(t)
	x.none(t)
	s, err := store.ReadStgRelease(ctx, x.w.rt)
	ok(t, err)
	if s.LastBatch < 1 {
		t.Fatal("fixture opened no batch")
	}
	_, err = store.AllocateStgSample(ctx, x.w.rt, sampleAlloc("A1"))
	wantSampleCode(t, err, "not_first")
}

// Serial: FreezeEventClock. An allocation whose run never appeared is
// abandoned only by another approval's proof read after the grace.
func TestStgSamplePGAbandon(t *testing.T) {
	w := sampleWorldPG(t, "none")
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	restore := store.FreezeEventClock(at)
	defer func() { restore() }()
	old, err := store.AllocateStgSample(ctx, w.rt, sampleAlloc("A1"))
	ok(t, err)
	early := sampleAlloc("A2")
	early.Abandon = &store.StgSampleAbsence{Approval: "A1", Run: "stgrel-x", ReadAt: at.Add(time.Minute)}
	store.FreezeEventClock(at.Add(time.Minute))
	_, err = store.AllocateStgSample(ctx, w.rt, early)
	wantSampleCode(t, err, "conflict")
	later := sampleAlloc("A2")
	later.Abandon = &store.StgSampleAbsence{Approval: "A1", Run: "stgrel-x", ReadAt: at.Add(3 * time.Minute)}
	store.FreezeEventClock(at.Add(3 * time.Minute))
	y, err := store.AllocateStgSample(ctx, w.rt, later)
	ok(t, err)
	s, err := store.ReadStgSamples(ctx, w.rt)
	ok(t, err)
	if !s.Samples["A1"].Abandoned || s.Samples["A1"].AbandonedBy != "A2" || s.Open().Approval != "A2" || y.Allocation <= old.Allocation {
		t.Fatalf("abandon %+v", s.Samples["A1"])
	}
	// A late run of the abandoned allocation is recorded for Finalize's
	// adoption; it does not reopen A1 or displace A2 as the open sample.
	late, err := store.RecordStgSampleCreated(ctx, w.rt, "A1", "late-uid")
	ok(t, err)
	s, err = store.ReadStgSamples(ctx, w.rt)
	ok(t, err)
	if late.UID != "late-uid" || late.Open() || s.Open().Approval != "A2" {
		t.Fatalf("late A1 %+v open %+v", late, s.Open())
	}
	_, err = store.PermitStgSampleSend(ctx, w.rt, "dev", "A1")
	wantSampleCode(t, err, "abandoned")
}

// Serial: FreezeEventClock. TD-460 review A1: the send permit records each
// send; an abandon needs the grace from the last one, and an abandon
// committed first makes the send permit refuse.
func TestStgSamplePGSendPermit(t *testing.T) {
	w := sampleWorldPG(t, "none")
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	restore := store.FreezeEventClock(at)
	defer func() { restore() }()
	_, err := store.PermitStgSampleSend(ctx, w.rt, "dev", "A1")
	wantSampleCode(t, err, "missing")
	_, err = store.AllocateStgSample(ctx, w.rt, sampleAlloc("A1"))
	ok(t, err)
	store.FreezeEventClock(at.Add(2 * time.Minute))
	x, err := store.PermitStgSampleSend(ctx, w.rt, "dev", "A1")
	ok(t, err)
	if !x.SentAt.Equal(at.Add(2*time.Minute)) || x.SendingSeq != 2 {
		t.Fatalf("send record %+v", x)
	}
	early := sampleAlloc("A2")
	early.Abandon = &store.StgSampleAbsence{Approval: "A1", Run: "stgrel-x", ReadAt: at.Add(3 * time.Minute)}
	store.FreezeEventClock(at.Add(3 * time.Minute))
	_, err = store.AllocateStgSample(ctx, w.rt, early)
	wantSampleCode(t, err, "conflict")
	// TD-460 landing review P1: a 404 shortly after the client deadline is
	// not proof after a send; the proof needs 30 minutes from the send.
	notYet := sampleAlloc("A2")
	notYet.Abandon = &store.StgSampleAbsence{Approval: "A1", Run: "stgrel-x", ReadAt: at.Add(31 * time.Minute)}
	store.FreezeEventClock(at.Add(31 * time.Minute))
	_, err = store.AllocateStgSample(ctx, w.rt, notYet)
	wantSampleCode(t, err, "conflict")
	later := sampleAlloc("A2")
	later.Abandon = &store.StgSampleAbsence{Approval: "A1", Run: "stgrel-x", ReadAt: at.Add(2*time.Minute + store.StgSampleAbandonAfterSend)}
	store.FreezeEventClock(at.Add(2*time.Minute + store.StgSampleAbandonAfterSend))
	_, err = store.AllocateStgSample(ctx, w.rt, later)
	ok(t, err)
	_, err = store.PermitStgSampleSend(ctx, w.rt, "dev", "A1")
	wantSampleCode(t, err, "abandoned")
	// The defence in depth: a run of A1 stored after all is adopted, archived
	// and released on the closed stream, A2 staying the open sample.
	_, err = store.RecordStgSampleCreated(ctx, w.rt, "A1", "late-uid")
	ok(t, err)
	_, err = store.ArchiveStgSample(ctx, w.rt, "A1", "F1", "late-uid", "Succeeded", map[string]any{})
	ok(t, err)
	a1, err := store.ReleaseStgSample(ctx, w.rt, "A1", "F1", "late-uid")
	ok(t, err)
	if !a1.Abandoned || !a1.Released || a1.UID != "late-uid" {
		t.Fatalf("late A1 %+v", a1)
	}
	_, err = store.RecordStgSampleCreated(ctx, w.rt, "A1", "other-uid")
	wantSampleCode(t, err, "missing")
	_, err = store.PermitStgSampleSend(ctx, w.rt, "dev", "A2")
	ok(t, err)
	_, err = store.RecordStgSampleCreated(ctx, w.rt, "A2", "uid-2")
	ok(t, err)
	_, err = store.PermitStgSampleSend(ctx, w.rt, "dev", "A2")
	wantSampleCode(t, err, "conflict")
}

// Serial: FreezeEventClock. TD-460 review B1: an allocation that has not
// been created yet passes the first-opening gate again on retry, both in the
// allocation and in the send permit; a created one is returned as recorded.
func TestStgSamplePGRetryKeepsTheGate(t *testing.T) {
	x := b5Ready(t)
	// An allocation from before the opening, written as Allocate writes it.
	ok(t, store.InTx(ctx, x.w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: store.StgSampleStream, Type: "note", Actor: "controller", Body: map[string]any{"text": "t",
			"stg_sample": map[string]any{"v": 1, "kind": "allocated", "approval": "A1", "P": sampleCommitP, "U": sampleCommitU, "infra": sampleCommitI}}})
		return err
	}))
	_, err := store.AllocateStgSample(ctx, x.w.rt, sampleAlloc("A1"))
	wantSampleCode(t, err, "rule_open")
	_, err = store.PermitStgSampleSend(ctx, x.w.rt, "dev", "A1")
	wantSampleCode(t, err, "rule_open")
	x.none(t)
	_, err = store.AllocateStgSample(ctx, x.w.rt, sampleAlloc("A1"))
	wantSampleCode(t, err, "not_first")
	_, err = store.PermitStgSampleSend(ctx, x.w.rt, "dev", "A1")
	wantSampleCode(t, err, "not_first")
	_, err = store.RecordStgSampleCreated(ctx, x.w.rt, "A1", "uid-1")
	ok(t, err)
	got, err := store.AllocateStgSample(ctx, x.w.rt, sampleAlloc("A1"))
	ok(t, err)
	if got.UID != "uid-1" {
		t.Fatal("a created sample is returned as recorded", got)
	}
	s, err := store.ReadStgSamples(ctx, x.w.rt)
	ok(t, err)
	if s.Samples["A1"].SendingSeq != 0 {
		t.Fatal("a refused permit recorded a send", s.Samples["A1"])
	}
}
