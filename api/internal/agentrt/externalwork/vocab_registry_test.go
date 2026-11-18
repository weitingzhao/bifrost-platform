package externalwork

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// An external work's record type is registered in
// bus/registry/ledger/types.yaml under lineage.
//
// Origin: 2026-11-17, migration 0032: the record left note for a type of its
// own (one owning domain per event type).
func TestNotedTypeIsRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "bus", "registry", "ledger", "types.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Types []struct{ Type, Domain string } `yaml:"types"`
	}
	if err = yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, lt := range doc.Types {
		if lt.Type == notedType {
			if lt.Domain != "lineage" {
				t.Fatalf("%s is registered under %q", notedType, lt.Domain)
			}
			return
		}
	}
	t.Fatalf("%s is not registered (%d types read)", notedType, len(doc.Types))
}
