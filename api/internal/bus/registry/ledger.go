package registry

import (
	_ "embed"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// The event ledger's own types (agentrt.event rows that are not bus topics),
// registered with the bus topics in this one register (the coordinator,
// 2026-11-17: one register, each domain registers its own events). Bodies,
// value checks and hooks stay with eventlog.Declare in the owning domain's
// package; this says only which types exist and whose they are.
//
//go:embed ledger/types.yaml
var ledgerYAML []byte

// LedgerType is one type of the event ledger and the domain whose work it records.
type LedgerType struct {
	Type   string `yaml:"type" json:"type"`
	Domain string `yaml:"domain" json:"domain"`
	Note   string `yaml:"note" json:"note,omitempty"`
}

// LedgerTypes is the ledger's own types, sorted by type; an entry listed
// twice or without a domain is an error.
func LedgerTypes() ([]LedgerType, error) {
	var doc struct {
		Types []LedgerType `yaml:"types"`
	}
	if err := yaml.Unmarshal(ledgerYAML, &doc); err != nil {
		return nil, fmt.Errorf("bus registry: ledger/types.yaml: %w", err)
	}
	seen := map[string]bool{}
	for _, t := range doc.Types {
		if t.Type == "" || t.Domain == "" {
			return nil, fmt.Errorf("bus registry: ledger/types.yaml: %q needs a type and a domain", t.Type)
		}
		if seen[t.Type] {
			return nil, fmt.Errorf("bus registry: ledger/types.yaml: %s listed twice", t.Type)
		}
		seen[t.Type] = true
	}
	sort.Slice(doc.Types, func(i, j int) bool { return doc.Types[i].Type < doc.Types[j].Type })
	return doc.Types, nil
}
