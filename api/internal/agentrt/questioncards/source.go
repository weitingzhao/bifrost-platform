// Package questioncards adapts the trusted question service to landing and
// STG cards. The server and database integration tests use this same source.
package questioncards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// New is the landing cards' own reach into the question
// store (S3 B3, CUT §11.6 v3 item 5): the frozen request asked as stored
// (72 hours, free text allowed, options in order), a key lookup that writes
// nothing, a read through the store's sweep (an expired open card reads
// expired), a withdrawal that says what became of the card, and an Ack that
// tells a gone card from a failure. No SameByKey fallback: a key that holds
// another request is an error. Zero without a question store.
func New(svc *questions.Service) store.CardQuestions {
	if svc == nil {
		return store.CardQuestions{}
	}
	toFact := func(q questions.Question) store.Question {
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
				return store.Question{}
			}
			f.Option, f.Text, f.AnsweredBy, f.AnsweredAt = option, text, by, at.UTC().Format(time.RFC3339)
		}
		f.Options, f.ExpiresAt = len(q.Options), q.ExpiresAt
		if q.Answer != nil {
			f.Channel = q.Answer.Channel
			// Service.Answer and the answer route require the admin role;
			// rule answers have their own channel. Preserve the login name.
			f.OwnerAnswer = q.Requester == questions.RuntimeSession && q.Status == questions.StatusAnswered &&
				q.Answer.Channel == "console" && q.Answer.By != "" && !q.Answer.Approval
		}
		return f
	}
	get := func(ctx context.Context, id string) (questions.Question, bool, error) {
		res := svc.Get(ctx, questions.RuntimeSession, questions.RuntimeSession, "operator", id)
		switch res.Code {
		case http.StatusOK:
		case http.StatusNotFound:
			return questions.Question{}, false, nil
		default:
			return questions.Question{}, false, fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
		}
		q, ok := res.Body.(questions.Question)
		if !ok {
			raw, err := json.Marshal(res.Body)
			if err != nil {
				return questions.Question{}, false, err
			}
			if err := json.Unmarshal(raw, &q); err != nil {
				return questions.Question{}, false, err
			}
		}
		return q, true, nil
	}
	// wire is the create body a frozen request makes: the same bytes on
	// every ask, so the store's fingerprint of it is the card's.
	wire := func(key string, request map[string]any) ([]byte, error) {
		req := map[string]any{}
		for k, v := range request {
			req[k] = v
		}
		labels, _ := request["options"].([]any)
		opts := make([]map[string]string, 0, len(labels))
		for _, l := range labels {
			opts = append(opts, map[string]string{"label": fmt.Sprint(l)})
		}
		req["options"], req["idempotency_key"] = opts, key
		if t, ok := req["title"].(string); ok {
			req["title"] = questions.ClipTitle(t)
		}
		return json.Marshal(req)
	}
	return store.CardQuestions{
		AskRaw: func(ctx context.Context, key string, request map[string]any) (string, time.Time, error) {
			raw, err := wire(key, request)
			if err != nil {
				return "", time.Time{}, err
			}
			res := svc.Create(ctx, questions.RuntimeSession, questions.RuntimeSession, "operator", raw)
			body, _ := res.Body.(map[string]any)
			id, _ := body["id"].(string)
			switch {
			case res.Code == http.StatusBadRequest:
				return "", time.Time{}, fmt.Errorf("%w: the question store refused the landing card: %v", store.ErrInvalid, res.Body)
			case res.Code == http.StatusTooManyRequests || res.Code == http.StatusInsufficientStorage:
				// Quota or capacity: both back the asks off (review 672 R-04).
				return "", time.Time{}, fmt.Errorf("%w: %v", store.ErrCardQuota, res.Body)
			case (res.Code != http.StatusCreated && res.Code != http.StatusOK) || id == "":
				return "", time.Time{}, fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
			}
			q, found, err := get(ctx, id)
			if err != nil || !found {
				return id, time.Time{}, err
			}
			return id, q.CreatedAt, nil
		},
		LookupKey: func(_ context.Context, key string, request map[string]any) (store.Question, time.Time, bool, error) {
			q, ok, err := svc.LookupKey(questions.RuntimeSession, key)
			if err != nil || !ok {
				return store.Question{}, time.Time{}, false, err
			}
			// The question under the key must ask exactly this request
			// (review 670 B-01): the store's own fingerprint of it.
			raw, err := wire(key, request)
			if err != nil {
				return store.Question{}, time.Time{}, false, err
			}
			c, err := questions.CanonicalizeRequestJSON(raw)
			if err != nil {
				return store.Question{}, time.Time{}, false, err
			}
			if q.RequestFP != c.RequestFP || q.Requester != questions.RuntimeSession {
				return store.Question{}, time.Time{}, false, fmt.Errorf("%w: %s", store.ErrCardMismatch, q.ID)
			}
			return toFact(q), q.CreatedAt, true, nil
		},
		Lookup: func(ctx context.Context, id string) (store.Question, bool, error) {
			q, found, err := get(ctx, id)
			if err != nil || !found {
				return store.Question{}, false, err
			}
			return toFact(q), true, nil
		},
		Withdraw: func(ctx context.Context, id, reason string) (string, error) {
			raw, err := json.Marshal(map[string]string{"reason": reason})
			if err != nil {
				return "", err
			}
			res := svc.Withdraw(ctx, questions.RuntimeSession, questions.RuntimeSession, "operator", id, raw)
			switch res.Code {
			case http.StatusOK:
				return questions.StatusWithdrawn, nil
			case http.StatusNotFound:
				return "gone", nil
			case http.StatusConflict:
				// Not open any more: what it became is read, never assumed.
				q, found, err := get(ctx, id)
				if err != nil {
					return "", err
				}
				if !found {
					return "gone", nil
				}
				return q.Status, nil
			}
			return "", fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
		},
		Ack: func(ctx context.Context, id string) (bool, error) {
			res := svc.Ack(ctx, questions.RuntimeSession, questions.RuntimeSession, "operator", id, nil)
			switch res.Code {
			case http.StatusOK:
				return true, nil
			case http.StatusNotFound:
				return false, nil
			}
			return false, fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
		},
	}
}
