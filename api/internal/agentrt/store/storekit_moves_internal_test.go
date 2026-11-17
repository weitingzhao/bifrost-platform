//bifrost:owner plan

package store

import (
	"reflect"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

// Origin: 2026-11-17, the mission fixtures left this package for storekit,
// which may not import store (the foundation wall) and so carries a copy of
// the table of mission moves. A state added to one and not the other would
// let a fixture move a mission the vocabulary refuses, or refuse one it
// takes: the two are held equal here.
func TestTheKitMovesMissionsAsTheVocabularyDoes(t *testing.T) {
	t.Parallel()
	if got := storekit.MissionMoves(); !reflect.DeepEqual(got, missionMoves) {
		t.Fatalf("storekit moves missions by %v, the vocabulary by %v", got, missionMoves)
	}
	// The positive control: the comparison sees a difference.
	changed := storekit.MissionMoves()
	changed["proposed"] = append(changed["proposed"], "elsewhere")
	if reflect.DeepEqual(changed, missionMoves) {
		t.Fatal("a changed table compared equal")
	}
}
