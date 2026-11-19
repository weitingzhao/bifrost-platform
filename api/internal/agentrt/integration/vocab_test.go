//bifrost:owner foundation

package integration_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	// owner_wait_opened and owner_wait_closed are declared by questioncards
	// (2026-11-17, store cut O2): link it, so TestEventFieldValues still
	// holds their values and every type in the closed list has its body here.
	_ "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questioncards"
	// action_traced is declared by actionrecord (2026-11-17, migration 0032):
	// link it for the same reason.
	_ "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	// home_layout_saved is declared by homelayout (2026-11-18, migration 0034): the same.
	_ "github.com/weitingzhao/bifrost-platform/api/internal/homelayout"
)

// The vocabulary a reader may filter by is the database's, not only the
// types the store writes (review 350 A1). It sits here because every domain
// that declares an event type has to be linked into the binary (2026-11-18).
func TestEventTypesAreTheDatabases(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	var def string
	ok(t, w.rt.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'`).Scan(&def))
	var db []string
	for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(def, -1) {
		db = append(db, m[1])
	}
	vocabulary, written := eventlog.EventTypes()
	// A bus type (a dotted name, bus/registry/rocket.yaml) is declared by its
	// publisher's package and lands in this binary only when a test imports
	// that package (evidence declares ci.verdict.changed, 2026-11-16); its
	// body is checked by the publisher's own tests, and the regexp above
	// reads only the store's names. Compare the store's names.
	vocabulary = slices.DeleteFunc(vocabulary, func(t string) bool { return strings.Contains(t, ".") })
	slices.Sort(db)
	slices.Sort(vocabulary)
	if !slices.Equal(db, vocabulary) {
		t.Fatalf("the database's vocabulary %v\nis not the store's %v", db, vocabulary)
	}
	for _, typ := range written {
		if !eventlog.IsEventType(typ) {
			t.Errorf("%s has a body but is not in the vocabulary", typ)
		}
	}
	for _, typ := range []string{"task_cancelled", "host_retired", "rule_expired", "attempt_progress"} {
		if !eventlog.IsEventType(typ) {
			t.Errorf("%s, in the vocabulary with no body yet, is refused", typ)
		}
	}
	if eventlog.IsEventType("not_a_type") {
		t.Error("a type outside the vocabulary is accepted")
	}
}
