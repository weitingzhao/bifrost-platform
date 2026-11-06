package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.7-5 slice 4: both STG evidence reads (pair by pair, and one
// workspace per pass) ask through releaseEvidence. An ordinary member is
// asked with its root and A; an InMain member's recorded H is asked without
// them (O-3). R is always the member's m.R; the answer and the error are
// passed on as read.
func TestReleaseEvidenceAsksWithTheMembersRootAndA(t *testing.T) {
	t.Parallel()
	p, r, a := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	m := store.StgMemberKey{Root: "t_01ARZ3NDEKTSV4RRFFQ69G5FAV", Op: "land:t_01ARZ3NDEKTSV4RRFFQ69G5FAV:1", R: r}
	ordinary := store.StgState{Legacy: landing.State{Operations: map[string]landing.Operation{m.Op: {A: a}}}}
	inMain := ordinary
	inMain.InMain = map[string]string{m.Op: r}
	for _, c := range []struct {
		name          string
		state         store.StgState
		wantRoot, wnA string
	}{
		{"ordinary", ordinary, m.Root, a},
		{"in main", inMain, "", ""},
	} {
		var got [4]string
		read := func(_ context.Context, gp, gr, groot, ga string) (string, bool, landing.MainHistory, error) {
			got = [4]string{gp, gr, groot, ga}
			return gp, true, landing.MainHistory{State: landing.Effective}, errors.New("passed on")
		}
		ev, err := releaseEvidence(context.Background(), read, c.state, m, p)
		if got != [4]string{p, r, c.wantRoot, c.wnA} {
			t.Fatalf("%s: asked %v", c.name, got)
		}
		if err == nil || err.Error() != "passed on" || ev.Head != p || !ev.HasR || ev.Validity != string(landing.Effective) {
			t.Fatalf("%s: answer %+v %v", c.name, ev, err)
		}
	}
}
