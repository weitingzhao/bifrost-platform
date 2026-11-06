package store

import (
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// R5:5.7 batch of cores: the batch gate takes the lead's grant for a member
// only when its material named this very batch (the same members in the
// same order, the same M0 and R) and it waives every soft check of the
// member, pushed on that M0 and R.
func TestBatchCoresGrantNamesTheBatch(t *testing.T) {
	t.Parallel()
	m0, r := strings.Repeat("a", 40), strings.Repeat("b", 40)
	lead, mate := "land:t_"+strings.Repeat("A", 26)+":1", "land:t_"+strings.Repeat("B", 26)+":1"
	b := LandingBatch{Batch: landing.BatchID(lead), Lead: lead, Members: []string{lead, mate}, M0: m0, R: r}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	grant := func(edit func(g *grantView)) *grantView {
		g := &grantView{LandingGrant: LandingGrant{Op: lead, M0: m0, R: r, Action: ActionCoreLanding, Waives: []string{"core", "rule_paths"},
			Batch: &LandingGrantBatch{Batch: b.Batch, Members: []string{lead, mate}}}, exp: now.Add(time.Hour)}
		if edit != nil {
			edit(g)
		}
		return g
	}
	if !grantNamesBatch(grant(nil), b) {
		t.Fatal("the batch's own grant")
	}
	for name, edit := range map[string]func(g *grantView){
		"no batch":        func(g *grantView) { g.Batch = nil },
		"another batch":   func(g *grantView) { g.Batch.Batch = landing.BatchID(mate) },
		"other members":   func(g *grantView) { g.Batch.Members = []string{lead, mate, "land:t_" + strings.Repeat("C", 26) + ":1"} },
		"another order":   func(g *grantView) { g.Batch.Members = []string{mate, lead} },
		"another M0":      func(g *grantView) { g.M0 = strings.Repeat("c", 40) },
		"another R":       func(g *grantView) { g.R = strings.Repeat("c", 40) },
		"another lead op": func(g *grantView) { g.Op = mate },
	} {
		if grantNamesBatch(grant(edit), b) {
			t.Error(name, "names the batch")
		}
	}
	f := landing.Facts{Repo: "bifrost-platform", GrantEnabled: true}
	o := landing.Operation{Phase: "branch_pushed", R: r, M0: m0}
	soft := []rules.LandingRefusal{rules.LandingCore, rules.LandingRulePaths}
	c := &batchCover{grant: grant(nil), batch: b}
	if !c.covers(mate, o, f, soft, now) {
		t.Fatal("the member is not covered")
	}
	var none *batchCover
	other := o
	other.R = strings.Repeat("c", 40)
	off := f
	off.GrantEnabled = false
	for name, covered := range map[string]bool{
		"no cover":         none.covers(mate, o, f, soft, now),
		"the lead":         c.covers(lead, o, f, soft, now),
		"not waived":       c.covers(mate, o, f, append(soft, rules.LandingSize), now),
		"another R":        c.covers(mate, other, f, soft, now),
		"grants off":       c.covers(mate, o, off, soft, now),
		"expired":          c.covers(mate, o, f, soft, now.Add(time.Hour)),
		"not branch_pushd": c.covers(mate, landing.Operation{Phase: "awaiting_grant", R: r, M0: m0}, f, soft, now),
	} {
		if covered {
			t.Error(name, "covered")
		}
	}
}
