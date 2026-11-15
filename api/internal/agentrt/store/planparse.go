package store

import (
	"encoding/json"
)

// Reading a plan (W-31 2.2b, the thin chief of staff). A planner hands in a
// report that ends in one fenced JSON block in the format its spec gives
// (goal.go); ParsePlan reads the last such block and checks it by program
// (ADR §12.2 item 13: the model proposes, the program lets it through).
// Nothing a planner writes reaches a command line as a program to run: a
// check of its own is one shape of `go test` (review 216 A1), and every card
// field fits the question store's own limits before the first card is made
// (review 216 B2).

// Bounds of a plan. The number of tasks and of questions has no bound since
// cleanup wave 1 (2026-11-11).
const (
	PlanMaxDecisions = 10
	PlanMaxCriteria  = 10
	planMaxVerify    = 5    // checks a plan adds to a task
	planMaxText      = 8000 // runes of a task's text
	planMaxField     = 1000 // runes of a criterion or of a decision's field
	planMaxRef       = 40
	planMaxPattern   = 200
)

// PlanDoc is a plan as a planner hands it in.
type PlanDoc struct {
	Mission   PlannedMission    `json:"mission"`
	Tasks     []PlannedTask     `json:"tasks"`
	Questions []PlannedQuestion `json:"questions"`
	Decisions []PlannedDecision `json:"decisions"`

	// Ignored are the keys the plan named that a plan does not have, as
	// tasks[0].goal: read past, not refused, so a planner's spec that asks
	// for more than the reader uses costs no round.
	Ignored []string `json:"-"`
	// Notes are the slips of form check fixed or dropped, in words a planner
	// can act on: the plan's accepted event records them.
	Notes []string `json:"-"`
}

// PlannedMission is what a plan says about its mission; empty keeps what the
// mission has.
type PlannedMission struct {
	Success      []string `json:"success"`
	OutOfScope   []string `json:"out_of_scope"`
	MaxAttempts  int      `json:"max_attempts"`
	MaxWallHours int      `json:"max_wall_hours"`
}

// PlannedTask is one implementation task of a plan. Core marks it core work
// (core.go): true or false, false when left out; kept as written so that a
// null or another type is refused, not read as false.
type PlannedTask struct {
	Wave      string          `json:"wave,omitempty"`
	Urgent    bool            `json:"urgent,omitempty"`
	Ref       string          `json:"ref"`
	Title     string          `json:"title"`
	Repo      string          `json:"repo"`
	Text      string          `json:"text"`
	Verify    [][]string      `json:"verify"`
	DependsOn []string        `json:"depends_on"`
	Core      json.RawMessage `json:"core,omitempty"`

	// Siblings are the goal's repositories the task's text names beside its
	// checkout (../<repo>) that its repository does not have: check adds
	// them, and the task's spec has them beside the checkout at main.
	Siblings []string `json:"-"`
}

// PlannedQuestion is a decision card for the Owner.
type PlannedQuestion struct {
	Question    string   `json:"question"`
	Body        string   `json:"body"`
	Options     []string `json:"options"`
	Recommended int      `json:"recommended"`
	Meanwhile   string   `json:"meanwhile"`
	OwnerDuty   string   `json:"owner_duty"`
}

// PlannedDecision is a choice the planner made.
type PlannedDecision struct {
	Decision string `json:"decision"`
	Why      string `json:"why"`
	Dropped  string `json:"dropped"`
	Reverse  string `json:"reverse"`
}
