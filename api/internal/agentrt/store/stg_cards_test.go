package store

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

func stgTestCard(tr CardTrigger) CardRecord {
	opts := map[string]string{}
	for i, c := range tr.Choices {
		opts[string(rune('a'+i))] = c
	}
	return CardRecord{Family: "stg", Repo: "bifrost-platform", Kind: tr.Kind, Block: tr.Block, Batch: tr.Batch, TriggerSeq: tr.Seq,
		Prep: CardPrepID(tr.Kind, tr.Seq, 1), N: 1, Options: opts, QuestionID: "q1", Bound: true}
}

func stgTestPrepared(tr CardTrigger) map[string]any {
	c := stgTestCard(tr)
	r := LandingCardRequest(c.Repo, tr, stgCardTitle(tr.Kind))
	fp, _ := canon.Sum(r)
	opts, _ := cardOptions(c.Kind, tr.Choices)
	m := map[string]any{"v": "1", "family": "stg", "repo": c.Repo, "kind": c.Kind, "prep": c.Prep, "trigger_seq": c.TriggerSeq,
		"n": 1, "template_version": 1, "key": cardKey(c.Repo, c.Kind, c.TriggerSeq, 1, 1), "request": r, "request_fp": strings.TrimPrefix(fp, "sha256:"), "options": opts}
	if c.Block != "" {
		m["block"] = c.Block
	}
	if c.Batch != "" {
		m["batch"] = c.Batch
	}
	return m
}

func stgAnswer(c CardRecord, letter string) Question {
	return Question{ID: c.QuestionID, Status: QuestionAnswered, Channel: "console", AnsweredBy: "owner", OwnerAnswer: true, Option: letter}
}

func TestStgCardsShapesAndCopy(t *testing.T) {
	t.Parallel()
	for _, tr := range []CardTrigger{
		{Family: "stg", Kind: CardSTGFailed, Seq: 1, Block: strings.Repeat("a", 16), Choices: []string{"retry_release", "do_not_release"}},
		{Family: "stg", Kind: CardSTGBlocked, Seq: 2, Block: strings.Repeat("b", 16), Choices: []string{"ack", "remind_later"}},
		{Family: "stg", Kind: CardSTGBlocked, Seq: 3, Batch: stgTestBatch, Choices: []string{"ack", "remind_later", "accept_unreconciled"}},
		{Family: "stg", Kind: CardSTGIneligible, Seq: 4, Block: strings.Repeat("c", 16), Choices: []string{"ack", "remind_later"}},
	} {
		t.Run(tr.Kind+tr.Batch, func(t *testing.T) {
			good := stgTestPrepared(tr)
			if why := badCardRecord("landing_card_prepared", good); why != "" {
				t.Fatal(why)
			}
			body := good["request"].(map[string]any)["body"].(string)
			for _, text := range []string{"An answer is not an approval", "Approval effect:", "Checks not performed", "Unknown"} {
				if !strings.Contains(body, text) {
					t.Fatal("missing honest card copy", text, body)
				}
			}
			for _, change := range []map[string]any{{"family": ""}, {"family": 1}, {"root": stgTestRoot}, {"op": "land:one:1"}, {"stop_generation": 1}, {"block": "bad"}, {"batch": "bad"}, {"kind": CardRepoStopped}} {
				bad := stgClone(good)
				for k, v := range change {
					bad[k] = v
				}
				if why := badCardRecord("landing_card_prepared", bad); why == "" {
					t.Fatal("accepted mixed family", change)
				}
			}
			if tr.Batch == "" && tr.Kind == CardSTGBlocked {
				tr.Choices = append(tr.Choices, "accept_unreconciled")
				if why := badCardRecord("landing_card_prepared", stgTestPrepared(tr)); why == "" {
					t.Fatal("ordinary block offered risk acceptance")
				}
			}
		})
	}
}

func TestStgCardsDurableIntervalsAndBudgetDecision(t *testing.T) {
	t.Parallel()
	for _, choice := range []string{"a", "b"} {
		h := stgExhausted(t)
		block := h.block("budget_exhausted", stgTestMember())
		s := h.state()
		trs := stgTriggers(s)
		if len(trs) != 1 || trs[0].Kind != CardSTGFailed || trs[0].Seq != block.Seq {
			t.Fatal(trs)
		}
		for range 100 {
			if !reflect.DeepEqual(trs, stgTriggers(h.state())) {
				t.Fatal("scan changed trigger")
			}
		}
		c := stgTestCard(trs[0])
		for _, changed := range []Question{
			{ID: "q1", Channel: "mcp", AnsweredBy: "owner", Option: choice},
			{ID: "q1", Channel: "console", AnsweredBy: "agent", Option: choice},
			{ID: "q1", Channel: "console", AnsweredBy: "owner", OwnerAnswer: true, Option: "z"},
		} {
			m, rs := stgCardDecision(s, c, changed, h.at)
			if m["outcome"] != "refused" || len(rs) != 0 || badCardOutcome(m) != "" {
				t.Fatal("bad refusal", m, rs, badCardOutcome(m))
			}
		}
		body, records := stgCardDecision(s, c, stgAnswer(c, choice), h.at)
		if body["outcome"] != "applied" || len(records) != 2 || badCardOutcome(body) != "" {
			t.Fatal(body, records, badCardOutcome(body))
		}
		h.raw(map[string]any{"text": "answer", "landing_card_outcome": body})
		for _, r := range records {
			h.add(map[string]any(r))
		}
		after := h.state()
		if after.Blocks[c.Block].IsOpen() || len(after.Grants) != 1 || after.Budget[StgBudgetKey{stgSHA("a"), 1}] != 3 || after.Debits[stgSHA("a")].NotBefore != s.Debits[stgSHA("a")].NotBefore {
			t.Fatal("answer rewrote history or missed unblock")
		}
		if choice == "a" && after.generation(stgSHA("a")) != 2 || choice == "b" && len(after.Withheld) != 1 {
			t.Fatal("wrong business decision")
		}
		m, rs := stgCardDecision(after, c, stgAnswer(c, choice), h.at)
		if m["outcome"] != "moot" || len(rs) != 0 {
			t.Fatal("stale card created business records")
		}
		if len(stgTriggers(after)) != 0 {
			t.Fatal("answered interval still asks")
		}
	}
}

func TestStgCardsRecheckExactRemainingMembers(t *testing.T) {
	t.Parallel()
	h := stgExhausted(t)
	m, other := stgTestMember(), stgTestMember2()
	block := h.block("budget_exhausted", m)
	s := h.state()
	c := stgTestCard(stgTriggers(s)[0])
	// The answer must not scoop up an unrelated exhausted member.
	s.Members[stgMember(other)] = s.Members[stgMember(m)]
	body, _ := stgCardDecision(s, c, stgAnswer(c, "a"), h.at)
	if !stgSame(body["members"], []any{m}) {
		t.Fatal("included sibling interval", body)
	}
	for _, dispose := range []string{"covered", "withheld", "legacy_owner", "disposed", "replaced", "block_closed"} {
		s := h.state()
		key := stgMember(m)
		switch dispose {
		case "covered":
			s.Covers[StgCoverKey{"manual", key}] = StgEvent{Seq: 100}
		case "withheld":
			s.Withheld[key] = StgEvent{Seq: 100}
		case "legacy_owner":
			s.Disposals[key.Op] = StgEvent{Seq: 100}
		case "disposed":
			x := s.Members[key]
			x.To = "disposed"
			s.Members[key] = x
		case "replaced":
			delete(s.Members, key)
		case "block_closed":
			b := s.Blocks[c.Block]
			b.Closed = StgEvent{Seq: block.Seq + 1}
			s.Blocks[c.Block] = b
		}
		body, rs := stgCardDecision(s, c, stgAnswer(c, "a"), h.at)
		if body["outcome"] != "moot" || len(rs) != 0 || badCardOutcome(body) != "" {
			t.Fatal(dispose, body, rs)
		}
	}
}

func TestStgCardsBlockedQuarantineAndHardFloor(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"block", "quarantine", "floor"} {
		h := stgReady(t, stgTestMember())
		switch mode {
		case "block":
			h.block("ci_red", stgTestMember())
		case "quarantine":
			h.add(h.start(1))
			h.add(stgRecord("quarantined", "try", 1, "why", "unknown_execution", "at", h.at.Format(time.RFC3339Nano)))
		case "floor":
			b := h.block("hard_floor", stgTestMember())
			h.add(stgRecord("withheld", "grant", "hard-floor", "members", []any{stgTestMember()}, "why", "hard_floor", "trigger_seq", b.Seq))
		}
		s := h.state()
		trs := stgTriggers(s)
		tr := trs[0]
		if mode == "floor" {
			tr = trs[len(trs)-1]
			if tr.Kind != CardSTGIneligible {
				t.Fatal(tr)
			}
		}
		c := stgTestCard(tr)
		for _, letter := range []string{"a", "b"} {
			body, rs := stgCardDecision(s, c, stgAnswer(c, letter), h.at)
			want := map[string]string{"a": "acknowledged", "b": "held"}[letter]
			if body["outcome"] != want || len(rs) != 0 || badCardOutcome(body) != "" {
				t.Fatal(body, rs, badCardOutcome(body))
			}
			if letter == "b" && body["remind_at"] != h.at.Add(24*time.Hour).Format(time.RFC3339Nano) {
				t.Fatal("wrong reminder")
			}
		}
		body, rs := stgCardDecision(s, c, stgAnswer(c, "c"), h.at)
		if mode != "quarantine" {
			if body["outcome"] != "refused" || len(rs) != 0 {
				t.Fatal("notice bypassed floor/block")
			}
			continue
		}
		if body["outcome"] != "applied" || badCardOutcome(body) != "" || len(rs) != 1 {
			t.Fatal(body, rs)
		}
		h.raw(map[string]any{"text": "answer", "landing_card_outcome": body})
		h.add(map[string]any(rs[0]))
		after := h.state()
		if after.Batches[stgTestBatch].Tries[1].Occupied() || after.Budget[StgBudgetKey{stgSHA("a"), 1}] != 1 || len(after.Evidence) != len(s.Evidence) || len(stgTriggers(after)) != 0 {
			t.Fatal("risk closure changed delivery/budget")
		}
	}
}

func TestStgCardsGrantMixedGenerations(t *testing.T) {
	t.Parallel()
	h := stgExhausted(t)
	h.block("budget_exhausted", stgTestMember(), stgTestMember2())
	s := h.state()
	keys := []StgMemberKey{stgMember(stgTestMember()), stgMember(stgTestMember2())}
	// A detached read after two members exhausted different historical
	// generations. Check the real decision and append transition together.
	s.Generations[keys[0].R], s.Generations[keys[1].R] = 2, 5
	for i, gen := range []int64{2, 5} {
		old := s.Members[keys[0]]
		old.Gen, old.To = gen, "exhausted"
		s.Members[keys[i]] = old
		s.Batches[stgTestBatch].Members[keys[i]] = gen
		s.Budget[StgBudgetKey{keys[i].R, gen}] = 3
	}
	c := stgTestCard(stgTriggers(s)[0])
	body, records := stgCardDecision(s, c, stgAnswer(c, "a"), h.at)
	if !stgSame(body["members"], stgMemberValues(keys)) || len(records) != 2 || records[0].Int("gen") != 6 {
		t.Fatal(body, records)
	}
	s.Answers[c.QuestionID] = StgEvent{Seq: s.LastSeq + 1, At: h.at, Record: StgRecord(body)}
	s.LastSeq++
	bad := stgClone(map[string]any(records[0]))
	bad["gen"] = 3
	if _, _, err := checkStgReleaseEvent(s, stgNote(bad), s.LastSeq+1, h.at); err == nil {
		t.Fatal("reused generation accepted")
	}
	if _, _, err := checkStgReleaseEvent(s, stgNote(map[string]any(records[0])), s.LastSeq+1, h.at); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if s.Members[key].Gen != 6 || s.Members[key].To != "released" || s.Budget[StgBudgetKey{key.R, 6}] != 0 {
			t.Fatal("fresh budget missing", s.Members)
		}
	}
	if s.Budget[StgBudgetKey{keys[0].R, 2}] != 3 || s.Budget[StgBudgetKey{keys[1].R, 5}] != 3 {
		t.Fatal("old debits erased")
	}
	// Once an external cover arrives, the next decision names only the
	// remaining member, using its own next generation.
	s = h.state()
	old := s.Members[keys[0]]
	s.Members[keys[1]] = old
	s.Covers[StgCoverKey{"manual", keys[0]}] = StgEvent{Seq: 100}
	body, records = stgCardDecision(s, c, stgAnswer(c, "a"), h.at)
	if !stgSame(body["members"], stgMemberValues(keys[1:])) || len(records) != 2 || records[0].Int("gen") != 2 {
		t.Fatal("partial coverage granted wrong members", body, records)
	}
}
