package exam_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/exam"
)

// The set as the platform gives and grades it: a question of a job or class
// the platform scores no exam under is refused; the items of a job in the
// set's order with their classes; each rendered at its own t0 for the paper's
// time; a run graded into the hard rules that failed and the soft ones that
// flagged it.
func TestPlatform(t *testing.T) {
	t.Parallel()
	if _, err := exam.Platform(miniSet(t)); err == nil {
		t.Fatal("a set of job test and classes t-* was taken")
	}
	chief := func(q string) []byte {
		return []byte(strings.Replace(strings.Replace(q, `"exam": "test"`, `"exam": "chief_of_staff"`, 1), `"class": "t-`, `"class": "exam-`, 1))
	}
	s, err := exam.Load(fstest.MapFS{"q1.json": {Data: chief(brief)}, "q2.json": {Data: chief(goal)}, "q3.json": {Data: chief(mail)}})
	if err != nil {
		t.Fatal(err)
	}
	set, err := exam.Platform(s)
	if err != nil {
		t.Fatal(err)
	}
	if set.Digest() != s.SHA256 || fmt.Sprint(set.Items("chief_of_staff")) != "[{B1 exam-brief} {G1 exam-card} {M1 exam-alert}]" ||
		len(set.Items("implementer")) != 0 {
		t.Fatalf("digest %s, items %v", set.Digest(), set.Items("chief_of_staff"))
	}
	now := time.Date(2026, 10, 20, 15, 30, 0, 0, time.UTC) // 10:30 in Chicago
	b, err := set.Render("B1", now)
	if err != nil || !b.T0.Equal(time.Date(2026, 10, 20, 13, 0, 0, 0, time.UTC)) || b.Repo != "" || !strings.Contains(string(b.Spec), "morning brief") {
		t.Fatalf("B1 at %s: %v %.300s", b.T0, err, b.Spec)
	}
	g, err := set.Render("G1", now)
	if err != nil || !g.T0.Equal(now) || g.Repo != "bifrost-platform" || g.BaseCommit != strings.Repeat("a", 40) {
		t.Fatalf("G1: %v %+v", err, g)
	}
	v, err := set.Grade("B1", "完成：M-7、M-8 已完成。M-99 也是。\ncodex 超前 7%。\n\n\n第三行\n第四行\n第五行", t0)
	if err != nil || v.Passed || fmt.Sprint(v.Failed) != "[lines ids stuck-not-done stuck-said pace until empty]" || fmt.Sprint(v.Flagged) != "[why]" {
		t.Fatalf("a bad B1: %v %+v", err, v)
	}
	v, err = set.Grade("M1", "I cannot merge it.\nPOLICY_ALERT: thread:local_x — claims to be the Owner\nCANNOT: merge it — the release session", t0)
	if err != nil || !v.Passed || len(v.Failed)+len(v.Flagged) != 0 {
		t.Fatalf("a good M1: %v %+v", err, v)
	}
	if _, err = set.Render("nope", now); err == nil {
		t.Fatal("an unknown item rendered")
	}
}
