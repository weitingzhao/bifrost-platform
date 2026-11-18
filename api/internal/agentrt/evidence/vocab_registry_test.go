package evidence

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// The full CI record's type is registered in bus/registry/ledger/types.yaml
// under verification.
//
// Origin: 2026-11-17, migration 0031: the daily full CI's record left note for
// a type of its own (one owning domain per event type).
func TestFullCINotedIsRegistered(t *testing.T) {
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
		if lt.Type == fullCINotedType {
			if lt.Domain != "verification" {
				t.Fatalf("%s is registered under %q", fullCINotedType, lt.Domain)
			}
			return
		}
	}
	t.Fatalf("%s is not registered (%d types read)", fullCINotedType, len(doc.Types))
}
