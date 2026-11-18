package actionrecord

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// The action trace's type is registered in bus/registry/ledger/types.yaml
// under foundation.
//
// Origin: 2026-11-17, migration 0032: the trace left note for a type of its
// own (one owning domain per event type).
func TestTracedTypeIsRegistered(t *testing.T) {
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
		if lt.Type == tracedType {
			if lt.Domain != "foundation" {
				t.Fatalf("%s is registered under %q", tracedType, lt.Domain)
			}
			return
		}
	}
	t.Fatalf("%s is not registered (%d types read)", tracedType, len(doc.Types))
}
