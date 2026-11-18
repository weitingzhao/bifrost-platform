package loopalert

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// The controller loops' two types are registered in
// bus/registry/ledger/types.yaml under foundation, and every body declared
// here is of one of them.
//
// Origin: 2026-11-17, migration 0029: the alert and the recovery left
// policy_alert and note for types of their own (one owning domain per type).
func TestLoopTypesAreRegistered(t *testing.T) {
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
	registered := map[string]string{}
	for _, lt := range doc.Types {
		registered[lt.Type] = lt.Domain
	}
	if len(registered) == 0 {
		t.Fatal("the register lists no type: the comparison below shows nothing")
	}
	for _, typ := range loopTypes {
		if d, ok := registered[typ]; !ok || d != "foundation" {
			t.Errorf("%s is declared here but registered as %q (listed %v)", typ, d, ok)
		}
	}
	for typ := range loopBodies {
		if !slices.Contains(loopTypes, typ) {
			t.Errorf("a body is declared for %s, which is not one of this package's types", typ)
		}
	}
}
