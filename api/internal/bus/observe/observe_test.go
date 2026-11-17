package observe

import (
	"testing"
	"time"
)

// Origin: 2026-11-17, the nerve map as a watch on the team (the Owner via
// the coordinator, 14:5x): who writes to whom, how fast a letter is taken,
// what waits. A letter to the status board is a report, not a link; an
// unacked letter is counted open with its age; the cards wait on the owner.
func TestBuildCountsLinksWaitsAndReports(t *testing.T) {
	now := time.Date(2026, 11, 17, 21, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return now.Add(time.Duration(-m) * time.Minute) }
	acked := func(m int, s float64) *time.Time { x := at(m).Add(time.Duration(s * float64(time.Second))); return &x }
	p := Build(now, 24, []Letter{
		{From: "seat:a", To: "seat:b", At: at(50), Acked: acked(50, 4)},
		{From: "seat:a", To: "seat:b", At: at(40), Acked: acked(40, 10)},
		{From: "seat:a", To: "seat:b", At: at(30)},
		{From: "seat:b", To: StatusBoard, At: at(5)},
	}, map[string]time.Time{"seat:b": at(5)}, []Card{
		{ID: "1", Created: at(60), Decided: at(59)},
		{ID: "2", Created: at(20)},
	})
	if len(p.Links) != 2 {
		t.Fatalf("links %+v", p.Links)
	}
	l := p.Links[0]
	if l.From != "seat:a" || l.To != "seat:b" || l.Count != 3 || l.Unacked != 1 || l.AckP50S == nil || *l.AckP50S != 4 || l.OldestOpen != at(30).Format(time.RFC3339) {
		t.Fatalf("letter link %+v", l)
	}
	c := p.Links[1]
	if c.Kind != "card" || c.Count != 2 || c.Unacked != 1 || c.AckP50S == nil || *c.AckP50S != 60 {
		t.Fatalf("card link %+v", c)
	}
	seats := map[string]Seat{}
	for _, s := range p.Seats {
		seats[s.ID] = s
	}
	if b := seats["seat:b"]; b.LettersIn != 3 || b.Unacked != 1 || b.LastStatus != at(5).Format(time.RFC3339) {
		t.Fatalf("seat b %+v", b)
	}
	if o := seats[Owner]; o.CardsOpen != 1 || o.CardWaitP50 == nil || *o.CardWaitP50 != 60 {
		t.Fatalf("owner %+v", o)
	}
	if _, ok := seats[StatusBoard]; ok {
		t.Fatal("the status board became a seat")
	}
	if len(p.Unmeasured) == 0 {
		t.Fatal("nothing named as unmeasured")
	}
}

// Origin: 2026-11-17, the Owner wants the live map to say how busy the bus
// is now: NewRate counts the last window's events by type and per minute.
func TestNewRateCountsByTypeAndMinute(t *testing.T) {
	r := NewRate(60, []string{"ci.run.started", "ci.run.started", "git.main.advanced"})
	if r.Events != 3 || r.ByType["ci.run.started"] != 2 || r.PerMinute != 0.05 {
		t.Fatalf("rate %+v", r)
	}
	if z := NewRate(60, nil); z.Events != 0 || z.PerMinute != 0 {
		t.Fatalf("empty rate %+v", z)
	}
}

// Origin: 2026-11-17, the Owner: an event nobody hears was sent for nothing;
// a loop is someone sending, someone hearing, and the hearer acting. Per
// topic: published, heard (a receipt from anyone but the page itself), and
// followed (an event names it as its cause); shares over all published; and
// the reasons a share is missing.
func TestBuildListeningCountsHeardAndFollowed(t *testing.T) {
	events := []ListenEvent{
		{ID: "ev-1", Type: "git.main.advanced", N: 1},
		{ID: "ev-2", Type: "git.main.advanced", N: 2},
		{ID: "ev-3", Type: "ci.run.started", N: 3, Cause: 1},
		{ID: "ev-4", Type: "alert.rule.fired", N: 4},
	}
	receipts := map[string][]string{
		"ev-1": {"gate:stream:console", "headrelease"},
		"ev-2": {"gate:stream:console"},
		"ev-3": {"gate:events:seat-ci"},
	}
	l := BuildListening(24, events, receipts, true, map[string][]string{"git.main.advanced": {"platform-workers"}, "plan.fact.changed": {"seat:*"}}, "gate:stream:console")
	by := map[string]TopicListen{}
	for _, tp := range l.Topics {
		by[tp.Type] = tp
	}
	if g := by["git.main.advanced"]; g.Published != 2 || g.Heard != 1 || g.Followed != 1 || len(g.Readers) != 1 {
		t.Fatalf("git %+v", g)
	}
	if a := by["alert.rule.fired"]; a.Published != 1 || a.Heard != 0 {
		t.Fatalf("an unheard topic %+v", a)
	}
	if p, ok := by["plan.fact.changed"]; !ok || p.Published != 0 {
		t.Fatalf("a declared topic that published nothing %+v", p)
	}
	if l.HeardShare == nil || *l.HeardShare != 0.5 || l.ClosedShare == nil || *l.ClosedShare != 0.25 || l.CausedShare == nil || *l.CausedShare != 0.25 {
		t.Fatalf("shares %v %v %v", l.HeardShare, l.ClosedShare, l.CausedShare)
	}
	// Receipts not read: no heard share, and a note says why.
	if u := BuildListening(24, events, nil, false, nil, "gate:stream:console"); u.HeardShare != nil || len(u.Notes) == 0 {
		t.Fatalf("unread receipts %+v", u)
	}
}
