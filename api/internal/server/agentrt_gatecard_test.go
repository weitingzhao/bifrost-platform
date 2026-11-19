//bifrost:owner owner

package server

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questionbridge"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Review td397-rec-1 A1: a plan change's card made by a try whose
// transaction then failed is found again by a retry under the same key when
// only what the runtime recommends has changed since (the plan moved), and
// keeps what it said; a retry under that key asking something else is
// refused, as before.
func TestPlanChangeCardRetryFindsItsCard(t *testing.T) {
	ctx := context.Background()
	rt := questionbridge.New(filepath.Join(t.TempDir(), "questions"), nil)
	ask := rt.Ask()
	recommended := planstore.Card{Title: "Plan change to ARCH: the catalog's gates", Body: "A thread proposes to change plan ARCH.\n\n- a change",
		Recommended: 1, RecommendedWhy: "it gives architecture rows their first gates, exactly the reviewed catalog's for stg"}
	plain := recommended
	plain.Recommended, plain.RecommendedWhy = 0, ""
	for i, tries := range [][2]planstore.Card{{recommended, plain}, {plain, recommended}} {
		first, retry := tries[0], tries[1]
		first.Key = strings.Repeat(string(rune('a'+i)), 32)
		retry.Key = first.Key
		id, err := ask(ctx, first)
		if err != nil {
			t.Fatal(err)
		}
		if again, err := ask(ctx, retry); err != nil || again != id {
			t.Fatalf("try %d: the retry got %q (%v), want card %s", i, again, err, id)
		}
		q, ok, err := rt.Svc.Lookup(id)
		if err != nil || !ok || (q.Recommended == planstore.CardApplyKey) != (first.Recommended == 1) {
			t.Fatalf("try %d: the card %+v %v %v", i, q, ok, err)
		}
		other := retry
		other.Body += "\n- another change"
		if got, err := ask(ctx, other); err == nil || !strings.Contains(err.Error(), "is not this one") {
			t.Fatalf("try %d: another proposal under the key got %q (%v)", i, got, err)
		}
	}
}
