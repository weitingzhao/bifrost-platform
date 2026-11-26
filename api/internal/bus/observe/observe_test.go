package observe

import (
	"testing"
	"time"
)

// Origin: the nerve map as a watch on the team (ISSUE-29): who writes to whom, how fast a letter is taken,
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

// Origin: 2026-11-17, ISSUE-29: the live map says how busy the bus
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

// Origin: an event nobody hears was sent for nothing (LAW-8 五); a loop is someone sending, someone hearing, and the hearer acting. Per
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
	l := BuildListening(24, events, receipts, true, map[string][]string{"git.main.advanced": {"platform-workers"}, "plan.fact.changed": {"seat:*"}}, "gate:stream:console", nil)
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
	if u := BuildListening(24, events, nil, false, nil, "gate:stream:console", nil); u.HeardShare != nil || len(u.Notes) == 0 {
		t.Fatalf("unread receipts %+v", u)
	}
	// 2026-11-17, the coordinator: ci.run.started (741 a day on PROD) is
	// published to be watched; counting it as unheard put a third of the bus
	// in the deaf share. A display-only topic keeps its own counts and leaves
	// the shares: 3 counted events, 1 heard (ev-1), 1 followed (ev-1).
	d := BuildListening(24, events, receipts, true, nil, "gate:stream:console", map[string]bool{"ci.run.started": true})
	if d.Counted != 3 || d.Watched != 1 || d.HeardShare == nil || *d.HeardShare != 0.333 || *d.ClosedShare != 0.333 {
		t.Fatalf("display-only shares %+v heard=%v closed=%v", d, d.HeardShare, d.ClosedShare)
	}
	for _, tp := range d.Topics {
		if tp.Type == "ci.run.started" && (!tp.DisplayOnly || tp.Published != 1 || tp.Heard != 1) {
			t.Fatalf("display-only topic %+v", tp)
		}
	}
}

// Origin: 2026-11-21, the main line "seen and felt" (LAW-8): which line
// waits on whose answer was not discoverable. An ask read but not answered
// left Unacked at 0. An ask is open until any letter comes back from its
// recipient to its sender after it; a note is never waited on.
func TestAnAskNobodyAnsweredIsOpen(t *testing.T) {
	t0 := time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC)
	read := t0.Add(time.Minute)
	letters := []Letter{
		{From: "thread:a", To: "thread:b", At: t0, Acked: &read, Kind: KindAsk},                     // read, never answered
		{From: "thread:a", To: "thread:c", At: t0, Kind: KindAsk},                                   // answered below
		{From: "thread:c", To: "thread:a", At: t0.Add(2 * time.Minute), Kind: "note"},               // the answer
		{From: "thread:b", To: "thread:a", At: t0.Add(-time.Minute), Kind: "note"},                  // before the ask: no answer
		{From: "thread:a", To: "thread:b", At: t0.Add(3 * time.Minute), Acked: &read, Kind: "note"}, // a note waits for nothing
	}
	p := Build(t0.Add(time.Hour), 24, letters, nil, nil)
	var ab, ac *Link
	for i := range p.Links {
		switch p.Links[i].From + ">" + p.Links[i].To {
		case "thread:a>thread:b":
			ab = &p.Links[i]
		case "thread:a>thread:c":
			ac = &p.Links[i]
		}
	}
	if ab == nil || ab.OpenAsks != 1 || ab.OldestAsk != t0.Format(time.RFC3339) || ab.Unacked != 0 {
		t.Fatalf("a>b %+v, want one open ask from %s and nothing unread", ab, t0.Format(time.RFC3339))
	}
	if ac == nil || ac.OpenAsks != 0 {
		t.Fatalf("a>c %+v, want its ask answered", ac)
	}
	seats := map[string]Seat{}
	for _, s := range p.Seats {
		seats[s.ID] = s
	}
	if seats["thread:a"].AsksWaiting != 1 || seats["thread:b"].AsksOwed != 1 || seats["thread:c"].AsksOwed != 0 {
		t.Fatalf("seats %+v", seats)
	}
}
