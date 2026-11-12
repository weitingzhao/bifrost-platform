package questions

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalhome"
)

func (s *Service) Create(ctx context.Context, session, principal, role string, raw []byte) Result {
	return s.create(ctx, session, principal, role, raw, false)
}

// CreateForRule creates a card the agent runtime asks that a card-auto-answer
// rule in force answers within seconds (TD-393, STG only: the server takes
// this path only there): the store's capacity does not hold it, so the
// runtime's cards are not held while the rule, not the Owner, answers them.
// While it is open it still counts in the store's capacity (20 open) for
// every other question. Only the runtime's own session may; everything else
// (the key, a duplicate, the size) is as Create.
func (s *Service) CreateForRule(ctx context.Context, session, principal, role string, raw []byte) Result {
	if session != RuntimeSession {
		return fail(http.StatusForbidden, "only the agent runtime asks past the quotas, for cards its rule answers", nil)
	}
	return s.create(ctx, session, principal, role, raw, true)
}

func (s *Service) create(ctx context.Context, session, principal, role string, raw []byte, byRule bool) Result {
	// Only the internal runtime rule path is non-human; HTTP cannot set byRule.
	if !byRule && !approvalhome.IsProd() {
		return fail(http.StatusForbidden, approvalhome.Required, nil)
	}
	if err := ValidSession(session); err != nil {
		return fail(http.StatusBadRequest, err.Error(), nil)
	}
	canon, err := CanonicalizeRequestJSON(raw)
	if err != nil {
		return fail(http.StatusBadRequest, err.Error(), nil)
	}
	if businessIssue(canon.WorkID) && session != RuntimeSession {
		// A business issue's card is the runtime's, by its template, and
		// carries no text of the issue (issuecard.go); a thread's ask_owner
		// names no issue.
		return fail(http.StatusBadRequest, "work_id is invalid: a business issue's card is asked by the agent runtime only", nil)
	}
	c := caller{session: session, principal: clipPrincipal(principal), role: role, op: "create"}
	return s.apply(ctx, c, func(doc *document, now time.Time) applied {
		if prev, ok := protectedByKey(doc, session, canon.Key, now); ok {
			if prev.RequestFP != canon.RequestFP {
				return applied{res: fail(http.StatusConflict, KeyReused, nil)}
			}
			return applied{res: Result{Code: http.StatusOK, Body: createBody(prev, true)}}
		}
		if dup, kind, ok := protectedDuplicate(doc, session, canon.WorkID, canon.QuestionFP, now); ok {
			if kind == StatusOpen {
				return applied{res: fail(http.StatusConflict, "duplicate of "+numberLabel(dup.Number), nil)}
			}
			return applied{res: fail(http.StatusConflict, "already answered: "+numberLabel(dup.Number), nil)}
		}
		// The store's capacity is the only count that holds a new question:
		// the hourly and per-requester quotas were removed in cleanup wave 1
		// (2026-11-11). Answers nobody acknowledged never count.
		if !byRule && openCount(doc) >= openGlobal {
			return applied{res: fail(http.StatusTooManyRequests, "store full: 20 questions are open", nil)}
		}
		id, err := s.freshID(doc)
		if err != nil {
			return applied{res: fail(http.StatusInternalServerError, "store questions: "+err.Error(), nil)}
		}
		q := Question{
			ID:                 id,
			Number:             doc.nextNumber(),
			Status:             StatusOpen,
			Requester:          session,
			RequesterPrincipal: c.principal,
			RequesterThread:    canon.Thread,
			WorkID:             canon.WorkID,
			OwnerDuty:          canon.OwnerDuty,
			TaskID:             canon.TaskID,
			Title:              canon.Title,
			Body:               canon.Body,
			Options:            canon.Options,
			Recommended:        canon.Recommended,
			AllowText:          canon.AllowText,
			Urgency:            canon.Urgency,
			Meanwhile:          canon.Meanwhile,
			Link:               canon.Link,
			LinkText:           canon.LinkText,
			CreatedAt:          now,
			ExpiresAt:          now.Add(time.Duration(canon.TTLHours) * time.Hour),
			IdempotencyKey:     canon.Key,
			RequestFP:          canon.RequestFP,
			QuestionFP:         canon.QuestionFP,
		}
		if n, err := recordBytes(q); err != nil {
			return applied{res: fail(http.StatusInternalServerError, "store questions: "+err.Error(), nil)}
		} else if n > maxRecordBytes {
			return applied{res: fail(http.StatusBadRequest, "question exceeds 16384 bytes", nil)}
		}
		doc.Questions = append(doc.Questions, q)
		cp := q.clone()
		return applied{
			res:     Result{Code: http.StatusCreated, Body: createBody(q, false)},
			created: &cp,
			changed: true,
			extra:   []event{{"question.create", q.ID, StatusOpen, "number=" + itoa(q.Number)}},
		}
	})
}

func (s *Service) freshID(doc *document) (string, error) {
	for i := 0; i < 5; i++ {
		id, err := s.newID()
		if err != nil {
			return "", err
		}
		if _, ok := doc.find(id); !ok {
			return id, nil
		}
	}
	return "", errors.New("could not allocate an id")
}

func createBody(q Question, replayed bool) map[string]any {
	body := map[string]any{
		"id":          q.ID,
		"number":      q.Number,
		"status":      q.Status,
		"expires_at":  q.ExpiresAt,
		"console_url": consoleURL(q.ID),
	}
	if replayed {
		body["replayed"] = true
	}
	return body
}

func protectedByKey(doc *document, requester, key string, now time.Time) (Question, bool) {
	var found Question
	ok := false
	for _, q := range doc.Questions {
		if q.Requester != requester || q.IdempotencyKey != key || !q.protected(now) {
			continue
		}
		if !ok || q.Number > found.Number {
			found = q
			ok = true
		}
	}
	return found, ok
}

func protectedDuplicate(doc *document, requester, work, fp string, now time.Time) (Question, string, bool) {
	var openQ, unreadQ Question
	hasOpen, hasUnread := false, false
	for _, q := range doc.Questions {
		if q.Requester != requester || q.WorkID != work || q.QuestionFP != fp || !q.protected(now) {
			continue
		}
		if q.Status == StatusOpen {
			openQ = q
			hasOpen = true
		}
		if q.unread() {
			unreadQ = q
			hasUnread = true
		}
	}
	if hasOpen {
		return openQ, StatusOpen, true
	}
	if hasUnread {
		return unreadQ, StatusAnswered, true
	}
	return Question{}, "", false
}

// openCount is the open questions of the store.
func openCount(doc *document) int {
	n := 0
	for _, q := range doc.Questions {
		if q.Status == StatusOpen {
			n++
		}
	}
	return n
}

func recordBytes(q Question) (int, error) {
	raw, err := marshalJSON(q)
	if err != nil {
		return 0, err
	}
	return len(raw), nil
}
