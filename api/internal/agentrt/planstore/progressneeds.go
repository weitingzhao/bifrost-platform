package planstore

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"
)

// The progress projection's needs_you (progress.go has the rest): the plan
// changes waiting on a decision card, and what each holds up.

// needsYou lists what the Owner may act on (SPEC §2.1): plan changes on a
// decision card no decision is recorded for, missions outside the standing
// envelope waiting on their card, and blocked tasks on their open card
// (blockedcard.go). Whether the card is still open
// is the question store's to say, which this read does not reach: an item is
// a candidate, and the Console keeps it only while that question is open.
// Waits he cannot act on are the missions' statuses.
func needsYou(ctx context.Context, tx pgx.Tx, now time.Time) (map[string]any, error) {
	type pending struct {
		event                    int64
		stream, reason, question string
		at                       time.Time
		changes                  []PlanChange
	}
	var list []pending
	err := planreadings.PendingChanges(ctx, tx, now, pendingChangeWindow, PendingChangeLimit, func(observed planreadings.PendingChange) error {
		p := pending{event: observed.Event, stream: observed.Stream, reason: observed.Reason, question: observed.Question, at: observed.At}
		if err := json.Unmarshal([]byte(observed.Changes), &p.changes); err != nil {
			return err
		}
		list = append(list, p)
		return nil
	})
	if err != nil {
		return nil, err
	}

	inputs := make([]planreadings.PendingNeed, 0, len(list))
	for _, p := range list {
		kind := "change"
		_, isRecorder, err := recorderImportLog(p.changes)
		if err != nil {
			return nil, err
		}
		if isRecorder {
			kind = "recorder"
		} else if _, ok := importOf(p.changes); ok {
			kind = "import"
		}
		inputs = append(inputs, planreadings.PendingNeed{Event: p.event, Stream: p.stream, Root: streamRoot(p.stream), Work: strings.TrimPrefix(p.stream, recorderStreamPrefix), Reason: p.reason, Question: p.question, At: p.at, Kind: kind, Payload: p.changes})
	}
	return planreadings.ProjectPendingNeeds(inputs, PendingChangeLimit, now, planreadings.NeedLabels{Apply: CardApply, Refuse: CardRefuse, RecorderApply: recorderApplyLabel, RecorderRefuse: recorderRefuseLabel, ImportApply: ImportApply, ImportRefuse: ImportRefuse}, func(raw any) (any, error) { return changeBlocks(ctx, tx, raw.([]PlanChange)) }, func(listed map[string]map[string]any) ([]planreadings.WaitingCard, bool, error) {
		cards, more, err := missionCards(ctx, tx, now, listed)
		var out []planreadings.WaitingCard
		for _, c := range cards {
			out = append(out, planreadings.WaitingCard{At: c.at, Question: c.question, Item: c.it})
		}
		return out, more, err
	})
}

// Historical mission cards retain their original option labels.
const (
	MissionStart    = "Start the mission"
	MissionNotStart = "Do not start it"
)

type missionCard struct {
	at       time.Time
	question string
	it       map[string]any
}

// missionCards are the missions outside the standing envelope waiting on
// their card, oldest first, at most the bound (more says there were more),
// leaving out a card already listed.
func missionCards(ctx context.Context, tx pgx.Tx, now time.Time, listed map[string]map[string]any) ([]missionCard, bool, error) {
	list, err := planreadings.WaitingMissions(ctx, tx, PendingChangeLimit)
	if err != nil {
		return nil, false, err
	}
	projected, more := planreadings.ProjectWaitingCards(list, PendingChangeLimit, now, listed, MissionStart, MissionNotStart)
	var cards []missionCard
	for _, c := range projected {
		cards = append(cards, missionCard{at: c.At, question: c.Question, it: c.Item})
	}
	return cards, more, nil
}

// changeBlocks is what a waiting plan change holds up (SPEC §2.1), from one
// set: the steps it touches (a node that is a step, or the steps under a
// node that is an item: an added node's parent, a retitled or re-ruled node,
// a rebound step, a moved task's target) and the tasks it moves. Its
// missions are those of these steps (a task on a step is of the step's
// mission), of the moved tasks (where they come from) and the missions a
// change binds to. Unknown when it touches no mission. It carries no count of
// unfinished tasks: no task is written any more (store cut 4, 2026-11-14), so
// that count had no source and was a constant 0.
func changeBlocks(ctx context.Context, tx pgx.Tx, changes []PlanChange) (any, error) {
	var nodes, bound []string
	for _, c := range changes {
		switch c.Op {
		case "add_node":
			nodes = append(nodes, c.Parent)
		case "move_task":
			nodes = append(nodes, c.Node)
		default:
			nodes = append(nodes, c.Node)
		}
		// Only these two bind a mission (planchange.go applyChanges); a
		// mission_id on another op changes nothing.
		if c.MissionID != "" && (c.Op == "rebind_step" || c.Op == "add_node") {
			bound = append(bound, c.MissionID)
		}
	}
	missions, err := planreadings.AffectedMissions(ctx, tx, nodes, bound)
	if err != nil {
		return nil, err
	}
	if len(missions) == 0 {
		return map[string]any{"unknown": "the change touches no step a mission's work sits on"}, nil
	}
	return map[string]any{"missions": missions}, nil
}
