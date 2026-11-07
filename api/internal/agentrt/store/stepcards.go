package store

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Step cards (goal 2): the Owner's approvals, questions, sign-offs and
// landing and STG cards, each bound to the plan step it serves and carrying
// every attempt of it, a card voided and opened again being its next
// attempt. Pure: the reader gathers the rows and events, FoldStepCards says
// what they mean.
//
// How a card finds its step, first match wins:
//
//	binding     a plan_node_binding row (approval or question) names it
//	task        its copy or card names a task, and the task is on a step
//	landing_op  its landing or STG card names an op whose task is on a step
//	signoff     an owner_signoff event carries the step itself
//
// A card with no step is kept under the empty key, never dropped.

// Step card kinds.
const (
	StepCardApproval = "approval"
	StepCardQuestion = "question"
	StepCardSignoff  = "signoff"
	StepCardLanding  = "landing_card"
	StepCardSTG      = "stg_card"
)

// How a step card found its step.
const (
	StepViaBinding   = "binding"
	StepViaTask      = "task"
	StepViaLandingOp = "landing_op"
	StepViaSignoff   = "signoff"
)

// StepBinding is a row of agentrt.plan_node_binding.
type StepBinding struct {
	PlanNodeID, Kind, Ref string // Kind: approval or question
	EventID               int64
}

// SeenCopy is an approval_seen or question_seen event: Ref is the approval
// number or question id, TaskID the task the card was asked for, when known.
type SeenCopy struct {
	Ref, Status, Option, AnsweredBy, AnsweredAt, TaskID string
	At                                                  time.Time
	EventID                                             int64
}

// StepCardRecord is a landing or STG card as its records say, with the
// times of its end records and the event that opened it.
type StepCardRecord struct {
	Card              CardRecord
	AckedAt, ClosedAt time.Time
	EventID           int64
}

// SignoffEvent is an owner_signoff event.
type SignoffEvent struct {
	PlanNodeID, Ref, By string
	At                  time.Time
	EventID             int64
}

// StepCardsInput is everything FoldStepCards reads.
type StepCardsInput struct {
	Bindings  []StepBinding
	Tasks     map[string]string            // task_id → plan_node_id ("" when on no step)
	Ops       map[string]landing.Operation // op id → operation
	Cards     []StepCardRecord
	Questions []SeenCopy // question_seen
	Approvals []SeenCopy // approval_seen
	Signoffs  []SignoffEvent
}

// StepCard is one card on a step with its attempts, oldest first.
type StepCard struct {
	Kind       string        `json:"kind"`
	Ref        string        `json:"ref"`
	PlanNodeID string        `json:"plan_node_id"`
	Via        string        `json:"via,omitempty"`
	TaskID     string        `json:"task_id,omitempty"`
	Attempts   []CardAttempt `json:"attempts"`
}

// CardAttempt is one asking of a card.
type CardAttempt struct {
	N          int         `json:"n"`
	QuestionID string      `json:"question_id,omitempty"`
	OpenedAt   time.Time   `json:"opened_at,omitzero"`
	ClosedAt   time.Time   `json:"closed_at,omitzero"`
	Result     *CardResult `json:"result,omitempty"`
	VoidReason string      `json:"void_reason,omitempty"`
	EventID    int64       `json:"event_id,omitempty"`
}

// CardResult is how an attempt ended: the answer read, the card's choice
// for it, or the last status seen.
type CardResult struct {
	Status     string `json:"status,omitempty"`
	Option     string `json:"option,omitempty"`
	Choice     string `json:"choice,omitempty"`
	AnsweredBy string `json:"answered_by,omitempty"`
	AnsweredAt string `json:"answered_at,omitempty"`
}

// stepCardOpen are the statuses of a copy still waiting.
var stepCardOpen = []string{"", "open", "pending"}

// FoldStepCards folds the cards into their steps, keyed by plan_node_id.
func FoldStepCards(in StepCardsInput) map[string][]StepCard {
	bound := map[string]string{} // kind + "\x00" + ref → step, the first row
	for _, b := range slices.SortedStableFunc(slices.Values(in.Bindings), func(a, b StepBinding) int { return cmp.Compare(a.EventID, b.EventID) }) {
		if k := b.Kind + "\x00" + b.Ref; bound[k] == "" && b.PlanNodeID != "" {
			bound[k] = b.PlanNodeID
		}
	}
	onStep := func(task string) string {
		if task == "" {
			return ""
		}
		return in.Tasks[task]
	}
	out := map[string][]StepCard{}
	add := func(c StepCard) { out[c.PlanNodeID] = append(out[c.PlanNodeID], c) }

	questions := seenByRef(in.Questions)
	approvals := seenByRef(in.Approvals)
	inFamily := map[string]bool{} // questions a card family asked

	// Landing and STG cards: one card per family, one attempt per N.
	families := map[string][]StepCardRecord{}
	var order []string
	for _, r := range in.Cards {
		k := cardFamilyKey(r.Card)
		if _, seen := families[k]; !seen {
			order = append(order, k)
		}
		families[k] = append(families[k], r)
		if r.Card.QuestionID != "" {
			inFamily[r.Card.QuestionID] = true
		}
	}
	for _, k := range order {
		recs := families[k]
		slices.SortStableFunc(recs, func(a, b StepCardRecord) int { return cmp.Compare(a.Card.N, b.Card.N) })
		first := recs[0].Card
		c := StepCard{Kind: StepCardLanding, Ref: cardFamilyRef(first)}
		if first.Family == "stg" {
			c.Kind = StepCardSTG
		}
		for _, r := range recs {
			if step := bound[StepCardQuestion+"\x00"+r.Card.QuestionID]; r.Card.QuestionID != "" && step != "" && c.Via == "" {
				c.PlanNodeID, c.Via = step, StepViaBinding
			}
		}
		if c.Via == "" {
			for _, r := range recs {
				for _, cp := range questions[r.Card.QuestionID] {
					if step := onStep(cp.TaskID); r.Card.QuestionID != "" && step != "" && c.Via == "" {
						c.PlanNodeID, c.Via, c.TaskID = step, StepViaTask, cp.TaskID
					}
				}
			}
		}
		if c.Via == "" {
			for _, r := range recs {
				if op, ok := in.Ops[r.Card.Op]; ok && r.Card.Op != "" && c.Via == "" {
					if step := onStep(op.Task); step != "" {
						c.PlanNodeID, c.Via, c.TaskID = step, StepViaLandingOp, op.Task
					}
				}
			}
		}
		for _, r := range recs {
			c.Attempts = append(c.Attempts, cardRecordAttempt(r, questions[r.Card.QuestionID]))
		}
		add(c)
	}

	// Questions and approvals asked outside a card family: one attempt from
	// their first and last copies.
	single := func(kind string, copies map[string][]SeenCopy) {
		refs := map[string]bool{}
		for ref := range copies {
			refs[ref] = true
		}
		for _, b := range in.Bindings {
			if b.Kind == kind {
				refs[b.Ref] = true
			}
		}
		for _, ref := range slices.Sorted(func(yield func(string) bool) {
			for r := range refs {
				if !yield(r) {
					return
				}
			}
		}) {
			if ref == "" || (kind == StepCardQuestion && inFamily[ref]) {
				continue
			}
			c := StepCard{Kind: kind, Ref: ref, Attempts: []CardAttempt{}}
			if step := bound[kind+"\x00"+ref]; step != "" {
				c.PlanNodeID, c.Via = step, StepViaBinding
			} else {
				for _, cp := range copies[ref] {
					if step := onStep(cp.TaskID); step != "" {
						c.PlanNodeID, c.Via, c.TaskID = step, StepViaTask, cp.TaskID
						break
					}
				}
			}
			if cps := copies[ref]; len(cps) > 0 {
				c.Attempts = append(c.Attempts, seenAttempt(kind, ref, cps))
			}
			add(c)
		}
	}
	single(StepCardApproval, approvals)
	single(StepCardQuestion, questions)

	// Sign-offs: one card per step and ref, one attempt per event.
	signoffs := slices.SortedStableFunc(slices.Values(in.Signoffs), func(a, b SignoffEvent) int { return cmp.Compare(a.EventID, b.EventID) })
	at := map[string]int{} // step + ref → index in out[step]
	for _, s := range signoffs {
		k := s.PlanNodeID + "\x00" + s.Ref
		i, seen := at[k]
		if !seen {
			c := StepCard{Kind: StepCardSignoff, Ref: s.Ref, PlanNodeID: s.PlanNodeID}
			if s.PlanNodeID != "" {
				c.Via = StepViaSignoff
			}
			add(c)
			i = len(out[s.PlanNodeID]) - 1
			at[k] = i
		}
		c := &out[s.PlanNodeID][i]
		c.Attempts = append(c.Attempts, CardAttempt{N: len(c.Attempts) + 1, OpenedAt: s.At, ClosedAt: s.At, EventID: s.EventID,
			Result: &CardResult{Status: "signed", AnsweredBy: s.By, AnsweredAt: s.At.UTC().Format(time.RFC3339Nano)}})
	}

	kindOrder := []string{StepCardApproval, StepCardQuestion, StepCardLanding, StepCardSTG, StepCardSignoff}
	for _, cards := range out {
		slices.SortStableFunc(cards, func(a, b StepCard) int {
			return cmp.Or(cmp.Compare(slices.Index(kindOrder, a.Kind), slices.Index(kindOrder, b.Kind)), cmp.Compare(a.Ref, b.Ref))
		})
	}
	return out
}

// seenByRef groups copies by their ref, oldest first.
func seenByRef(copies []SeenCopy) map[string][]SeenCopy {
	out := map[string][]SeenCopy{}
	for _, cp := range slices.SortedStableFunc(slices.Values(copies), func(a, b SeenCopy) int { return cmp.Compare(a.EventID, b.EventID) }) {
		out[cp.Ref] = append(out[cp.Ref], cp)
	}
	return out
}

// cardFamilyKey names a card's family: every N of one trigger and kind.
func cardFamilyKey(c CardRecord) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s", c.Repo, c.Family, c.Kind, c.TriggerSeq, c.Root, c.Op, c.Block, c.Batch)
}

// cardFamilyRef is a family as shown: its stream and its prep without N.
func cardFamilyRef(c CardRecord) string {
	return CardsStream(c.Repo) + "/card:" + strconv.FormatInt(c.TriggerSeq, 10) + ":" + c.Kind
}

// lastAnswered is the last answered copy, if any.
func lastAnswered(copies []SeenCopy) (SeenCopy, bool) {
	for i := len(copies) - 1; i >= 0; i-- {
		if copies[i].Status == QuestionAnswered {
			return copies[i], true
		}
	}
	return SeenCopy{}, false
}

// cardRecordAttempt is one N of a card family.
func cardRecordAttempt(r StepCardRecord, copies []SeenCopy) CardAttempt {
	c := r.Card
	a := CardAttempt{N: c.N, QuestionID: c.QuestionID, EventID: r.EventID}
	switch {
	case c.Bound:
		a.OpenedAt = c.BoundAt
	case c.AskStarted:
		a.OpenedAt = c.AskStartedAt
	}
	ans, hasAnswer := lastAnswered(copies)
	if hasAnswer {
		a.Result = &CardResult{Status: QuestionAnswered, Option: ans.Option, Choice: c.Options[ans.Option], AnsweredBy: ans.AnsweredBy, AnsweredAt: ans.AnsweredAt}
	} else if c.Acked {
		a.Result = &CardResult{Status: "acked"}
	}
	switch {
	case c.Closed:
		a.ClosedAt = r.ClosedAt
		a.VoidReason = c.ClosedWhy
	case hasAnswer:
		if t, err := time.Parse(time.RFC3339Nano, ans.AnsweredAt); err == nil {
			a.ClosedAt = t
		} else {
			a.ClosedAt = ans.At
		}
	case c.Acked:
		a.ClosedAt = r.AckedAt
	}
	return a
}

// seenAttempt is the one attempt of a question or approval asked outside a
// card family: opened at its first copy, closed at its last when that one no
// longer waits.
func seenAttempt(kind, ref string, copies []SeenCopy) CardAttempt {
	first, last := copies[0], copies[len(copies)-1]
	a := CardAttempt{N: 1, OpenedAt: first.At, EventID: last.EventID}
	if kind == StepCardQuestion {
		a.QuestionID = ref
	}
	if !slices.Contains(stepCardOpen, last.Status) {
		a.ClosedAt = last.At
	}
	if last.Status != "" {
		a.Result = &CardResult{Status: last.Status}
	}
	if ans, ok := lastAnswered(copies); ok && kind == StepCardQuestion {
		a.Result = &CardResult{Status: last.Status, Option: ans.Option, AnsweredBy: ans.AnsweredBy, AnsweredAt: ans.AnsweredAt}
	}
	return a
}
