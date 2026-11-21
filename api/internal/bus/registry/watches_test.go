package registry

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The watch rows and the registered bodies are the configuration a module's
// state needs to reach the bus (2026-11-20, the Owner: a module that joins
// or leaves must not mean a code change).
func TestTheWatchRowsParseAgainstTheRegister(t *testing.T) {
	rows, err := Watches()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows["rocket"]) == 0 {
		t.Fatal("rocket has no watch rows")
	}
}

func TestAWatchRowThatDoesNotFitItsTopicIsRefused(t *testing.T) {
	tenants, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for name, row := range map[string]string{
		"unregistered topic": "type: cluster.thing.changed",
		"state not in body":  "type: cluster.node.changed",
	} {
		data := `tenant: rocket
watches:
  - id: x
    version: v1
    resource: nodes
    ` + row + `
    stream: x
    subject: "{metadata.name}"
    subject_field: node
    state:
      - {name: colour, path: status.colour}
`
		if _, err := ParseWatches("rocket.yaml", []byte(data), tenants); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A type's body is declared once: in Go or in the register, never both
// (eventlog panics on a second declaration, at the process's start).
func TestRegisteredBodiesAreDeclaredToTheLedger(t *testing.T) {
	tenants, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, tn := range tenants {
		for typ, c := range tn.Channels {
			if len(c.Body) == 0 {
				continue
			}
			n++
			if !eventlog.IsEventType(typ) {
				t.Errorf("%s has a body in the register but the ledger does not know it", typ)
			}
		}
	}
	if n == 0 {
		t.Fatal("no registered body: the check reads nothing")
	}
}

func TestABadBodyIsRefusedByTheRegister(t *testing.T) {
	src, err := files.ReadFile("rocket.yaml")
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(src), "{name: ready, kind: str,", "{name: ready, kind: colour,", 1)
	if broken == string(src) {
		t.Fatal("the fixture's anchor moved")
	}
	if _, err := Parse("rocket.yaml", []byte(broken)); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("a body field of an unknown kind was accepted: %v", err)
	}
}
