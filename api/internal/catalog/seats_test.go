package catalog

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

// 2026-11-25, UI / Engine for Agents and the City: the engine read a
// hand-copied roster that went stale, and no read route said which thread
// sits which seat of which domain. The roster the scan keeps is served with
// the domains' names; an unquoted date in the roster reads as written. The
// control: before any scan the route answers an empty list with a note.
func TestSeatsServeTheKeptRosterWithDomainNames(t *testing.T) {
	now := time.Date(2026, 11, 25, 3, 0, 0, 0, time.UTC)
	s := &Scanner{Kept: memKept{}, Now: func() time.Time { return now }}
	var empty SeatsDoc
	w := httptest.NewRecorder()
	SeatsRoute{Scanner: s}.ServeHTTP(w, httptest.NewRequest("GET", "/catalog/seats", nil))
	if json.Unmarshal(w.Body.Bytes(), &empty) != nil || len(empty.Seats) != 0 || empty.Note == "" {
		t.Fatalf("before a scan: %s", w.Body.String())
	}
	ix, _ := json.Marshal(Index{Classes: []Class{{Class: "seat", Sources: []string{"infra:c/seats.yaml"}, Check: CheckScanned}}})
	_ = s.kept().Write(keptIndex, ix)
	tree := Tree{Commit: "c1", Files: []string{"c/seats.yaml", "c/a.md"}, Read: func(string) ([]byte, bool, error) {
		return []byte(`seats:
  - {id: 0-2-catalog, charter: a.md, domain_id: foundation, kind: owner, harness: claude, session: local_76b5, mailbox: registered, also_domain_ids: [plan]}
  - {id: 10-cockpit, charter: b.md, domain_id: null, kind: owner, harness: claude, session: local_d303, mailbox: registered}
temporary:
  - {id: visit, name: v, domain_id: foundation, kind: temporary, harness: codex, session: codex-01, invited_by: x, started: 2026-11-20, ended: null}
`), true, nil
	}}
	s.keepSeats("infra", tree)
	doc := s.seatsDoc()
	doc.Domains = map[string]string{"foundation": "Foundation"}
	for i := range doc.Seats {
		doc.Seats[i].DomainName = doc.Domains[doc.Seats[i].Domain]
	}
	if doc.Commit != "c1" || doc.Path != "c/seats.yaml" || !doc.At.Equal(now) || len(doc.Seats) != 3 {
		t.Fatalf("%+v", doc)
	}
	a, b, v := doc.Seats[0], doc.Seats[1], doc.Seats[2]
	if a.Thread != "thread:local_76b5" || a.Domain != "foundation" || a.DomainName != "Foundation" || len(a.AlsoDomains) != 1 || a.Mailbox != "registered" {
		t.Errorf("owner seat %+v", a)
	}
	if b.Domain != "" || b.Thread != "thread:local_d303" {
		t.Errorf("a seat of no domain %+v", b)
	}
	if v.Thread != "codex-01" || v.Kind != "temporary" || v.Started != "2026-11-20" || v.Ended != "" {
		t.Errorf("a temporary visit %+v", v)
	}
}
