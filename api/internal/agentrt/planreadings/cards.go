package planreadings

import (
	"strings"
	"time"
)

// WaitingCard is a historical mission's read projection; it grants no permission.
type WaitingCard struct {
	At       time.Time
	Question string
	Item     map[string]any
}

// ProjectWaitingCards preserves the shared question deduplication and truncation
// before deduplication. Choice labels come from the existing caller.
func ProjectWaitingCards(list []WaitingMission, limit int, now time.Time, listed map[string]map[string]any, startOption, stopOption string) ([]WaitingCard, bool) {
	more := len(list) > limit
	if more {
		list = list[:limit]
	}
	var cards []WaitingCard
	for _, c := range list {
		if _, seen := listed[c.Question]; seen {
			continue
		}
		it := map[string]any{"id": "question:" + c.Question, "kind": "mission", "ref": c.Mission, "also_refs": []string{},
			"title": "Mission " + c.Mission + ": " + firstTitleLine(c.Title), "since": c.At.UTC().Format(time.RFC3339Nano),
			"waiting_min": int64(now.Sub(c.At) / time.Minute),
			"action":      map[string]any{"where": "console", "question_id": c.Question, "options": []string{startOption, stopOption}},
			"can_act_now": nil, "can_act_now_reason": "the card's state is the question store's: open there, it can be answered now",
			"deadline": nil, "blocks": map[string]any{"missions": []string{c.Mission}, "unfinished_tasks": c.Open, "why": c.Why},
			"after": "Start: the controller makes it active on its next pass (about 15 seconds). Do not start: it stops."}
		listed[c.Question] = it
		cards = append(cards, WaitingCard{At: c.At, Question: c.Question, Item: it})
	}
	return cards, more
}
func firstTitleLine(s string) string { line, _, _ := strings.Cut(s, "\n"); return line }
