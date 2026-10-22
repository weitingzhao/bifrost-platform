package store_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// briefData is the data document of a brief's spec (controllerSpec: the one
// fenced JSON block after "## The data").
func briefData(t *testing.T, spec string) map[string]any {
	t.Helper()
	_, rest, found := strings.Cut(spec, "## The data")
	if !found {
		t.Fatalf("the spec has no data: %.800s", spec)
	}
	_, rest, _ = strings.Cut(rest, "```json\n")
	doc, _, found := strings.Cut(rest, "```")
	if !found {
		t.Fatalf("the data is not one fenced block: %.800s", rest)
	}
	var data map[string]any
	ok(t, json.Unmarshal([]byte(doc), &data))
	return data
}

// W-31 2.3 slice 6 (RULE-waits 总则 3: the record, then whom it calls): the
// scheduled brief reads the waits recorded past their limits from the slot
// before it to its own, with what waited, since when, its limit and whom it
// calls, and says so in its instructions; a wait recorded before that is the
// brief before's, not this one's.
// Serial: FreezeEventClock (it freezes the package's event clock).
func TestAScheduledBriefReadsTheWaitsThatPassed(t *testing.T) {
	w := newKernelWorld(t)
	// A ready task no host takes, its wait recorded 31 minutes on, every
	// event of it written at the time given.
	passed := func(at time.Time) string {
		t.Helper()
		thaw := store.FreezeEventClock(at)
		defer thaw()
		id := w.ready(t, store.Task{})
		if n, err := store.WaitLimits(ctx, w.rt, at.Add(31*time.Minute)); err != nil || n != 1 {
			t.Fatalf("waits recorded at %s: %d %v", at, n, err)
		}
		return id
	}
	// The 8:00 brief with none before it covers the 13 hours from 19:00.
	before := passed(chicago(t, "2026-10-18 18:00"))
	inside := passed(chicago(t, "2026-10-19 07:00"))
	_, err := store.ScheduleBrief(ctx, w.rt, chicago(t, "2026-10-19 08:10"))
	ok(t, err)
	mail, err := store.PendingMail(ctx, w.rt)
	if err != nil || len(mail) != 1 {
		t.Fatalf("pending %v %v", mail, err)
	}
	task, _ := takeAndReady(t, w, mail[0])
	spec := specText(t, w, task)
	// The platform's own waits, told apart from what waits for the Owner, and
	// an empty part that need not take a line of the 15 (the exam line,
	// 2026-10-22: the seat's briefs ran 9 to 14 lines).
	for _, want := range []string{"which of the platform's own", "waits passed their limit from since to until (not what waits for him)",
		"(waits_passed_since)", "Say plainly when a part is empty; empty parts may share one line."} {
		if !strings.Contains(spec, want) {
			t.Fatalf("the instructions lack %q: %.1500s", want, spec)
		}
	}
	data := briefData(t, spec)
	list, _ := data["waits_passed_since"].([]any)
	if len(list) != 1 {
		t.Fatalf("waits_passed_since %v, want the one wait from 19:00 to 8:00", data["waits_passed_since"])
	}
	got, _ := list[0].(map[string]any)
	// The task is the controller's, so its wait calls whoever proposed its
	// mission (waitCalls).
	var calls string
	ok(t, w.rt.QueryRow(ctx, `SELECT created_by_actor_id FROM agentrt.mission WHERE mission_id = 'M-1'`).Scan(&calls))
	want := map[string]any{"wait": store.WaitTaskReady, "ref": inside, "task": inside, "mission": "M-1", "title": "t",
		"since": chicago(t, "2026-10-19 07:00").UTC().Format(time.RFC3339), "limit_s": "1800", "calls": calls,
		"at": chicago(t, "2026-10-19 07:00").UTC().Format(time.RFC3339)}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("waits_passed_since[0].%s = %v, want %v (%v)", k, got[k], v, got)
		}
	}
	if got["waited_s"] == "" || got["waited_s"] == nil {
		t.Fatalf("no waited_s: %v", got)
	}
	if strings.Contains(spec, before) {
		t.Fatalf("the wait recorded before the window is in the brief: %s", before)
	}
}

// With no wait past its limit in the window the brief still has the list,
// empty, so the chief of staff can say the part is empty.
func TestAScheduledBriefWithNoWaitsSaysSo(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	_, err := store.ScheduleBrief(ctx, w.rt, chicago(t, "2026-10-19 08:10"))
	ok(t, err)
	mail, err := store.PendingMail(ctx, w.rt)
	if err != nil || len(mail) != 1 {
		t.Fatalf("pending %v %v", mail, err)
	}
	task, _ := takeAndReady(t, w, mail[0])
	data := briefData(t, specText(t, w, task))
	if list, isList := data["waits_passed_since"].([]any); !isList || len(list) != 0 {
		t.Fatalf("waits_passed_since %#v, want an empty list", data["waits_passed_since"])
	}
}
