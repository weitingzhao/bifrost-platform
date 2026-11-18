package seenfacts

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every fact this package records is registered in
// bus/registry/ledger/types.yaml under foundation, and every body it declares
// is of one of those facts. (The foundation's other types are declared by
// other packages; the register's whole list is held to the database's by the
// store's TestEventTypesAreTheDatabases.)
//
// Origin: 2026-11-17. The event vocabulary moved out of the store by domain;
// the five *_seen facts are recorded only here, so they are declared here and
// registered under foundation (communication line 388fab51).
func TestSeenFactsAreRegistered(t *testing.T) {
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
	for _, typ := range seenTypes {
		if d, ok := registered[typ]; !ok || d != "foundation" {
			t.Errorf("%s is recorded here but registered as %q (listed %v)", typ, d, ok)
		}
	}
	for typ := range seenBodies {
		if !slices.Contains(seenTypes, typ) {
			t.Errorf("a body is declared for %s, which this package does not record", typ)
		}
	}
}
