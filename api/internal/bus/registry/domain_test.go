package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
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
