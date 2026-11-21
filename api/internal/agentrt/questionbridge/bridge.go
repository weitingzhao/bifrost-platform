// Package questionbridge is how the agent runtime asks, reads and answers the
// Owner's questions (decoupling item 5, A5, 2026-11-18; it was the server's
// questions_wire.go). The runtime's controller and plan code declare what they
// need as ports in contracts (ownercard.Ask, Withdraw, RuntimeCard;
// seenfacts.Question); this package implements them over the question
// service. Since 2026-11-19 it imports no plan code: it is the owner's side
// of that contract (OWNER_LAW #28). It holds the translation of a plan's
// card into a question and back, the runtime's own session, and the bounded
// reads; the server only constructs it, mounts it and closes it.
package questionbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/ownercard"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questioncards"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/readslot"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/ownerq"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Runtime is the holder of the questions service for the agent runtime. The
// questions sweep is started and stopped by the server, which owns its
// cancellation.
type Runtime struct {
	Svc   *questions.Service
	reads *readslot.Slots // the agent runtime's reads (Question)
	// byRule: a card the runtime asks ByRule (a card-auto-answer rule in
	// force answers its kind) is taken past the question store's quotas
	// (questions.CreateForRule, TD-393). STG's alone.
	byRule atomic.Bool

	cancel context.CancelFunc
	done   chan struct{}
	stop   sync.Once
}

// StartSweep runs the questions sweep until Stop. The runtime owns its
// cancellation.
func (rt *Runtime) StartSweep(interval time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	rt.cancel = cancel
	rt.done = make(chan struct{})
	safego.Go("questions.sweep", func() {
		defer close(rt.done)
		rt.Svc.RunSweep(ctx, interval)
	})
}

// Stop stops the questions sweep and waits for it.
func (rt *Runtime) Stop() {
	rt.stop.Do(func() {
		if rt.cancel != nil {
			rt.cancel()
		}
		if rt.done != nil {
			<-rt.done
		}
	})
}

// TakeCardsByRule says whether this platform takes the runtime's cards a
// rule answers past the question store's quotas.
func (rt *Runtime) TakeCardsByRule(on bool) {
	if rt != nil {
		rt.byRule.Store(on)
	}
}

// New makes the runtime over a question store at path.
func New(path string, audit *actuation.AuditLog) *Runtime {
	return &Runtime{Svc: questions.New(path, audit), reads: readslot.New(2)}
}

// Question reads a question for the agent runtime's controller
// (agentRuntimeSources) by Lookup, which writes nothing; nil without a
// question store.
func (rt *Runtime) Question() func(context.Context, string) (seenfacts.Question, bool) {
	if rt == nil || rt.Svc == nil || rt.reads == nil {
		return nil
	}
	return func(ctx context.Context, id string) (seenfacts.Question, bool) {
		q, ok, err := readslot.Within(ctx, rt.reads, func() (questions.Question, bool, error) { return rt.Svc.Lookup(id) })
		if err != nil || !ok {
			return seenfacts.Question{}, false
		}
		return questioncards.Fact(q)
	}
}

// Ask is how the agent runtime puts a plan change outside the
// standing authorization to the Owner (design v5 §5.2 POST /plan/proposals):
// a decision card of the runtime's own (session agentrt), owner_duty goal,
// two options the controller reads back (apply, do not apply), keyed by the
// proposal so a retry finds the same card. A planner's card and a mission's
// card name their own options; only a planner's has a default and a task.
// nil without a question store.
func (rt *Runtime) Ask() ownercard.Ask {
	if rt == nil || rt.Svc == nil {
		return nil
	}
	return func(ctx context.Context, card ownercard.Card) (string, error) {
		req := map[string]any{
			"title":           questions.ClipTitle(card.Title),
			"body":            card.Body,
			"options":         []map[string]string{{"label": ownercard.CardApply}, {"label": ownercard.CardRefuse}},
			"urgency":         "when you can: only this change waits for you",
			"meanwhile":       "the plan stays as it is",
			"owner_duty":      "goal",
			"idempotency_key": card.Key,
		}
		// A plan change's card recommends applying it when its rules say so
		// (TD-397 F-T33-rec: the catalog's first gates, gatesRecommend), with
		// why beside that option; STG's rule takes it, PROD's Owner reads it.
		if card.Recommended > 0 && len(card.Options) == 0 {
			options := []map[string]string{{"label": ownercard.CardApply}, {"label": ownercard.CardRefuse}}
			if card.Recommended <= len(options) && card.RecommendedWhy != "" {
				options[card.Recommended-1]["detail"] = card.RecommendedWhy
			}
			req["options"], req["recommended"] = options, card.Recommended
		}
		// A planner's card (W-31 2.2b): its own options and default; the
		// work goes on with the default meanwhile. A mission's card: its own
		// options, no default, and how soon it says (missiondecide.go).
		if len(card.Options) > 0 {
			options := make([]map[string]string, 0, len(card.Options))
			for i, o := range card.Options {
				opt := map[string]string{"label": o}
				// A blocked task's card says why its rules recommend an option
				// (blockedrecommend.go): the option's detail, beside it.
				if i == card.Recommended-1 && card.RecommendedWhy != "" {
					opt["detail"] = card.RecommendedWhy
				}
				options = append(options, opt)
			}
			req["options"], req["recommended"], req["meanwhile"] = options, card.Recommended, card.Meanwhile
			req["owner_duty"], req["task_id"] = card.OwnerDuty, card.TaskID
			req["urgency"] = "when you can: the work goes on with the recommended option"
			if card.Urgency != "" {
				req["urgency"] = card.Urgency
			}
		}
		if card.WorkID != "" {
			req["work_id"] = card.WorkID
			if card.Link != "" {
				req["link"], req["link_text"] = card.Link, card.LinkText
			}
		}
		raw, err := json.Marshal(req)
		if err != nil {
			return "", err
		}
		create := rt.Svc.Create
		if card.ByRule && rt.byRule.Load() {
			// STG's rule answers it within seconds; the quotas guard the
			// Owner from what waits for him (TD-393).
			create = rt.Svc.CreateForRule
		}
		res := create(ctx, requester, requester, "operator", raw)
		body, _ := res.Body.(map[string]any)
		id, _ := body["id"].(string)
		msg, _ := body["error"].(string)
		switch {
		case res.Code == http.StatusBadRequest:
			// The card itself does not fit: retrying will not help.
			return "", fmt.Errorf("%w: the question store refused the card: %v", eventlog.ErrInvalid, res.Body)
		case card.TaskID != "" && res.Code == http.StatusConflict &&
			(strings.HasPrefix(msg, "already answered: ") || strings.HasPrefix(msg, "duplicate of ")):
			// A planner's question the store holds under another key, open or
			// answered (review 217 B2): the Owner has it; the plan goes on.
			return "", fmt.Errorf("%w: %s", ownercard.ErrAsked, msg)
		case card.SameByKey && res.Code == http.StatusConflict && msg == questions.KeyReused:
			// A blocked card made by an earlier try whose transaction then
			// failed, in words that have changed since (it says how long ago
			// its mission started): the card under its key is this card when
			// it names the same task and options (review 439 A3).
			q, ok, lerr := rt.Svc.LookupKey(requester, card.Key)
			if lerr != nil || !ok || !sameCard(q, card) {
				return "", fmt.Errorf("the question store answered %d: %v (the card under its key is not this one: %v)", res.Code, res.Body, lerr)
			}
			return q.ID, nil
		case len(card.Options) == 0 && res.Code == http.StatusConflict && msg == questions.KeyReused:
			// A plan change's card made by an earlier try whose transaction
			// then failed, when what the runtime recommends has changed since
			// (the plan moved; review td397-rec-1 A1): the card under its key
			// is this card when it asks the same in all else. Whether a
			// recommendation still holds is read again where a rule's answer
			// is used (gatesStillRecommended).
			q, ok, lerr := rt.Svc.LookupKey(requester, card.Key)
			if lerr != nil || !ok || !sameAsked(q, raw) {
				return "", fmt.Errorf("the question store answered %d: %v (the card under its key is not this one: %v)", res.Code, res.Body, lerr)
			}
			return q.ID, nil
		case (res.Code != http.StatusCreated && res.Code != http.StatusOK) || id == "":
			return "", fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
		}
		return id, nil
	}
}

// Cards lists the runtime's cards for its card-auto-answer rule
// (TD-390): the open ones and those its rule answered that it has not
// acknowledged, by RuleCards, which writes nothing; nil without a question
// store.
func (rt *Runtime) Cards() func(context.Context) ([]ownercard.RuntimeCard, error) {
	if rt == nil || rt.Svc == nil || rt.reads == nil {
		return nil
	}
	return func(ctx context.Context) ([]ownercard.RuntimeCard, error) {
		type r struct {
			qs  []questions.Question
			err error
		}
		x, ok, err := readslot.Within(ctx, rt.reads, func() (r, bool, error) {
			qs, err := rt.Svc.RuleCards(requester)
			return r{qs, err}, true, nil
		})
		if err != nil || !ok {
			return nil, fmt.Errorf("the question store did not answer: %v", err)
		}
		if x.err != nil {
			return nil, x.err
		}
		cards := make([]ownercard.RuntimeCard, 0, len(x.qs))
		for _, q := range x.qs {
			c := ownercard.RuntimeCard{ID: q.ID, TaskID: q.TaskID, Recommended: q.Recommended, CreatedAt: q.CreatedAt}
			for _, o := range q.Options {
				c.Options = append(c.Options, o.Key)
			}
			if q.Status == questions.StatusAnswered && q.Answer != nil {
				c.Answered, c.By, c.Option = true, q.Answer.By, q.Answer.Option
			}
			cards = append(cards, c)
		}
		return cards, nil
	}
}

// RuleAnswer answers one of the runtime's cards as its rule
// while the rule holds (questions.AnswerAsRule: the option the card
// recommends, or another with why, by the rule, on the rule channel); nil
// without a question store.
func (rt *Runtime) RuleAnswer() func(ctx context.Context, id, option, rule string, until time.Time, why string) error {
	if rt == nil || rt.Svc == nil {
		return nil
	}
	return func(ctx context.Context, id, option, rule string, until time.Time, why string) error {
		res := rt.Svc.AnswerAsRule(ctx, requester, rule, id, option, until, why)
		if res.Code != http.StatusOK {
			return fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
		}
		return nil
	}
}

// sameCard says whether a question the store holds is the card c: the same
// task and option labels (a blocked card's third names its mission). The
// title is not compared: the store cleans it, and it is the task's anyway.
func sameCard(q questions.Question, c ownercard.Card) bool {
	if q.TaskID != c.TaskID || len(q.Options) != len(c.Options) {
		return false
	}
	for i, o := range q.Options {
		if o.Label != c.Options[i] {
			return false
		}
	}
	return true
}

// sameAsked says whether a question the store holds asks what the request
// raw asks, as the store cleans it: the same title, body, option labels,
// urgency, meanwhile, duty and task. Only the recommendation and the
// options' details may differ.
func sameAsked(q questions.Question, raw []byte) bool {
	c, err := questions.CanonicalizeRequestJSON(raw)
	if err != nil || q.Title != c.Title || q.Body != c.Body || q.Urgency != c.Urgency || q.Meanwhile != c.Meanwhile ||
		q.OwnerDuty != c.OwnerDuty || q.TaskID != c.TaskID || len(q.Options) != len(c.Options) {
		return false
	}
	for i, o := range q.Options {
		if o.Label != c.Options[i].Label {
			return false
		}
	}
	return true
}

// requester is the session the runtime asks the Owner as, which
// no route may name (questions.RuntimeSession).
const requester = questions.RuntimeSession

// Withdraw withdraws, as the runtime, a card a plan asked whose
// round was refused (W-31 2.2b, review 216 B2). A card that is no longer
// open (answered, withdrawn) or gone needs nothing. nil without a question
// store.
func (rt *Runtime) Withdraw() ownercard.Withdraw {
	if rt == nil || rt.Svc == nil {
		return nil
	}
	return func(ctx context.Context, id, reason string) error {
		raw, err := json.Marshal(map[string]string{"reason": reason})
		if err != nil {
			return err
		}
		res := rt.Svc.Withdraw(ctx, requester, requester, "operator", id, raw)
		switch res.Code {
		case http.StatusOK, http.StatusConflict, http.StatusNotFound:
			return nil
		}
		return fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
	}
}

// Ack acknowledges, as the runtime, the answer of a card it
// asked: an answered card nobody acknowledged keeps counting against the
// runtime's quota (review 180 B3). nil without a question store.
func (rt *Runtime) Ack() func(context.Context, string) error {
	if rt == nil || rt.Svc == nil {
		return nil
	}
	return func(ctx context.Context, id string) error {
		res := rt.Svc.Ack(ctx, requester, requester, "operator", id, nil)
		switch res.Code {
		case http.StatusOK:
			return nil
		case http.StatusNotFound:
			return fmt.Errorf("%w: %s", ownercard.ErrQuestionGone, id)
		}
		return fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
	}
}

// Unacked lists the runtime's answered cards it has not
// acknowledged (questioncards.Unacked), for the controller to acknowledge
// those no task waits on; nil without a question store.
func (rt *Runtime) Unacked() func(context.Context) ([]questioncards.AnsweredCard, error) {
	if rt == nil {
		return nil
	}
	return questioncards.Unacked(rt.Svc)
}

// Observations lists retained cards without sweeping.
func (rt *Runtime) Observations() func(context.Context) ([]seenfacts.Question, error) {
	if rt == nil || rt.Svc == nil || rt.reads == nil {
		return nil
	}
	return func(ctx context.Context) ([]seenfacts.Question, error) {
		items, ok, err := readslot.Within(ctx, rt.reads, func() ([]questions.Question, bool, error) {
			items, err := rt.Svc.Observations()
			return items, true, err
		})
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("question observations unavailable")
		}
		now := time.Now()
		var out []seenfacts.Question
		for _, q := range items {
			if f, ok := questioncards.Observation(q, now); ok {
				out = append(out, f)
			}
		}
		return out, nil
	}
}

// Announcer hands the questions' transitions to the bus: it writes the
// Owner's questions' transitions (bus/ownerq), in the background, as the
// cards' announcer does for the cards. db is read per call: nil means the bus
// is not connected and nothing is written.
func Announcer(db func() eventlog.DB) func([]questions.Transition) {
	return func(ts []questions.Transition) {
		d := db()
		if d == nil {
			return
		}
		list := make([]ownerq.Question, 0, len(ts))
		for _, t := range ts {
			if q := (ownerq.Question{Type: ownerq.TypeOf(t.AuditAction), ID: t.ID, Status: t.Status}); ownerq.Valid(q) {
				list = append(list, q)
			}
		}
		if len(list) == 0 {
			return
		}
		safego.Go("bus.questions", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := ownerq.Announce(ctx, d, list); err != nil {
				slog.Warn("bus: questions not announced", "error", err, "count", len(list))
			}
		})
	}
}
