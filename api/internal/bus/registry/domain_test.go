package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Origin: 2026-11-17, the Bus page's map zooms from domains to their parts
// (the Owner via the coordinator): which domain an event tells of is read
// from here, so every channel names one, and it must be a domain of
// config/domain-ownership.json (a name not there would be a domain the map
// invents).
func TestEveryChannelNamesAnOwnedDomain(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "config", "domain-ownership.json"))
	if err != nil {
		t.Fatal(err)
	}
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
}

// Origin: 2026-11-17, the ledger's types join the bus topics in one register
// (the coordinator; split agreed with the foundation line the same day).
// Every ledger type names a domain of config/domain-ownership.json; and the
// ledger's types with the bus topics are exactly the closed vocabulary the
// latest migration gives agentrt.event (event_type_closed): a type the
// database allows but nobody registered, or one registered that it refuses,
// fails here.
func TestLedgerTypesAndTopicsAreTheClosedVocabulary(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "config", "domain-ownership.json"))
	if err != nil {
		t.Fatal(err)
	}
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
	allowed := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z_.]+)'`).FindAllStringSubmatch(clause, -1) {
		allowed[m[1]] = true
	}
	if len(allowed) < 50 {
		t.Fatalf("read %d types from event_type_closed: the parse is wrong", len(allowed))
	}
	for typ := range allowed {
		if !registered[typ] {
			t.Errorf("%s is allowed by the database but not registered", typ)
		}
	}
	for typ := range registered {
		if !allowed[typ] {
			t.Errorf("%s is registered but the database refuses it", typ)
		}
	}
}
