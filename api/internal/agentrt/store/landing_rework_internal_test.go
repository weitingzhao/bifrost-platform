package store

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// R5:5.5-18 §3.1: the set key is fixed by its siblings' (by, by_op, new_a)
// in by order, whatever order or why they come with.
func TestReworkSetKeyVector(t *testing.T) {
	t.Parallel()
	a := ReworkSibling{By: "t_01M6G0STV7647402B31WBHNRXB", ByOp: "land:t_01M6G0STV7647402B31WBHNRXB:1", NewA: strings.Repeat("1", 40), Why: "different"}
	b := ReworkSibling{By: "t_01M6F1CZSK0KJTNV25SA5MP4ZT", ByOp: "land:t_01M6F1CZSK0KJTNV25SA5MP4ZT:2", NewA: strings.Repeat("2", 40), Why: "not_comparable"}
	const want = "e8e527e9468cabf89e4d09264d92500bdca1c1d2c51cd39223ddc1c2feaaf740"
	for _, set := range [][]ReworkSibling{{a, b}, {b, a}} {
		if got := ReworkSetKey(set); got != want {
			t.Fatalf("set key %s, want %s", got, want)
		}
	}
	a.Why = "not_comparable"
	if ReworkSetKey([]ReworkSibling{a, b}) != want {
		t.Fatal("why changed the set key")
	}
	a.NewA = strings.Repeat("3", 40)
	if ReworkSetKey([]ReworkSibling{a, b}) == want {
		t.Fatal("another A kept the set key")
	}
}

func reworkTriggerOf(n int, title string) CardTrigger {
	v := &ReworkView{OldA: strings.Repeat("a", 40)}
	for i := range n {
		id := fmt.Sprintf("t_01M6G0STV7647402B31WB%05d", i)
		v.Siblings = append(v.Siblings, ReworkSiblingView{ReworkSibling: ReworkSibling{By: id, ByOp: "land:" + id + ":1",
			NewA: strings.Repeat("b", 40), Why: "different"}, Title: title})
	}
	return CardTrigger{Kind: CardReworkLanded, Seq: 9, At: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC), Root: "t_01M6EYXFTB6HC8T92TAG58TRTP",
		Op: "land:t_01M6EYXFTB6HC8T92TAG58TRTP:1", Why: "declined", Choices: []string{"count_replaced", "still_owed", "wait"}, Rework: v}
}

// §6.2: every body that fits names every sibling and passes the question
// store's own parser; titles go before siblings do, and a set too large for
// even task ids is not asked.
func TestReworkCardBodyFitsTheQuestionStore(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("改过再交的任务标题", 20) // multi-byte, far over the title bound
	for _, c := range []struct {
		n      int
		title  string
		fits   bool
		titled bool
	}{
		{3, "feat(agentrt): a short title", true, true},
		{3, long, true, true},
		{20, long, true, false},
		{40, long, true, false},
		{60, long, false, false},
	} {
		tr := reworkTriggerOf(c.n, c.title)
		body, fits := reworkCardBody("bifrost-platform", tr)
		if fits != c.fits {
			t.Fatalf("%d siblings: fits %v (%d bytes), want %v", c.n, fits, len(body), c.fits)
		}
		if !fits {
			if _, ok, err := reworkTriggerFits(tr); err != nil || ok {
				t.Fatalf("%d siblings: a body that does not fit was offered", c.n)
			}
			continue
		}
		if len(body) > ReworkBodyLimit {
			t.Fatalf("%d siblings: %d bytes", c.n, len(body))
		}
		for _, x := range tr.Rework.Siblings {
			if !strings.Contains(body, x.By) || !strings.Contains(body, x.NewA[:8]) {
				t.Fatalf("%d siblings: %s not named", c.n, x.By)
			}
		}
		if strings.Contains(body, "“") != c.titled {
			t.Fatalf("%d siblings, title %d bytes: titled %v, want %v", c.n, len(c.title), !c.titled, c.titled)
		}
		// The create body as questioncards' wire makes it (that package
		// imports this one): options as labels, the key, the title clipped.
		req := LandingCardRequest("bifrost-platform", tr, "Declined, rework landed: "+tr.Root)
		labels, _ := req["options"].([]any)
		opts := make([]map[string]string, 0, len(labels))
		for _, l := range labels {
			opts = append(opts, map[string]string{"label": fmt.Sprint(l)})
		}
		req["options"], req["idempotency_key"] = opts, cardKey("bifrost-platform", tr.Kind, tr.Seq, 1, cardTemplate)
		req["title"] = questions.ClipTitle(req["title"].(string))
		raw, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := questions.CanonicalizeRequestJSON(raw); err != nil {
			t.Fatalf("%d siblings: the question store refuses the request: %v", c.n, err)
		}
	}
}

// reworkTriggerFits is reworkTrigger's last check alone: a trigger whose
// body does not fit is not offered.
func reworkTriggerFits(tr CardTrigger) (CardTrigger, bool, error) {
	if _, fits := reworkCardBody("bifrost-platform", tr); !fits {
		return CardTrigger{}, false, nil
	}
	return tr, true, nil
}

// §3: each shape rule of the two records refuses on its own, with its
// reason; the good bodies pass.
func TestReworkRecordShapes(t *testing.T) {
	t.Parallel()
	root, by, by2 := "t_01M6EYXFTB6HC8T92TAG58TRTP", "t_01M6F1CZSK0KJTNV25SA5MP4ZT", "t_01M6G0STV7647402B31WBHNRXB"
	sib := func(b, why string) map[string]any {
		return map[string]any{"by": b, "by_op": "land:" + b + ":1", "new_a": strings.Repeat("1", 40), "why": why}
	}
	sibs := []ReworkSibling{{By: by, ByOp: "land:" + by + ":1", NewA: strings.Repeat("1", 40)}, {By: by2, ByOp: "land:" + by2 + ":1", NewA: strings.Repeat("1", 40)}}
	good := func() map[string]any {
		return map[string]any{"v": "1", "root": root, "op": "land:" + root + ":1", "old_a": strings.Repeat("a", 40), "set_key": ReworkSetKey(sibs),
			"siblings": []any{sib(by, "different"), sib(by2, "not_comparable")}}
	}
	with := func(k string, v any) map[string]any {
		m := good()
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
		return m
	}
	if why := badLandingReworkSeen(good()); why != "" {
		t.Fatalf("the good body: %s", why)
	}
	extraSib := sib(by, "different")
	extraSib["title"] = "x"
	for name, c := range map[string]struct {
		body   map[string]any
		reason string
	}{
		"not v1":                         {with("v", "2"), "is a v1 object"},
		"a field less":                   {with("old_a", nil), "is a v1 object"},
		"a field more":                   {with("why", "x"), "is a v1 object"},
		"root not a task":                {with("root", "nope"), "root is a task"},
		"no op":                          {with("op", ""), "needs op"},
		"old_a not a commit":             {with("old_a", "abc"), "old_a is a commit"},
		"set_key not a digest":           {with("set_key", "abc"), "set_key is a digest"},
		"no siblings":                    {with("siblings", []any{}), "at least one sibling"},
		"a sibling not an object":        {with("siblings", []any{"x"}), "at least one sibling"},
		"a sibling field more":           {with("siblings", []any{extraSib, sib(by2, "different")}), "sibling is an object of"},
		"a sibling is the root":          {with("siblings", []any{sib(root, "different")}), "sibling by is another task"},
		"an unknown why":                 {with("siblings", []any{sib(by, "same"), sib(by2, "different")}), "why is different or not_comparable"},
		"a sibling with no by_op":        {with("siblings", []any{func() map[string]any { m := sib(by, "different"); m["by_op"] = ""; return m }(), sib(by2, "different")}), "needs by_op and new_a"},
		"a sibling's new_a not a commit": {with("siblings", []any{func() map[string]any { m := sib(by, "different"); m["new_a"] = "abc"; return m }(), sib(by2, "different")}), "needs by_op and new_a"},
		"out of order":                   {with("siblings", []any{sib(by2, "different"), sib(by, "different")}), "in by order, each once"},
		"one twice":                      {with("siblings", []any{sib(by, "different"), sib(by, "different")}), "in by order, each once"},
		"set_key not its own":            {with("set_key", strings.Repeat("0", 64)), "set_key is not its siblings'"},
	} {
		if why := badLandingReworkSeen(c.body); !strings.Contains(why, c.reason) {
			t.Errorf("%s: %q, want %q", name, why, c.reason)
		}
	}

	replaced := func() map[string]any {
		return map[string]any{"v": "1", "root": root, "op": "land:" + root + ":1", "trigger_seq": float64(7), "question_id": "q1", "set_key": ReworkSetKey(sibs)}
	}
	if why := badLandingReplaced(replaced()); why != "" {
		t.Fatalf("the good replaced: %s", why)
	}
	for name, c := range map[string]struct {
		k      string
		v      any
		reason string
	}{
		"not v1":          {"v", "2", "is a v1 object"},
		"a field more":    {"by", "x", "is a v1 object"},
		"root not a task": {"root", "nope", "needs root, op and question_id"},
		"no question":     {"question_id", "", "needs root, op and question_id"},
		"no trigger":      {"trigger_seq", float64(0), "needs trigger_seq"},
		"set_key":         {"set_key", "abc", "set_key is a digest"},
	} {
		m := replaced()
		m[c.k] = c.v
		if why := badLandingReplaced(m); !strings.Contains(why, c.reason) {
			t.Errorf("replaced %s: %q, want %q", name, why, c.reason)
		}
	}
}

// §6.2, §6.4: the card's choices, labels, rank and outcome words.
func TestReworkCardWords(t *testing.T) {
	t.Parallel()
	if got := cardChoices[CardReworkLanded]; len(got) != 1 || strings.Join(got[0], ",") != "count_replaced,still_owed,wait" {
		t.Fatalf("choices %v", got)
	}
	for _, c := range []string{"count_replaced", "still_owed", "wait"} {
		if cardChoiceLabels[c] == "" {
			t.Errorf("no label for %s", c)
		}
	}
	if cardRank(CardReworkLanded) != 0 || slices.Contains(statusOnlyKinds, CardReworkLanded) {
		t.Fatal("a rework card is a decision that is asked")
	}
	base := map[string]any{"channel": "console", "kind": CardReworkLanded}
	for choice, why := range map[string]string{"count_replaced": "counted_replaced", "still_owed": "still_owed"} {
		m := maps.Clone(base)
		m["choice"], m["outcome"], m["why"] = choice, "applied", why
		if bad := badCardOutcomeChoice(m); bad != "" {
			t.Errorf("%s → %s: %s", choice, why, bad)
		}
		m["why"] = map[string]string{"counted_replaced": "still_owed", "still_owed": "counted_replaced"}[why]
		if bad := badCardOutcomeChoice(m); bad == "" {
			t.Errorf("%s recorded as %s", choice, m["why"])
		}
	}
}
