package planreadings

import (
	"reflect"
	"testing"
	"time"
)

func TestWaitingCardsKeepBoundBeforeDedupAndChoices(t *testing.T) {
	at := time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC)
	list := []WaitingMission{{Mission: "M-1", Question: "q1", Title: "first\nsecond", At: at, Open: 3, Why: "reason"}, {Mission: "M-2", Question: "q1", At: at}, {Mission: "M-3", Question: "q3", At: at}}
	listed := map[string]map[string]any{}
	cards, more := ProjectWaitingCards(list, 2, at.Add(125*time.Second), listed, "start", "stop")
	if !more || len(cards) != 1 || cards[0].Question != "q1" || cards[0].Item["title"] != "Mission M-1: first" || cards[0].Item["waiting_min"] != int64(2) || cards[0].Item["can_act_now"] != nil {
		t.Fatalf("%+v %v", cards, more)
	}
	action := cards[0].Item["action"].(map[string]any)
	if !reflect.DeepEqual(action["options"], []string{"start", "stop"}) || len(listed) != 1 {
		t.Fatal(action, listed)
	}
	again, more := ProjectWaitingCards(list, 2, at, listed, "start", "stop")
	if len(again) != 0 || !more {
		t.Fatal(again, more)
	}
	empty, more := ProjectWaitingCards(nil, 2, at, map[string]map[string]any{}, "start", "stop")
	if empty != nil || more {
		t.Fatal(empty, more)
	}
	if list[0].Title != "first\nsecond" {
		t.Fatal("input changed")
	}
}
