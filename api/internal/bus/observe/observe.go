// Package observe is the bus's picture of the team at work, for the Bus
// page's nerve map (the Owner, 2026-11-17, as the coordinator relayed it: the
// map is for watching how the seats work together, where they wait and
// where they stall; cards and approvals are decided elsewhere, in words).
// Everything is computed from the platform's own records when asked: the
// letters between seats (agentrt.message, written with their
// message_posted event), the seats' status reports, and the approval cards'
// bus events. What the platform does not record is named as unmeasured,
// not guessed.
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

// Letter is one message between actors.
type Letter struct {
	From, To string
	At       time.Time
	Acked    *time.Time
}

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
}

// Seat is one actor's readings.
type Seat struct {
	ID          string   `json:"id"`
	LettersOut  int      `json:"letters_out"`
	LettersIn   int      `json:"letters_in"`
	AckP50S     *float64 `json:"ack_p50_s,omitempty"`
	Unacked     int      `json:"unacked"`
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
	{What: "blocked by a permission", Why: "the guards (preflight, auto mode) refuse on the seat's own machine and write nothing to the platform"},
	{What: "backlog per subscription", Why: "comes with the bus health readings (each durable consumer's pending count)"},
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
		link Link
		ack  []float64
		open time.Time
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

// Querier is a pool, a connection or a transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ReadLetters reads the letters and the latest status report of each actor
// since the given time.
func ReadLetters(ctx context.Context, db Querier, since time.Time) ([]Letter, map[string]time.Time, error) {
	rows, err := db.Query(ctx, `SELECT from_actor_id, to_actor_id, created_at, acked_at FROM agentrt.message
	  WHERE created_at >= $1 ORDER BY created_at LIMIT 20000`, since)
	if err != nil {
		return nil, nil, fmt.Errorf("observe: letters: %w", err)
	}
	defer rows.Close()
	var letters []Letter
	status := map[string]time.Time{}
	for rows.Next() {
		var l Letter
		if err := rows.Scan(&l.From, &l.To, &l.At, &l.Acked); err != nil {
			return nil, nil, err
		}
		if l.To == StatusBoard {
			if l.At.After(status[l.From]) {
				status[l.From] = l.At
			}
			continue
		}
		letters = append(letters, l)
	}
	return letters, status, rows.Err()
}
