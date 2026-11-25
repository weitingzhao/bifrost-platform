package planstore_test

import (
	"errors"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// issuer is a thread writing issues for the first time (2026-11-25: the first
// load on PROD failed on the actor key; the write registers its thread).
var issuer = actor.Writer{Actor: "thread:7274b722-9792-4ba2-bd5c-2dd8aff76cb5", Token: "reporter"}

// Origin: 2026-11-25, the Owner: 「你第三轮扫出来的那些吐槽需要有一个去处」. An
// issue is recorded as a new version only when it changes, so the legislation
// line may post the whole list again; and once a law answers it, the reading
// counts what he said about it after the law went in force.
func TestOwnerIssueVersionsAndSaidAfterLaw(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	tr := "7274b722-9792-4ba2-bd5c-2dd8aff76cb5"
	law := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	before, after := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	_, err := planstore.RecordOwnerSaid(ctx, w.rt, "controller", []planstore.OwnerSaid{
		{MessageID: before, Transcript: tr, SaidAt: law.Add(-time.Hour), Text: "怎么又停了"},
		{MessageID: after, Transcript: tr, SaidAt: law.Add(time.Hour), Text: "又停了，要我叫醒"},
	})
	ok(t, err)
	issue := planstore.OwnerIssue{Issue: "ISSUE-1", Title: "线程停下要 Owner 叫醒", Kinds: []string{"pain"}, Action: "enforce",
		Detail: "法已规定不等 Owner；缺唤醒机制", To: "plan", Status: "open", Evidence: []string{before, after},
		Quotes: []string{"怎么又停了"}, Times: 2, FirstAt: law.Add(-time.Hour), LastAt: law.Add(time.Hour), Round: 3}
	n, err := planstore.RecordOwnerIssues(ctx, w.rt, issuer, []planstore.OwnerIssue{issue})
	ok(t, err)
	if n != 1 {
		t.Fatalf("recorded %d, want 1", n)
	}
	// Positive control of the case above: posted again unchanged, nothing is recorded.
	if n, err = planstore.RecordOwnerIssues(ctx, w.rt, issuer, []planstore.OwnerIssue{issue}); err != nil || n != 0 {
		t.Fatalf("posted again: recorded %d, %v", n, err)
	}
	got, err := planstore.OwnerIssues(ctx, w.rt)
	ok(t, err)
	if len(got) != 1 || got[0].Version != 1 || got[0].SaidAfterLaw != nil || got[0].By != issuer.Actor {
		t.Fatalf("read %+v", got)
	}
	// The law that answers it goes in force: one of the two messages came after.
	issue.Status, issue.LawRef, issue.LawInForceAt = "in_force", "LAW-6 三", &law
	if n, err = planstore.RecordOwnerIssues(ctx, w.rt, issuer, []planstore.OwnerIssue{issue}); err != nil || n != 1 {
		t.Fatalf("changed: recorded %d, %v", n, err)
	}
	got, err = planstore.OwnerIssues(ctx, w.rt)
	ok(t, err)
	if len(got) != 1 || got[0].Version != 2 || got[0].SaidAfterLaw == nil || *got[0].SaidAfterLaw != 1 {
		t.Fatalf("after the law %+v", got)
	}
}

// Origin: same decision. What the store refuses: an issue with no evidence, an
// action outside the five ways and a conflict, and an issue in force without
// the time its law went in force.
func TestOwnerIssueRefusals(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	at := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	good := planstore.OwnerIssue{Issue: "ISSUE-2", Title: "t", Kinds: []string{"need"}, Action: "law-new", Detail: "d", Status: "open",
		Evidence: []string{"11111111-1111-4111-8111-111111111111"}, Times: 1, FirstAt: at, LastAt: at}
	for name, change := range map[string]func(*planstore.OwnerIssue){
		"no evidence":           func(m *planstore.OwnerIssue) { m.Evidence = nil },
		"unknown action":        func(m *planstore.OwnerIssue) { m.Action = "later" },
		"in force with no time": func(m *planstore.OwnerIssue) { m.Status = "in_force" },
		"bad code":              func(m *planstore.OwnerIssue) { m.Issue = "T-1" },
	} {
		m := good
		change(&m)
		if _, err := planstore.RecordOwnerIssues(ctx, w.rt, issuer, []planstore.OwnerIssue{m}); !errors.Is(err, eventlog.ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
	// Positive control: the unchanged issue is recorded.
	if n, err := planstore.RecordOwnerIssues(ctx, w.rt, issuer, []planstore.OwnerIssue{good}); err != nil || n != 1 {
		t.Fatalf("good issue: %d, %v", n, err)
	}
}
