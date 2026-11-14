// Package questioncards adapts the trusted question service to landing and
// STG cards. The server and database integration tests use this same source.
package questioncards

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// Unacked lists the questions the runtime asked that the Owner answered and
// the runtime has not acknowledged (each counts against its quota until it
// is); it writes nothing. nil without a question store.
func Unacked(svc *questions.Service) func(ctx context.Context) ([]store.AnsweredCard, error) {
	if svc == nil {
		return nil
	}
	return func(ctx context.Context) ([]store.AnsweredCard, error) {
		res := svc.List(ctx, questions.RuntimeSession, questions.RuntimeSession, "operator", "unread", true)
		if res.Code != http.StatusOK {
			return nil, fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
		}
		body, _ := res.Body.(map[string]any)
		qs, ok := body["questions"].([]questions.Question)
		if !ok {
			return nil, fmt.Errorf("the question store's list is not a list of questions: %T", body["questions"])
		}
		var out []store.AnsweredCard
		for _, q := range qs {
			if q.Requester != questions.RuntimeSession || q.Status != questions.StatusAnswered || q.Answer == nil || q.Answer.Read != nil {
				continue
			}
			out = append(out, store.AnsweredCard{ID: q.ID, Number: q.Number, TaskID: q.TaskID, AnsweredAt: q.Answer.At,
				ByRule: q.Answer.Channel == questions.RuleChannel})
		}
		return out, nil
	}
}

// Fact converts a trusted question without copying non-runtime free text.
func Fact(q questions.Question) (store.Question, bool) {
	var option, text, by string
	var at time.Time
	if q.Answer != nil {
		option, text, by, at = q.Answer.Option, q.Answer.Text, q.Answer.By, q.Answer.At
	}
	f := store.Question{ID: q.ID, Status: q.Status}
	if q.Status == questions.StatusAnswered {
		if option == "" && text != "" {
			option = "none"
		}
		if option == "" || by == "" || at.IsZero() {
			return store.Question{}, false
		}
		f.Option, f.Text, f.AnsweredBy, f.AnsweredAt = option, text, by, at.UTC().Format(time.RFC3339)
	}
	f.Options, f.ExpiresAt, f.Recommended = len(q.Options), q.ExpiresAt, q.Recommended
	if q.Answer != nil {
		f.Channel = q.Answer.Channel
		// Service.Answer and the answer route require the admin role;
		// rule answers have their own channel. Preserve the login name.
		f.OwnerAnswer = q.Requester == questions.RuntimeSession && q.Status == questions.StatusAnswered &&
			q.Answer.Channel == "console" && q.Answer.By != "" && !q.Answer.Approval
	}
	f.Number, f.OwnerDuty, f.TaskID = q.Number, q.OwnerDuty, q.TaskID
	if !q.CreatedAt.IsZero() {
		f.CreatedAt = q.CreatedAt.UTC().Format(time.RFC3339)
	}
	if q.Withdrawal != nil {
		f.WithdrawnBy = q.Withdrawal.Principal
		if !q.Withdrawal.At.IsZero() {
			f.WithdrawnAt = q.Withdrawal.At.UTC().Format(time.RFC3339)
		}
	}
	if q.Status == questions.StatusExpired && !q.ExpiresAt.IsZero() {
		f.ExpiredAt = q.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if q.Requester != questions.RuntimeSession {
		f.Text = ""
	}
	return f, true
}
