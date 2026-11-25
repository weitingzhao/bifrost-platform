package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Origin: the Bus page's map zooms from domains to their parts (ISSUE-53):
// which domain an event tells of is read
// from here, so every channel names one, and it must be a domain of
// config/domain-ownership.json (a name not there would be a domain the map
// invents).
func TestEveryChannelNamesAnOwnedDomain(t *testing.T) {
	b := ownershipRaw(t)
	var own struct {
		Domains []struct {
			ID   string `json:"id"`
			Tier *int   `json:"tier"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(b, &own); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, d := range own.Domains {
		known[d.ID] = true
		// ISSUE-53: the map stands each
		// domain on the floor of its tier (base at the bottom, the Owner on
		// top), read from this table and not placed by hand.
		if d.Tier == nil || *d.Tier < 0 {
			t.Errorf("domain %s declares no tier (0 = base, higher = further from it)", d.ID)
		}
	}
	if len(known) == 0 {
		t.Fatal("no domains read")
	}
	all, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, tn := range all {
		for typ, ch := range tn.Channels {
			n++
			if !known[ch.Domain] {
				t.Errorf("%s/%s: domain %q is not in config/domain-ownership.json", tn.Name, typ, ch.Domain)
			}
		}
	}
	if n == 0 {
		t.Fatal("no channels")
	}
	// 2026-11-17: the map draws an arc to a durable client's domain and to a
	// row's target domain; neither may be a domain the table does not have.
	subs, err := LoadSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range subs {
		for _, c := range st.Clients {
			if c.Kind == "durable" && !known[c.Domain] {
				t.Errorf("client %s: domain %q is not in config/domain-ownership.json", c.Name, c.Domain)
			}
		}
		for _, r := range st.Rows {
			if d := r.TargetDomain(); d != "" && !known[d] {
				t.Errorf("row %s: target domain %q is not in config/domain-ownership.json", r.ID, d)
			}
		}
	}
}

// Origin: 2026-11-17, the ledger's types join the bus topics in one register
// (the coordinator; split agreed with the foundation line the same day).
// Every ledger type names a domain of config/domain-ownership.json; and the
// ledger's types with the bus topics are exactly the closed vocabulary the
// latest migration gives agentrt.event (event_type_closed): a type the
// database allows but nobody registered, or one registered that it refuses,
// fails here.
func TestLedgerTypesAndTopicsAreTheClosedVocabulary(t *testing.T) {
	b := ownershipRaw(t)
	var own struct {
		Domains []struct {
			ID string `json:"id"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(b, &own); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, d := range own.Domains {
		known[d.ID] = true
	}
	ledger, err := LedgerTypes()
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, l := range ledger {
		registered[l.Type] = true
		if !known[l.Domain] {
			t.Errorf("ledger %s: domain %q is not in config/domain-ownership.json", l.Type, l.Domain)
		}
	}
	all, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for typ := range all["rocket"].Channels {
		if registered[typ] {
			t.Errorf("%s is both a ledger type and a bus topic", typ)
		}
		registered[typ] = true
	}

	dir := filepath.Join("..", "..", "agentrt", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var latest string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "event_type_closed CHECK") {
			latest = string(body) // names sort in order: the last one wins
		}
	}
	if latest == "" {
		t.Fatal("no migration defines event_type_closed")
	}
	clause := latest[strings.LastIndex(latest, "event_type_closed CHECK"):]
	clause = clause[:strings.Index(clause, ";")]
	// Since 0041 the ledger's own types are listed one by one and the bus
	// topics are allowed by their shape (event_type ~ '...'): the register is
	// their closed set, so a new topic needs no migration (ISSUE-34).
	shapeAt := strings.Index(clause, "event_type ~ '")
	if shapeAt < 0 {
		t.Fatal("event_type_closed has no shape for the bus topics")
	}
	listed := clause[:shapeAt]
	shape := clause[shapeAt+len("event_type ~ '"):]
	shape = shape[:strings.Index(shape, "'")]
	topic, err := regexp.Compile(shape)
	if err != nil {
		t.Fatalf("the topics' shape %q: %v", shape, err)
	}
	allowed := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z_.]+)'`).FindAllStringSubmatch(listed, -1) {
		allowed[m[1]] = true
	}
	if len(allowed) < 50 {
		t.Fatalf("read %d types from event_type_closed: the parse is wrong", len(allowed))
	}
	ledgerTypes := map[string]bool{}
	for _, l := range ledger {
		ledgerTypes[l.Type] = true
		if !allowed[l.Type] {
			t.Errorf("ledger type %s is refused by the database", l.Type)
		}
	}
	for typ := range allowed {
		if !ledgerTypes[typ] {
			t.Errorf("%s is listed by the database but is not a ledger type", typ)
		}
	}
	for typ := range all["rocket"].Channels {
		if !topic.MatchString(typ) {
			t.Errorf("bus topic %s does not have the shape the database allows (%s)", typ, shape)
		}
	}
	// Positive control: a ledger type does not pass as a topic.
	if topic.MatchString("plan_observation") {
		t.Errorf("the topics' shape %s also takes ledger types", shape)
	}
}
