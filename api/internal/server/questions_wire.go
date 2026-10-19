package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
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
		return questionFact(q.ID, q.Status, q.Status == questions.StatusAnswered, option, text, by, at)
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
		// A planner's card (W-31 2.2b): its own options and default; the
		// work goes on with the default meanwhile. A mission's card: its own
		// options, no default, and how soon it says (missiondecide.go).
		if len(card.Options) > 0 {
			options := make([]map[string]string, 0, len(card.Options))
			for _, o := range card.Options {
				options = append(options, map[string]string{"label": o})
			}
			req["options"], req["recommended"], req["meanwhile"] = options, card.Recommended, card.Meanwhile
			req["owner_duty"], req["task_id"] = card.OwnerDuty, card.TaskID
			req["urgency"] = "when you can: the work goes on with the recommended option"
			if card.Urgency != "" {
				req["urgency"] = card.Urgency
			}
		}
		raw, err := json.Marshal(req)
		if err != nil {
			return "", err
		}
		res := rt.svc.Create(ctx, agentRuntimeRequester, agentRuntimeRequester, "operator", raw)
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
		case (res.Code != http.StatusCreated && res.Code != http.StatusOK) || id == "":
			return "", fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
		}
		return id, nil
	}
}

// agentRuntimeRequester is the session the runtime asks the Owner as.
const agentRuntimeRequester = "agentrt"

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
