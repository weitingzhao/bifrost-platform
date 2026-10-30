package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// B5 and its cards share the supervisor's landing:dev rule. An expired
// non-none rule still permits notices/settlement, never a release itself.
func stgCardPolicy(ctx context.Context, tx pgx.Tx) (bool, error) {
	r, err := effectiveRule(ctx, tx, "landing:dev", DecisionAt(), true)
	if err != nil || r == nil || r.Landing == nil {
		return false, err
	}
	repo, ok := r.Landing.Repo("bifrost-platform")
	return ok && repo.STGRelease == "bifrost-deliver-platform", nil
}

func lockStgCardPolicy(ctx context.Context, tx pgx.Tx, repo string) (bool, error) {
	if repo != "bifrost-platform" {
		return false, nil
	}
	return stgControlEnabled(ctx, tx, "dev", func() bool { return true })
}

// Look up the durable family before locking. No STG lifecycle record is
// written under none, even for a card prepared before the rule changed.
func stgCardWriteAllowed(ctx context.Context, tx pgx.Tx, repo, prep string) (bool, error) {
	cards, err := LandingCards(ctx, tx, CardsStream(repo))
	if err != nil {
		return false, err
	}
	if cards[prep].Family != "stg" {
		return true, nil
	}
	return lockStgCardPolicy(ctx, tx, repo)
}

func stgCardTriggers(ctx context.Context, tx pgx.Tx, repo string) ([]CardTrigger, error) {
	if repo != "bifrost-platform" {
		return nil, nil
	}
	enabled, err := stgCardPolicy(ctx, tx)
	if err != nil || !enabled {
		return nil, err
	}
	s, err := StgReleaseState(ctx, tx)
	if err != nil {
		return nil, err
	}
	return stgTriggers(s), nil
}

func stgTriggers(s StgState) []CardTrigger {
	var out []CardTrigger
	for id, block := range s.Blocks {
		if !block.IsOpen() {
			continue
		}
		r := block.Opened.Record
		tr := CardTrigger{Family: "stg", Kind: CardSTGBlocked, Block: id, Seq: block.Opened.Seq,
			At: r.Time("first_at"), Why: r.String("why"), P: r.String("P"), BaseP: r.object("base").String("P"), Choices: []string{"ack", "remind_later"}}
		for _, m := range r.object("detail").members("members") {
			tr.Members = append(tr.Members, m.StgMemberKey)
		}
		if tr.Why == "budget_exhausted" {
			tr.Kind, tr.Choices = CardSTGFailed, []string{"retry_release", "do_not_release"}
			tr.Members = stgExhaustedMembers(s, block.Opened)
			if len(tr.Members) == 0 {
				continue
			}
		}
		sortStgMembers(tr.Members)
		out = append(out, tr)
	}
	for id, b := range s.Batches {
		for _, t := range b.Tries {
			if !t.IsQuarantined() {
				continue
			}
			tr := CardTrigger{Family: "stg", Kind: CardSTGBlocked, Batch: id, Seq: t.Quarantined.Seq,
				At: t.Quarantined.At, Why: t.Quarantined.Record.String("why"), P: t.Snapshot.Record.object("inputs").String("revision"),
				BaseP: t.Snapshot.Record.object("base").String("P"), Choices: []string{"ack", "remind_later", "accept_unreconciled"}}
			for m := range b.Members {
				tr.Members = append(tr.Members, m)
			}
			sortStgMembers(tr.Members)
			out = append(out, tr)
		}
	}
	// One notice per durable withholding, even when it names several roots.
	seen := map[int64]bool{}
	for _, e := range s.Withheld {
		if e.Record.String("why") != "hard_floor" || seen[e.Seq] {
			continue
		}
		seen[e.Seq] = true
		trigger := s.Records[e.Record.Int("trigger_seq")]
		tr := CardTrigger{Family: "stg", Kind: CardSTGIneligible, Block: trigger.Record.String("block"), Seq: e.Seq,
			At: e.At, Why: "hard_floor", P: trigger.Record.String("P"), BaseP: trigger.Record.object("base").String("P"), Choices: []string{"ack", "remind_later"}}
		for _, m := range e.Record.members("members") {
			if s.Withheld[m.StgMemberKey].Seq == e.Seq {
				tr.Members = append(tr.Members, m.StgMemberKey)
			}
		}
		if len(tr.Members) > 0 {
			sortStgMembers(tr.Members)
			out = append(out, tr)
		}
	}
	slices.SortFunc(out, func(a, b CardTrigger) int {
		if a.Seq < b.Seq {
			return -1
		}
		if a.Seq > b.Seq {
			return 1
		}
		return 0
	})
	return out
}

// Re-read the interval's exact set; a later external cover or disposition
// removes only that member. Another interval's exhausted roots never join.
func stgExhaustedMembers(s StgState, trigger StgEvent) []StgMemberKey {
	var out []StgMemberKey
	for _, m := range trigger.Record.object("detail").members("members") {
		key := m.StgMemberKey
		old := s.Members[key]
		b := s.Batches[old.Batch]
		if old.To != "exhausted" || s.covered(key) || s.Withheld[key].Seq > 0 || s.Disposals[key.Op].Seq > 0 ||
			s.Legacy.Operations[key.Op].Reverted || b == nil || b.Phase != "CLOSED" {
			continue
		}
		out = append(out, key)
	}
	sortStgMembers(out)
	return out
}

func matchingStgTrigger(s StgState, c CardRecord) (CardTrigger, bool) {
	for _, tr := range stgTriggers(s) {
		if c.Family == tr.Family && c.Kind == tr.Kind && c.TriggerSeq == tr.Seq && c.Block == tr.Block && c.Batch == tr.Batch {
			return tr, true
		}
	}
	return CardTrigger{}, false
}

func checkStgCardPrepared(ctx context.Context, tx pgx.Tx, m map[string]any) error {
	enabled, err := stgCardPolicy(ctx, tx)
	if err != nil {
		return err
	}
	if !enabled || m["repo"] != "bifrost-platform" {
		return fmt.Errorf("STG cards require an enabled repository rule")
	}
	s, err := StgReleaseState(ctx, tx)
	if err != nil {
		return err
	}
	c := stgCardIdentity(m)
	tr, ok := matchingStgTrigger(s, c)
	if !ok {
		return fmt.Errorf("STG card requires its current durable trigger")
	}
	choices := []string{}
	opts, _ := m["options"].(map[string]any)
	for _, letter := range []string{"a", "b", "c"} {
		if v, ok := opts[letter].(string); ok {
			choices = append(choices, v)
		}
	}
	if !slices.Equal(choices, tr.Choices) {
		return errCardOptions
	}
	return nil
}

func stgCardIdentity(m map[string]any) CardRecord {
	seq, _ := cardNumber(m["trigger_seq"])
	return CardRecord{Family: valueString(m["family"]), Kind: valueString(m["kind"]), Block: valueString(m["block"]), Batch: valueString(m["batch"]), TriggerSeq: seq}
}

func sameCardFamily(c CardRecord, m map[string]any) bool {
	return c.Family == valueString(m["family"]) && c.Block == valueString(m["block"]) && c.Batch == valueString(m["batch"])
}
