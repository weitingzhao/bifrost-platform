package catalog

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
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
	Note    string            `json:"note,omitempty"`
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
	for _, r := range k.Rows {
		x := Seat{Seat: r.ID, Name: r.Name, Thread: thread(r.Session), AlsoDomains: r.AlsoDomainIDs, Kind: r.Kind,
			Harness: r.Harness, Mailbox: r.Mailbox, Charter: r.Charter, Started: r.Started, Ended: r.Ended}
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
