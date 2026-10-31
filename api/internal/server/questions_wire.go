package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questioncards"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// questionRuntime is the only server-package holder of the questions service.
// This sweep used to fire immediately; the runtime owns its cancellation.
type questionRuntime struct {
	svc    *questions.Service
	reads  *boundedReads // the agent runtime's reads (agentRuntimeQuestion)
	cancel context.CancelFunc
	done   chan struct{}
	stop   sync.Once
	// byRule: a card the runtime asks ByRule (a card-auto-answer rule in
	// force answers its kind) is taken past the question store's quotas
	// (questions.CreateForRule, TD-393). STG's alone (agentrt_wire.go).
	byRule atomic.Bool
}

// takeCardsByRule says whether this platform takes the runtime's cards a
// rule answers past the question store's quotas.
func (rt *questionRuntime) takeCardsByRule(on bool) {
	if rt != nil {
		rt.byRule.Store(on)
	}
}

func newQuestionRuntime(path string, audit *actuation.AuditLog) *questionRuntime {
	return &questionRuntime{svc: questions.New(path, audit), reads: newBoundedReads(2)}
}

func (s *Server) wireQuestions(dataDir string, workers bool) {
	rt := newQuestionRuntime(filepath.Join(dataDir, "questions"), s.audit)
	svc := rt.svc
	s.questionRuntime = rt
	if !workers || svc == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	rt.cancel = cancel
	rt.done = make(chan struct{})
	safego.Go("questions.sweep", func() {
		defer close(rt.done)
		svc.RunSweep(ctx, approvalRetryInterval)
	})
}

// Close cancels approval result flushing without waiting on storage I/O,
// then stops the questions sweep. Other background loops are unchanged.
func (s *Server) Close() {
	s.closeAgentRuntime()
	if s.approvalsCancel != nil {
		s.approvalsCancel()
	}
	if s.approvals != nil {
		// A result held by this process gets one last write before it exits.
		s.approvals.FlushHeldFinishes()
	}
	rt := s.questionRuntime
	if rt == nil {
		return
	}
	rt.stop.Do(func() {
		if rt.cancel != nil {
			rt.cancel()
		}
		if rt.done != nil {
			<-rt.done
		}
	})
}

func (s *Server) mountQuestions(r chi.Router) {
	if s.questionRuntime == nil {
		return
	}
	questions.Mount(r, s.auth, s.questionRuntime.svc)
}

// agentRuntimeQuestion reads a question for the agent runtime's controller
// (agentRuntimeSources) by Lookup, which writes nothing; nil without a
// question store.
func (rt *questionRuntime) agentRuntimeQuestion() func(context.Context, string) (store.Question, bool) {
	if rt == nil || rt.svc == nil || rt.reads == nil {
		return nil
	}
	return func(ctx context.Context, id string) (store.Question, bool) {
		q, ok, err := within(ctx, rt.reads, func() (questions.Question, bool, error) { return rt.svc.Lookup(id) })
		if err != nil || !ok {
			return store.Question{}, false
		}
		var option, text, by string
		var at time.Time
		if q.Answer != nil {
			option, text, by, at = q.Answer.Option, q.Answer.Text, q.Answer.By, q.Answer.At
		}
		f, ok := questionFact(q.ID, q.Status, q.Status == questions.StatusAnswered, option, text, by, at)
		f.Options, f.ExpiresAt = len(q.Options), q.ExpiresAt
		if q.Answer != nil {
			f.Channel = q.Answer.Channel
		}
		return f, ok
	}
}

// agentRuntimeAsk is how the agent runtime puts a plan change outside the
// standing authorization to the Owner (design v5 §5.2 POST /plan/proposals):
// a decision card of the runtime's own (session agentrt), owner_duty goal,
// two options the controller reads back (apply, do not apply), keyed by the
// proposal so a retry finds the same card. A planner's card and a mission's
// card name their own options; only a planner's has a default and a task.
// nil without a question store.
func (rt *questionRuntime) agentRuntimeAsk() store.Ask {
	if rt == nil || rt.svc == nil {
		return nil
	}
	return func(ctx context.Context, card store.Card) (string, error) {
		req := map[string]any{
			"title":           questions.ClipTitle(card.Title),
			"body":            card.Body,
			"options":         []map[string]string{{"label": store.CardApply}, {"label": store.CardRefuse}},
			"urgency":         "when you can: only this change waits for you",
			"meanwhile":       "the plan stays as it is",
			"owner_duty":      "goal",
			"idempotency_key": card.Key,
		}
		// A plan change's card recommends applying it when its rules say so
		// (TD-397 F-T33-rec: the catalog's first gates, gatesRecommend), with
		// why beside that option; STG's rule takes it, PROD's Owner reads it.
		if card.Recommended > 0 && len(card.Options) == 0 {
			options := []map[string]string{{"label": store.CardApply}, {"label": store.CardRefuse}}
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
		// A business issue's card (W-31 BI-1 slice 2, store/issuecard.go):
		// its number as the work id, so the question store takes options
		// only on it, and its page as the link.
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
		create := rt.svc.Create
		if card.ByRule && rt.byRule.Load() {
			// STG's rule answers it within seconds; the quotas guard the
			// Owner from what waits for him (TD-393).
			create = rt.svc.CreateForRule
		}
		res := create(ctx, agentRuntimeRequester, agentRuntimeRequester, "operator", raw)
		body, _ := res.Body.(map[string]any)
		id, _ := body["id"].(string)
		msg, _ := body["error"].(string)
		switch {
		case res.Code == http.StatusBadRequest:
			// The card itself does not fit: retrying will not help.
			return "", fmt.Errorf("%w: the question store refused the card: %v", store.ErrInvalid, res.Body)
		case card.TaskID != "" && res.Code == http.StatusConflict &&
			(strings.HasPrefix(msg, "already answered: ") || strings.HasPrefix(msg, "duplicate of ")):
			// A planner's question the store holds under another key, open or
			// answered (review 217 B2): the Owner has it; the plan goes on.
			return "", fmt.Errorf("%w: %s", store.ErrAsked, msg)
		case card.SameByKey && res.Code == http.StatusConflict && msg == questions.KeyReused:
			// A blocked card made by an earlier try whose transaction then
			// failed, in words that have changed since (it says how long ago
			// its mission started): the card under its key is this card when
			// it names the same task and options (review 439 A3).
			q, ok, lerr := rt.svc.LookupKey(agentRuntimeRequester, card.Key)
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
			q, ok, lerr := rt.svc.LookupKey(agentRuntimeRequester, card.Key)
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

// agentRuntimeCards lists the runtime's cards for its card-auto-answer rule
// (TD-390): the open ones and those its rule answered that it has not
// acknowledged, by RuleCards, which writes nothing; nil without a question
// store.
func (rt *questionRuntime) agentRuntimeCards() func(context.Context) ([]store.RuntimeCard, error) {
	if rt == nil || rt.svc == nil || rt.reads == nil {
		return nil
	}
	return func(ctx context.Context) ([]store.RuntimeCard, error) {
		type r struct {
			qs  []questions.Question
			err error
		}
		x, ok, err := within(ctx, rt.reads, func() (r, bool, error) {
			qs, err := rt.svc.RuleCards(agentRuntimeRequester)
			return r{qs, err}, true, nil
		})
		if err != nil || !ok {
			return nil, fmt.Errorf("the question store did not answer: %v", err)
		}
		if x.err != nil {
			return nil, x.err
		}
		cards := make([]store.RuntimeCard, 0, len(x.qs))
		for _, q := range x.qs {
			if questions.BusinessIssueCard(q) {
				continue // the Owner's alone: no rule answers it (DESIGN-crew-intake §5.3)
			}
			c := store.RuntimeCard{ID: q.ID, TaskID: q.TaskID, Recommended: q.Recommended, CreatedAt: q.CreatedAt}
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

// agentRuntimeOpenCards lists the runtime's open cards as their
// assessments read them (TD-391): title, body, options with their details,
// the option each recommends; by RuleCards, which writes nothing. nil without
// a question store.
func (rt *questionRuntime) agentRuntimeOpenCards() func(context.Context) ([]store.AssessItem, error) {
	if rt == nil || rt.svc == nil || rt.reads == nil {
		return nil
	}
	return func(ctx context.Context) ([]store.AssessItem, error) {
		type r struct {
			qs  []questions.Question
			err error
		}
		x, ok, err := within(ctx, rt.reads, func() (r, bool, error) {
			qs, err := rt.svc.RuleCards(agentRuntimeRequester)
			return r{qs, err}, true, nil
		})
		if err != nil || !ok {
			return nil, fmt.Errorf("the question store did not answer: %v", err)
		}
		if x.err != nil {
			return nil, x.err
		}
		var items []store.AssessItem
		for _, q := range x.qs {
			// A business issue's card is not assessed (DESIGN-crew-intake
			// §5.3): nothing of it becomes an assessment's task_spec.
			if q.Status != questions.StatusOpen || questions.BusinessIssueCard(q) {
				continue
			}
			it := store.AssessItem{Kind: store.AssessCard, ID: q.ID, Title: q.Title, Body: q.Body, Recommended: q.Recommended,
				TaskID: q.TaskID, CreatedAt: q.CreatedAt}
			for _, o := range q.Options {
				it.Options = append(it.Options, store.AssessOption{Key: o.Key, Label: o.Label, Detail: o.Detail})
			}
			items = append(items, it)
		}
		return items, nil
	}
}

// agentRuntimeRuleAnswer answers one of the runtime's cards as its rule
// while the rule holds (questions.AnswerAsRule: the option the card
// recommends, or another with why, by the rule, on the rule channel); nil
// without a question store.
func (rt *questionRuntime) agentRuntimeRuleAnswer() func(ctx context.Context, id, option, rule string, until time.Time, why string) error {
	if rt == nil || rt.svc == nil {
		return nil
	}
	return func(ctx context.Context, id, option, rule string, until time.Time, why string) error {
		res := rt.svc.AnswerAsRule(ctx, agentRuntimeRequester, rule, id, option, until, why)
		if res.Code != http.StatusOK {
			return fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
		}
		return nil
	}
}

// sameCard says whether a question the store holds is the card c: the same
// task and option labels (a blocked card's third names its mission). The
// title is not compared: the store cleans it, and it is the task's anyway.
func sameCard(q questions.Question, c store.Card) bool {
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

// agentRuntimeRequester is the session the runtime asks the Owner as, which
// no route may name (questions.RuntimeSession).
const agentRuntimeRequester = questions.RuntimeSession

// agentRuntimeWithdraw withdraws, as the runtime, a card a plan asked whose
// round was refused (W-31 2.2b, review 216 B2). A card that is no longer
// open (answered, withdrawn) or gone needs nothing. nil without a question
// store.
func (rt *questionRuntime) agentRuntimeWithdraw() store.Withdraw {
	if rt == nil || rt.svc == nil {
		return nil
	}
	return func(ctx context.Context, id, reason string) error {
		raw, err := json.Marshal(map[string]string{"reason": reason})
		if err != nil {
			return err
		}
		res := rt.svc.Withdraw(ctx, agentRuntimeRequester, agentRuntimeRequester, "operator", id, raw)
		switch res.Code {
		case http.StatusOK, http.StatusConflict, http.StatusNotFound:
			return nil
		}
		return fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
	}
}

// agentRuntimeAck acknowledges, as the runtime, the answer of a card it
// asked: an answered card nobody acknowledged keeps counting against the
// runtime's quota (review 180 B3). nil without a question store.
func (rt *questionRuntime) agentRuntimeAck() func(context.Context, string) error {
	if rt == nil || rt.svc == nil {
		return nil
	}
	return func(ctx context.Context, id string) error {
		res := rt.svc.Ack(ctx, agentRuntimeRequester, agentRuntimeRequester, "operator", id, nil)
		switch res.Code {
		case http.StatusOK:
			return nil
		case http.StatusNotFound:
			return fmt.Errorf("%w: %s", store.ErrQuestionGone, id)
		}
		return fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
	}
}

// landingCardQuestions uses the shared trusted question-store adapter.
func (rt *questionRuntime) landingCardQuestions() store.CardQuestions {
	if rt == nil {
		return store.CardQuestions{}
	}
	return questioncards.New(rt.svc)
}

func (s *Server) wireQuestionAssessments() {
	if s.questionRuntime != nil && s.questionRuntime.svc != nil {
		s.questionRuntime.svc.Assessment = s.assessmentReader(store.AssessCard)
	}
}

func (rt *questionRuntime) checkAssessmentCard(ctx context.Context, id string) error {
	if rt == nil || rt.svc == nil || rt.reads == nil {
		return fmt.Errorf("question store is unavailable")
	}
	q, found, err := within(ctx, rt.reads, func() (questions.Question, bool, error) { return rt.svc.Lookup(id) })
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: question does not exist", pgx.ErrNoRows)
	}
	if q.ID != id {
		return fmt.Errorf("%w: use the question id", store.ErrState)
	}
	if q.Status != questions.StatusOpen || !time.Now().Before(q.ExpiresAt) {
		return fmt.Errorf("%w: question is %s or expired; assessment requires an open question", store.ErrState, q.Status)
	}
	if q.Requester != agentRuntimeRequester || questions.BusinessIssueCard(q) {
		return fmt.Errorf("%w: assessments are available for runtime decision cards only", store.ErrState)
	}
	return nil
}
