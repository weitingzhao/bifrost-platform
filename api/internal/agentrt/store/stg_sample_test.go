package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var sampleFoldAt = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

func sampleEntry(seq int64, at time.Time, record map[string]any) StgSampleEntry {
	record["v"] = 1
	return StgSampleEntry{EventID: 9000 + seq*3, Seq: seq, At: at, Type: "note", Actor: "controller", Body: map[string]any{"text": "t", "stg_sample": record}}
}

func sampleAllocated(approval string) map[string]any {
	return map[string]any{"kind": "allocated", "approval": approval, "P": strings.Repeat("a", 40), "U": strings.Repeat("b", 40), "infra": strings.Repeat("c", 40)}
}

func TestStgSampleFoldLifecycle(t *testing.T) {
	t.Parallel()
	at := sampleFoldAt
	s, err := StgSampleFold([]StgSampleEntry{
		sampleEntry(1, at, sampleAllocated("A1")),
		sampleEntry(2, at, map[string]any{"kind": "created", "approval": "A1", "uid": "uid-1"}),
		sampleEntry(3, at, map[string]any{"kind": "archived", "approval": "A1", "by": "F1", "uid": "uid-1", "status": "Succeeded", "raw": map[string]any{"child_refs": []any{}}}),
		sampleEntry(4, at, map[string]any{"kind": "released", "approval": "A1", "by": "F2", "uid": "uid-1"}),
		sampleEntry(5, at, sampleAllocated("A2")),
	})
	if err != nil {
		t.Fatal(err)
	}
	a1 := s.Samples["A1"]
	if a1.Allocation != 9003 || a1.IntentSeq != 1 || a1.UID != "uid-1" || !a1.Archived || !a1.Released || a1.ArchivedBy != "F1" || a1.ReleasedBy != "F2" || a1.Status != "Succeeded" {
		t.Fatalf("A1 %+v", a1)
	}
	if s.Open() != s.Samples["A2"] || s.Samples["A2"].Allocation != 9015 {
		t.Fatal("A2 is the open sample", s.Open())
	}
}

func TestStgSampleFoldRefusals(t *testing.T) {
	t.Parallel()
	at := sampleFoldAt
	created := sampleEntry(2, at, map[string]any{"kind": "created", "approval": "A1", "uid": "uid-1"})
	base := []StgSampleEntry{sampleEntry(1, at, sampleAllocated("A1"))}
	for _, test := range []struct {
		name string
		next []StgSampleEntry
		code string
	}{
		{"second sample while one is open", []StgSampleEntry{sampleEntry(2, at, sampleAllocated("A2"))}, "exists"},
		{"same approval allocates twice", []StgSampleEntry{sampleEntry(2, at, sampleAllocated("A1"))}, "conflict"},
		{"created twice", []StgSampleEntry{created, sampleEntry(3, at, map[string]any{"kind": "created", "approval": "A1", "uid": "uid-2"})}, "conflict"},
		{"archive before created", []StgSampleEntry{sampleEntry(2, at, map[string]any{"kind": "archived", "approval": "A1", "by": "F", "uid": "uid-1", "status": "Succeeded", "raw": map[string]any{}})}, "conflict"},
		{"archive of another uid", []StgSampleEntry{created, sampleEntry(3, at, map[string]any{"kind": "archived", "approval": "A1", "by": "F", "uid": "uid-2", "status": "Succeeded", "raw": map[string]any{}})}, "conflict"},
		{"archive of a running run", []StgSampleEntry{created, sampleEntry(3, at, map[string]any{"kind": "archived", "approval": "A1", "by": "F", "uid": "uid-1", "status": "Running", "raw": map[string]any{}})}, "invalid"},
		{"release before archive", []StgSampleEntry{created, sampleEntry(3, at, map[string]any{"kind": "released", "approval": "A1", "by": "F", "uid": "uid-1"})}, "conflict"},
		{"abandon a created sample", []StgSampleEntry{created, sampleEntry(3, at.Add(time.Hour), map[string]any{"kind": "abandoned", "approval": "A1", "by": "A2", "run": "r", "read_at": at.Add(time.Hour).Format(time.RFC3339Nano)})}, "conflict"},
		{"abandon inside the grace", []StgSampleEntry{sampleEntry(2, at.Add(time.Minute), map[string]any{"kind": "abandoned", "approval": "A1", "by": "A2", "run": "r", "read_at": at.Add(time.Minute).Format(time.RFC3339Nano)})}, "conflict"},
		{"abandon by the same approval", []StgSampleEntry{sampleEntry(2, at.Add(time.Hour), map[string]any{"kind": "abandoned", "approval": "A1", "by": "A1", "run": "r", "read_at": at.Add(time.Hour).Format(time.RFC3339Nano)})}, "conflict"},
		{"send after created", []StgSampleEntry{created, sampleEntry(3, at, map[string]any{"kind": "sending", "approval": "A1"})}, "conflict"},
		{"abandon inside the grace from the last send", []StgSampleEntry{
			sampleEntry(2, at.Add(3*time.Minute), map[string]any{"kind": "sending", "approval": "A1"}),
			sampleEntry(3, at.Add(4*time.Minute), map[string]any{"kind": "abandoned", "approval": "A1", "by": "A2", "run": "r", "read_at": at.Add(4 * time.Minute).Format(time.RFC3339Nano)})}, "conflict"},
		// TD-460 landing review P1: after a send, the 2 minute grace is not
		// enough; a 404 counts only 30 minutes after the last send record.
		{"abandon 29 minutes after a send", []StgSampleEntry{
			sampleEntry(2, at, map[string]any{"kind": "sending", "approval": "A1"}),
			sampleEntry(3, at.Add(29*time.Minute), map[string]any{"kind": "abandoned", "approval": "A1", "by": "A2", "run": "r", "read_at": at.Add(29 * time.Minute).Format(time.RFC3339Nano)})}, "conflict"},
		{"late run of an abandoned allocation adopted twice", []StgSampleEntry{
			sampleEntry(2, at.Add(3*time.Minute), map[string]any{"kind": "abandoned", "approval": "A1", "by": "A2", "run": "r", "read_at": at.Add(3 * time.Minute).Format(time.RFC3339Nano)}),
			sampleEntry(3, at.Add(4*time.Minute), map[string]any{"kind": "created", "approval": "A1", "uid": "uid-1"}),
			sampleEntry(4, at.Add(4*time.Minute), map[string]any{"kind": "created", "approval": "A1", "uid": "uid-2"})}, "conflict"},
		{"step after release", []StgSampleEntry{created,
			sampleEntry(3, at, map[string]any{"kind": "archived", "approval": "A1", "by": "F", "uid": "uid-1", "status": "Succeeded", "raw": map[string]any{}}),
			sampleEntry(4, at, map[string]any{"kind": "released", "approval": "A1", "by": "F", "uid": "uid-1"}),
			sampleEntry(5, at, map[string]any{"kind": "released", "approval": "A1", "by": "F", "uid": "uid-1"})}, "missing"},
		{"send after abandoned", []StgSampleEntry{
			sampleEntry(2, at.Add(3*time.Minute), map[string]any{"kind": "abandoned", "approval": "A1", "by": "A2", "run": "r", "read_at": at.Add(3 * time.Minute).Format(time.RFC3339Nano)}),
			sampleEntry(3, at.Add(3*time.Minute), map[string]any{"kind": "sending", "approval": "A1"})}, "missing"},
		{"step of an unknown approval", []StgSampleEntry{sampleEntry(2, at, map[string]any{"kind": "created", "approval": "A9", "uid": "uid-1"})}, "missing"},
		{"unknown kind", []StgSampleEntry{sampleEntry(2, at, map[string]any{"kind": "deleted", "approval": "A1"})}, "invalid"},
		{"extra field", []StgSampleEntry{sampleEntry(2, at, map[string]any{"kind": "created", "approval": "A1", "uid": "u", "finalizers": "none"})}, "invalid"},
		{"short commit", []StgSampleEntry{sampleEntry(2, at, map[string]any{"kind": "allocated", "approval": "A2", "P": "main", "U": strings.Repeat("b", 40), "infra": strings.Repeat("c", 40)})}, "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := StgSampleFold(append(append([]StgSampleEntry{}, base...), test.next...))
			var typed *StgSampleError
			if !errors.As(err, &typed) || typed.Code != test.code {
				t.Fatalf("want %s, got %v", test.code, err)
			}
		})
	}
	wrongActor := sampleEntry(1, at, sampleAllocated("A1"))
	wrongActor.Actor = "owner"
	if _, err := StgSampleFold([]StgSampleEntry{wrongActor}); err == nil {
		t.Fatal("non-controller record accepted")
	}
	abandoned, err := StgSampleFold(append(base, sampleEntry(2, at.Add(3*time.Minute), map[string]any{"kind": "abandoned", "approval": "A1", "by": "A2", "run": "r", "read_at": at.Add(3 * time.Minute).Format(time.RFC3339Nano)}),
		sampleEntry(3, at.Add(3*time.Minute), sampleAllocated("A2"))))
	if err != nil || !abandoned.Samples["A1"].Abandoned || abandoned.Open() != abandoned.Samples["A2"] {
		t.Fatal("proven absence frees the stream", err)
	}
	// After a send the proof needs 30 minutes from the last send record,
	// not from the allocation or the first send.
	sendAt := at.Add(4 * time.Minute)
	if _, err = StgSampleFold(append(base, sampleEntry(2, at.Add(time.Minute), map[string]any{"kind": "sending", "approval": "A1"}),
		sampleEntry(3, sendAt, map[string]any{"kind": "sending", "approval": "A1"}),
		sampleEntry(4, at.Add(32*time.Minute), map[string]any{"kind": "abandoned", "approval": "A1", "by": "A2", "run": "r", "read_at": at.Add(32 * time.Minute).Format(time.RFC3339Nano)}))); err == nil {
		t.Fatal("abandon 30 minutes after the first send but not the last")
	}
	sent, err := StgSampleFold(append(base, sampleEntry(2, at.Add(time.Minute), map[string]any{"kind": "sending", "approval": "A1"}),
		sampleEntry(3, sendAt, map[string]any{"kind": "sending", "approval": "A1"}),
		sampleEntry(4, sendAt.Add(StgSampleAbandonAfterSend), map[string]any{"kind": "abandoned", "approval": "A1", "by": "A2", "run": "r", "read_at": sendAt.Add(StgSampleAbandonAfterSend).Format(time.RFC3339Nano)})))
	if err != nil || !sent.Samples["A1"].Abandoned || !sent.Samples["A1"].SentAt.Equal(sendAt) || sent.Samples["A1"].SendingSeq != 3 {
		t.Fatal("abandon 30 minutes after the last send", err)
	}
	// A run stored after its allocation was abandoned is adopted, archived and
	// released while the replacement allocation stays the open sample.
	late, err := StgSampleFold(append(base, sampleEntry(2, at.Add(3*time.Minute), map[string]any{"kind": "abandoned", "approval": "A1", "by": "A2", "run": "r", "read_at": at.Add(3 * time.Minute).Format(time.RFC3339Nano)}),
		sampleEntry(3, at.Add(3*time.Minute), sampleAllocated("A2")),
		sampleEntry(4, at.Add(9*time.Minute), map[string]any{"kind": "created", "approval": "A1", "uid": "late-uid"}),
		sampleEntry(5, at.Add(9*time.Minute), map[string]any{"kind": "archived", "approval": "A1", "by": "F", "uid": "late-uid", "status": "Succeeded", "raw": map[string]any{}}),
		sampleEntry(6, at.Add(9*time.Minute), map[string]any{"kind": "released", "approval": "A1", "by": "F", "uid": "late-uid"})))
	if a1 := late.Samples["A1"]; err != nil || !a1.Abandoned || a1.UID != "late-uid" || !a1.Released || late.Open() != late.Samples["A2"] {
		t.Fatal("late run of an abandoned allocation is closed", err)
	}
}
