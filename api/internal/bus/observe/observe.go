// Package observe is the bus's picture of the team at work, for the Bus
// page's nerve map (the Owner, 2026-11-17, as the coordinator relayed it: the
// map is for watching how the seats work together, where they wait and
// where they stall; cards and approvals are decided elsewhere, in words).
// Everything is computed from the platform's own records when asked: the
// letters between seats and the seats' status reports (coordination's
// messages, read through coord.ReadLetters, which the server hands the
// route), and the approval cards' bus events. What the platform does not
// record is named as unmeasured, not guessed.
package observe

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// StatusBoard is where seats send their status reports (coord.StatusBoard);
// a letter to it is a report, not a conversation.
const StatusBoard = "controller"

// Letter is one message between actors; Kind is the message's kind (ask,
// answer, help, handover, note, wake).
type Letter struct {
	From, To string
	At       time.Time
	Acked    *time.Time
	Kind     string
}

// KindAsk is the kind of a letter that waits for an answer.
const KindAsk = "ask"

// Card is one approval card's bus events: when it was created and decided.
type Card struct {
	ID               string
	Created, Decided time.Time
}

// Link is who wrote to whom, how often, and how fast it was taken.
type Link struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	Kind       string   `json:"kind"` // letter or card
	Count      int      `json:"count"`
	AckP50S    *float64 `json:"ack_p50_s,omitempty"`
	Unacked    int      `json:"unacked"`
	OldestOpen string   `json:"oldest_open,omitempty"`
	// OpenAsks are the asks on this link that nobody answered: no letter
	// came back from To to From after them (read or not). OldestAsk is when
	// the oldest of them was sent. 2026-11-21 (the main line "seen and
	// felt", law 69): which line waits on whose answer was not discoverable;
	// an unread letter (Unacked) said nothing of a read one left unanswered.
	OpenAsks  int    `json:"open_asks,omitempty"`
	OldestAsk string `json:"oldest_ask,omitempty"`
}

// Seat is one actor's readings.
type Seat struct {
	ID         string   `json:"id"`
	LettersOut int      `json:"letters_out"`
	LettersIn  int      `json:"letters_in"`
	AckP50S    *float64 `json:"ack_p50_s,omitempty"`
	Unacked    int      `json:"unacked"`
	// AsksWaiting are this seat's asks nobody answered yet; AsksOwed the
	// asks to it it has not answered (Link.OpenAsks).
	AsksWaiting int      `json:"asks_waiting,omitempty"`
	AsksOwed    int      `json:"asks_owed,omitempty"`
	LastStatus  string   `json:"last_status,omitempty"`
	CardsOpen   int      `json:"cards_open,omitempty"`
	CardWaitP50 *float64 `json:"card_wait_p50_s,omitempty"`
}

// Unmeasured is a reading the page would show that the platform does not record.
type Unmeasured struct {
	What string `json:"what"`
	Why  string `json:"why"`
}

// Picture is the team at work over the last Hours.
type Picture struct {
	Hours       int          `json:"hours"`
	GeneratedAt string       `json:"generated_at"`
	Seats       []Seat       `json:"seats"`
	Links       []Link       `json:"links"`
	Unmeasured  []Unmeasured `json:"unmeasured"`
	// Rate is how many bus events the last hour carried (the live map's
	// "how busy is it now"), set by the caller from the ledger.
	Rate *Rate `json:"rate,omitempty"`
	// Listening is who hears each topic and what follows, over the last day.
	Listening *Listening `json:"listening,omitempty"`
}

// Rate is the bus's events over a recent window.
type Rate struct {
	Minutes   int            `json:"minutes"`
	Events    int            `json:"events"`
	PerMinute float64        `json:"per_minute"`
	ByType    map[string]int `json:"by_type"`
}

// NewRate counts events by type over minutes.
func NewRate(minutes int, types []string) *Rate {
	r := &Rate{Minutes: minutes, ByType: map[string]int{}}
	for _, t := range types {
		r.ByType[t]++
		r.Events++
	}
	if minutes > 0 {
		r.PerMinute = float64(int64(float64(r.Events)/float64(minutes)*100+0.5)) / 100
	}
	return r
}

// Owner is the actor the cards wait on.
const Owner = "owner"

// unmeasured are the readings asked for (the coordinator, 14:5x) that no record holds.
var unmeasured = []Unmeasured{
	{What: "blocked by auto mode", Why: "auto mode refuses on the seat's own machine and writes nothing to the platform; preflight's refusals are on the bus as guard.block.recorded, sent by each machine's doorbell"},
	{What: "backlog per subscription", Why: "measured in Prometheus (bifrost_bus_consumer_pending and _ack_pending, by consumer, every minute) but not drawn on this page yet"},
	{What: "letters between sessions", Why: "Claude Desktop's session messages do not pass through the platform; only platform letters are counted"},
}

func p50(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	sort.Float64s(xs)
	v := xs[(len(xs)-1)/2]
	v = float64(int64(v*10+0.5)) / 10
	return &v
}

// Build computes the picture from the records.
func Build(now time.Time, hours int, letters []Letter, status map[string]time.Time, cards []Card) Picture {
	type acc struct {
		link      Link
		ack       []float64
		open      time.Time
		oldestAsk time.Time
	}
	links := map[[2]string]*acc{}
	seats := map[string]*Seat{}
	seatAck := map[string][]float64{}
	seat := func(id string) *Seat {
		s, ok := seats[id]
		if !ok {
			s = &Seat{ID: id}
			seats[id] = s
		}
		return s
	}
	for _, l := range letters {
		if l.To == StatusBoard {
			continue
		}
		k := [2]string{l.From, l.To}
		a, ok := links[k]
		if !ok {
			a = &acc{link: Link{From: l.From, To: l.To, Kind: "letter"}}
			links[k] = a
		}
		a.link.Count++
		seat(l.From).LettersOut++
		to := seat(l.To)
		to.LettersIn++
		if l.Acked != nil {
			s := l.Acked.Sub(l.At).Seconds()
			a.ack = append(a.ack, s)
			seatAck[l.To] = append(seatAck[l.To], s)
		} else {
			a.link.Unacked++
			to.Unacked++
			if a.open.IsZero() || l.At.Before(a.open) {
				a.open = l.At
			}
		}
	}
	// An ask is answered by any later letter back from its recipient to its
	// sender (the reply_to in a body is not read: bodies are artifacts).
	for i, l := range letters {
		if l.Kind != KindAsk || l.To == StatusBoard {
			continue
		}
		answered := false
		for _, r := range letters[i+1:] {
			if r.From == l.To && r.To == l.From && r.At.After(l.At) {
				answered = true
				break
			}
		}
		if answered {
			continue
		}
		a := links[[2]string{l.From, l.To}]
		a.link.OpenAsks++
		if a.oldestAsk.IsZero() || l.At.Before(a.oldestAsk) {
			a.oldestAsk = l.At
		}
		seat(l.From).AsksWaiting++
		seat(l.To).AsksOwed++
	}
	for id, at := range status {
		seat(id).LastStatus = at.UTC().Format(time.RFC3339)
	}
	if len(cards) > 0 {
		o := seat(Owner)
		var waits []float64
		cl := Link{From: "seats", To: Owner, Kind: "card"}
		var open time.Time
		for _, c := range cards {
			cl.Count++
			if c.Decided.IsZero() {
				o.CardsOpen++
				cl.Unacked++
				if open.IsZero() || c.Created.Before(open) {
					open = c.Created
				}
				continue
			}
			waits = append(waits, c.Decided.Sub(c.Created).Seconds())
		}
		o.CardWaitP50 = p50(waits)
		cl.AckP50S = p50(append([]float64(nil), waits...))
		if !open.IsZero() {
			cl.OldestOpen = open.UTC().Format(time.RFC3339)
		}
		links[[2]string{"seats", Owner}] = &acc{link: cl}
	}
	p := Picture{Hours: hours, GeneratedAt: now.UTC().Format(time.RFC3339), Unmeasured: unmeasured}
	for _, a := range links {
		if a.link.Kind == "letter" {
			a.link.AckP50S = p50(a.ack)
			if !a.open.IsZero() {
				a.link.OldestOpen = a.open.UTC().Format(time.RFC3339)
			}
			if !a.oldestAsk.IsZero() {
				a.link.OldestAsk = a.oldestAsk.UTC().Format(time.RFC3339)
			}
		}
		p.Links = append(p.Links, a.link)
	}
	for id, s := range seats {
		if s.AckP50S == nil {
			s.AckP50S = p50(seatAck[id])
		}
		p.Seats = append(p.Seats, *s)
	}
	sort.Slice(p.Links, func(i, j int) bool {
		if p.Links[i].Count != p.Links[j].Count {
			return p.Links[i].Count > p.Links[j].Count
		}
		return p.Links[i].From+p.Links[i].To < p.Links[j].From+p.Links[j].To
	})
	sort.Slice(p.Seats, func(i, j int) bool { return p.Seats[i].ID < p.Seats[j].ID })
	return p
}

// Querier is a pool, a connection or a transaction: what the letters are
// read over (Handler.Letters).
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// TopicListen is whether anyone listens to a topic: what it published over
// the window, who may read it (declared consumers and subscription rows),
// how many of its events someone took (a receipt from anyone but the
// Console's own view), and how many led to an event that names it as its
// cause.
type TopicListen struct {
	Type      string   `json:"type"`
	Published int      `json:"published"`
	Readers   []string `json:"readers"`
	Heard     int      `json:"heard"`
	Followed  int      `json:"followed"`
	// DisplayOnly: published for watching (registry display_only); left out
	// of the shares and never deaf.
	DisplayOnly bool `json:"display_only,omitempty"`
}

// Listening is the bus's loop over the window: who hears, what follows.
type Listening struct {
	Hours  int           `json:"hours"`
	Topics []TopicListen `json:"topics"`
	// Events heard by someone, and events that something followed, as shares
	// of all published but the display-only topics' (Counted).
	HeardShare  *float64 `json:"heard_share,omitempty"`
	ClosedShare *float64 `json:"closed_share,omitempty"`
	// CausedShare is the share of events that name the event they answer (their cause).
	CausedShare *float64 `json:"caused_share,omitempty"`
	// Counted is the shares' denominator; Watched the display-only events left out of it.
	Counted int `json:"counted"`
	Watched int `json:"watched"`
	// Why a share is missing or partial (the receipt stream not read, a scan cut short).
	Notes []string `json:"notes,omitempty"`
}

// ListenEvent is one bus event of the window: its id (ev-<n>), type and cause (0, none).
type ListenEvent struct {
	ID    string
	Type  string
	Cause int64
	N     int64
}

// BuildListening counts, per topic, what was published, heard and followed.
// own is the receipt consumer that is only the page looking (not a listener).
// display is the display-only topics: counted per topic, left out of the shares.
func BuildListening(hours int, events []ListenEvent, receipts map[string][]string, receiptsRead bool, readers map[string][]string, own string, display map[string]bool) Listening {
	l := Listening{Hours: hours}
	causes := map[int64]bool{}
	caused := 0
	for _, e := range events {
		if e.Cause != 0 {
			causes[e.Cause] = true
			caused++
		}
	}
	by := map[string]*TopicListen{}
	for t, rs := range readers {
		by[t] = &TopicListen{Type: t, Readers: rs, DisplayOnly: display[t]}
	}
	heard, followed := 0, 0
	for _, e := range events {
		t, ok := by[e.Type]
		if !ok {
			t = &TopicListen{Type: e.Type, DisplayOnly: display[e.Type]}
			by[e.Type] = t
		}
		t.Published++
		counted := !display[e.Type]
		if counted {
			l.Counted++
		} else {
			l.Watched++
		}
		for _, c := range receipts[e.ID] {
			if c != own {
				t.Heard++
				if counted {
					heard++
				}
				break
			}
		}
		if causes[e.N] {
			t.Followed++
			if counted {
				followed++
			}
		}
	}
	for _, t := range by {
		l.Topics = append(l.Topics, *t)
	}
	sort.Slice(l.Topics, func(i, j int) bool { return l.Topics[i].Type < l.Topics[j].Type })
	if l.Counted > 0 {
		f := float64(followed) / float64(l.Counted)
		f = float64(int64(f*1000+0.5)) / 1000
		l.ClosedShare = &f
		c := float64(caused) / float64(len(events))
		c = float64(int64(c*1000+0.5)) / 1000
		l.CausedShare = &c
		if receiptsRead {
			h := float64(heard) / float64(l.Counted)
			h = float64(int64(h*1000+0.5)) / 1000
			l.HeardShare = &h
		}
	}
	if !receiptsRead {
		l.Notes = append(l.Notes, "receipts not read: who heard each event is unknown")
	}
	if l.Watched > 0 {
		l.Notes = append(l.Notes, fmt.Sprintf("%d events of display-only topics are left out of heard and followed: published to be watched, not acted on", l.Watched))
	}
	if followed == 0 && len(events) > 0 {
		l.Notes = append(l.Notes, "no consumer records the event it acted on (causeid) yet, so no loop can be seen closing")
	}
	return l
}
