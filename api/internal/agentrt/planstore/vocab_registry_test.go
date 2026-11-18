package planstore

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// registeredLedger reads the register's ledger types (bus/registry/ledger/
// types.yaml) as type -> domain. The file is the register itself; reading it
// keeps the test off the registry package's internals.
func registeredLedger(t *testing.T) map[string]string {
	t.Helper()
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
	out := map[string]string{}
	for _, lt := range doc.Types {
		out[lt.Type] = lt.Domain
	}
	return out
}

// The plan's event types are the register's plan types, no more and no
// fewer: every type this package declares is listed in
// bus/registry/ledger/types.yaml under plan, and every type listed there under
// plan is declared here. Every declared body is of a declared type.
//
// Origin: 2026-11-17. The event vocabulary moved out of the store by domain
// (agreed with the communication line: one register, each domain declares its
// own and checks itself against it); this is the plan domain's check.
func TestPlanVocabularyIsRegistered(t *testing.T) {
	ledger := registeredLedger(t)
	var registered []string
	for typ, domain := range ledger {
		if domain == "plan" {
			registered = append(registered, typ)
		}
	}
	for _, typ := range planTypes {
		if d, ok := ledger[typ]; !ok || d != "plan" {
			t.Errorf("%s is declared by the plan but registered as %q (listed %v): register it under plan or declare it in its domain", typ, d, ok)
		}
	}
	for _, typ := range registered {
		if !slices.Contains(planTypes, typ) {
			t.Errorf("%s is registered under plan but the plan does not declare it", typ)
		}
	}
	for typ := range planBodies {
		if !slices.Contains(planTypes, typ) {
			t.Errorf("a body is declared for %s, which is not one of the plan's types", typ)
		}
	}
	if len(registered) == 0 {
		t.Fatal("the register lists no plan type: the comparison above shows nothing")
	}
}
