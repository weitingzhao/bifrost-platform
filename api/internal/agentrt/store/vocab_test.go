package store_test

import (
	"regexp"
	"slices"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The vocabulary a reader may filter by is the database's, not only the
// types the store writes (review 350 A1).
func TestEventTypesAreTheDatabases(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	var def string
	ok(t, w.rt.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'`).Scan(&def))
	var db []string
	for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(def, -1) {
		db = append(db, m[1])
	}
	vocabulary, written := store.EventTypesForTest()
	slices.Sort(db)
	slices.Sort(vocabulary)
	if !slices.Equal(db, vocabulary) {
		t.Fatalf("the database's vocabulary %v\nis not the store's %v", db, vocabulary)
	}
	for _, typ := range written {
		if !store.IsEventType(typ) {
			t.Errorf("%s has a body but is not in the vocabulary", typ)
		}
	}
	for _, typ := range []string{"task_cancelled", "host_retired", "rule_expired", "attempt_progress"} {
		if !store.IsEventType(typ) {
			t.Errorf("%s, in the vocabulary with no body yet, is refused", typ)
		}
	}
	if store.IsEventType("not_a_type") {
		t.Error("a type outside the vocabulary is accepted")
	}
}
