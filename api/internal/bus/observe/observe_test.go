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
