// Package ownercard is the contract between the plan code and the Owner's
// questions: the card the runtime asks the Owner, the card as the question
// store holds it, the ports that ask and withdraw one, and the errors an
// asker reads back. It holds types, constants and sentinel errors only; it
// judges nothing and imports nothing of this module.
//
// Moved out of planstore on 2026-11-19 unchanged (planproposal.go,
// planresult.go, plandecide.go, cardauto.go; planstore names them by alias),
// so the questions' side (agentrt/questionbridge, owner) implements the
// plan's ports without importing the plan, and the plan uses the questions
// without importing the owner (LAW-10: domains talk through
// contracts).
package ownercard

import (
	"context"
	"errors"
	"time"
)

// Ask creates the Owner's decision card for a plan change and returns its
// question id. key makes a retry find the card it created.
type Ask func(ctx context.Context, card Card) (string, error)

// Withdraw withdraws a card the runtime asked, with a reason; a card that is
// already answered, withdrawn or gone is not an error.
type Withdraw func(ctx context.Context, questionID, reason string) error

// Card is a decision card for the Owner. A plan change's names its key,
// title and body only, and gets the two options below; a planner's names its
// own options, the one it goes with when nobody answers (1-based), what
// happens meanwhile, which of the Owner's duties it is and the task that asks
// (planresult.go); a mission's (missiondecide.go) names its options, what
// happens meanwhile, the duty and how soon (Urgency), with no default; a
// blocked task's (blockedcard.go) names its options, the one its rules
// recommend (1-based, nothing done on it unanswered) and why (the
// recommended option's detail in the question store), what happens
// meanwhile, the duty, the task and how soon. Its body may change between a
// try that made it and a retry (it says how long ago its mission started):
// SameByKey lets the store's card under its key stand for it when that card
// names the same task and options (review 439 A3: no other card's
// retry may take a card made for other content).
type Card struct {
	Key, Title, Body             string
	Options                      []string
	Recommended                  int
	RecommendedWhy               string
	Meanwhile, OwnerDuty, TaskID string
	Urgency                      string
	SameByKey                    bool
	// ByRule: a card-auto-answer rule in force answers a card of its kind
	// (RuleAnswers, TD-393), so the question store may take it past its
	// quotas: the rule, not the Owner, answers it within seconds.
	ByRule bool
	// WorkID is a business issue's number (BI-<n>); Link, LinkText its page (issuecard.go).
	WorkID, Link, LinkText string
}

// Options of a plan change's card, as the Owner reads them, and the keys
// the question store gives them in order, which an answer carries (review
// 180 A1): the controller applies the change on the first.
const (
	CardApply     = "Apply the change"
	CardRefuse    = "Do not apply it"
	CardApplyKey  = "a"
	CardRefuseKey = "b"
)

// RuntimeCard is a card the runtime asked, as the question store holds it:
// open, or answered by a rule (By) and not acknowledged yet (Answered).
type RuntimeCard struct {
	ID, TaskID, Recommended string
	CreatedAt               time.Time
	// Options is the keys of the card's options in order, as the question
	// store gave them (a, b, …): a rule under assessment: veto answers only
	// with one of them.
	Options    []string
	Answered   bool
	By, Option string // a rule's answer: the rule and the option it took
}

// ErrAsked: the question store holds this question already, open or
// answered, under another key (a card of an earlier round, or another plan's).
// The Owner has been asked; a plan goes on with its default and does not
// count the card as its own (review 217 B2).
var ErrAsked = errors.New("store: the Owner was already asked this question")

// ErrQuestionGone: the question store no longer has the question (it trims
// read and expired ones); there is nothing left to acknowledge.
var ErrQuestionGone = errors.New("store: the question is gone from the question store")
