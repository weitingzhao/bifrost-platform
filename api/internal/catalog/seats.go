package catalog

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Which thread sits which seat of which domain (2026-11-25, UI / Engine for
// Agents and the City: the engine read a hand-copied roster that went stale;
// the ownership table says who owns code and routes, not who sits). The
// roster is the workshop's (charters/seats.yaml); the map already keeps it
// each round for the seat check, and this serves that kept copy, joined with
// the domains' names from the table. Read-only.

// Seat is one row of the roster as served.
type Seat struct {
	Seat        string   `json:"seat"`
	Name        string   `json:"name,omitempty"`
	Thread      string   `json:"thread,omitempty"`
	Domain      string   `json:"domain,omitempty"`
	DomainName  string   `json:"domain_name,omitempty"`
	AlsoDomains []string `json:"also_domains,omitempty"`
	Kind        string   `json:"kind"`
	Harness     string   `json:"harness,omitempty"`
	Mailbox     string   `json:"mailbox,omitempty"`
	Charter     string   `json:"charter,omitempty"`
	Started     string   `json:"started,omitempty"`
	Ended       string   `json:"ended,omitempty"`
	Account     string   `json:"account,omitempty"`
	RunKey      string   `json:"run_key,omitempty"`
	InvitedBy   string   `json:"invited_by,omitempty"`
	Note        string   `json:"note,omitempty"`
}

// SeatsDoc is GET /catalog/seats.
type SeatsDoc struct {
	Version int `json:"version"`
	// Repo, Commit and Path are where the roster was read; At when.
	Repo   string    `json:"repo,omitempty"`
	Commit string    `json:"commit,omitempty"`
	Path   string    `json:"path,omitempty"`
	At     time.Time `json:"at,omitempty"`
	Seats  []Seat    `json:"seats"`
	// Domains are the table's domains, id to name.
	Domains map[string]string `json:"domains"`
	// Roster is the roster's sections as written (seats, temporary,
	// outside_seats, humans, accounts, every field), and Cards each seat's
	// identity card beside it (cards/<seat>.yaml) as written, by file name
	// (Crew 2026-11-23: one read for the City and Agents, with the cards).
	Roster map[string]any `json:"roster,omitempty"`
	Cards  map[string]any `json:"cards,omitempty"`
	Note   string         `json:"note,omitempty"`
}

const keptRosterRaw = "seats-as-written"

type rosterAsWritten struct {
	Roster map[string]any `json:"roster"`
	Cards  map[string]any `json:"cards"`
}

// RosterSections are the roster's sections served as written.
var RosterSections = []string{"seats", "temporary", "outside_seats", "humans", "accounts"}

// keepRosterAsWritten keeps the roster's sections and the cards beside it as
// written; a card that does not parse is left out and logged.
func (s *Scanner) keepRosterAsWritten(repo string, t Tree, file string, raw []byte) {
	var all map[string]any
	if err := yaml.Unmarshal(raw, &all); err != nil {
		return
	}
	k := rosterAsWritten{Roster: map[string]any{}, Cards: map[string]any{}}
	for _, sec := range RosterSections {
		if v, ok := all[sec]; ok && v != nil {
			k.Roster[sec] = v
		} else {
			k.Roster[sec] = []any{}
		}
	}
	dir := path.Join(path.Dir(file), "cards")
	var cards []string
	for _, f := range t.Files {
		if path.Dir(f) == dir && strings.HasSuffix(f, ".yaml") {
			cards = append(cards, f)
		}
	}
	sort.Strings(cards)
	for _, f := range cards {
		b, ok, err := t.Read(f)
		if err != nil || !ok {
			continue
		}
		var card any
		if err := yaml.Unmarshal(b, &card); err != nil {
			slog.Warn("catalog: seat card", "repo", repo, "file", f, "err", err)
			continue
		}
		k.Cards[path.Base(f)] = card
	}
	if out, err := json.Marshal(k); err == nil {
		if err := s.kept().Write(keptRosterRaw, out); err != nil {
			slog.Warn("catalog: keep roster as written", "err", err)
		}
	}
}

const keptSeatsAt = "seats-at"

// SeatsRoute is GET /catalog/seats: the roster last kept.
type SeatsRoute struct{ Scanner *Scanner }

func (rt SeatsRoute) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, rt.Scanner.seatsDoc())
}

func (s *Scanner) seatsDoc() SeatsDoc {
	doc := SeatsDoc{Version: Version, Seats: []Seat{}, Domains: map[string]string{}}
	var domains []contractDomain
	if k, ok := s.keptSections(); ok {
		_ = json.Unmarshal(k.Domains, &domains)
	}
	for _, d := range domains {
		doc.Domains[d.ID] = d.Name
	}
	raw, err := s.kept().Read(keptSeats)
	if err != nil || raw == nil {
		doc.Note = "no scan has kept the seat roster yet"
		return doc
	}
	var k keptSeatRoster
	if json.Unmarshal(raw, &k) != nil {
		doc.Note = "the kept seat roster does not read"
		return doc
	}
	doc.Repo, doc.Commit, doc.Path = k.Repo, k.Commit, k.Path
	if at, err := s.kept().Read(keptSeatsAt); err == nil && at != nil {
		_ = json.Unmarshal(at, &doc.At)
	}
	if raw, err := s.kept().Read(keptRosterRaw); err == nil && raw != nil {
		var w rosterAsWritten
		if json.Unmarshal(raw, &w) == nil {
			doc.Roster, doc.Cards = w.Roster, w.Cards
		}
	}
	for _, r := range append(append([]seatRow{}, k.Rows...), k.Outside...) {
		x := Seat{Seat: r.ID, Name: r.Name, Thread: thread(r.Session), AlsoDomains: r.AlsoDomainIDs, Kind: r.Kind,
			Harness: r.Harness, Mailbox: r.Mailbox, Charter: r.Charter, Started: r.Started, Ended: r.Ended,
			Account: r.Account, RunKey: r.RunKey, InvitedBy: r.InvitedBy, Note: r.Note}
		if r.DomainID != nil {
			x.Domain, x.DomainName = *r.DomainID, doc.Domains[*r.DomainID]
		}
		doc.Seats = append(doc.Seats, x)
	}
	return doc
}

// thread is a roster session as the mailbox names threads ("thread:local_…");
// a session of another harness (codex-…) is left as written.
func thread(session string) string {
	if strings.HasPrefix(session, "local_") {
		return "thread:" + session
	}
	return session
}
