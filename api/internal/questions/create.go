package questions

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalhome"
)

func (s *Service) Create(ctx context.Context, session, principal, role string, raw []byte) Result {
	return s.create(ctx, session, principal, role, raw, false)
}

// CreateForRule creates a card the agent runtime asks that a card-auto-answer
// rule in force answers within seconds (TD-393, STG only: the server takes
// this path only there): the requester's and the store's quotas do not hold
// it, and it leaves no mark in the hourly quotas, so the runtime's cards are
// not held while the rule, not the Owner, answers them, and other
// requesters keep their hourly quotas. While it is open or unread it still
// counts in the store's capacity (20 open, 40 open or unread) like any
// question. Only the runtime's own session may; everything else (the key, a
// duplicate, the size) is as Create.
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
		marks := recentHourly(doc.Hourly, now)
		hash := requesterHash(session)
		if !byRule {
			if len(marks) >= hourlyGlobal {
				return applied{res: fail(http.StatusTooManyRequests, "hourly quota: 30 questions created in the last hour", nil)}
			}
			if countHash(marks, hash) >= hourlyPerRequester {
				return applied{res: fail(http.StatusTooManyRequests, "hourly quota: 6 questions from this requester in the last hour", nil)}
			}
			t := tally(doc, session, now)
			if t.openReq >= openPerRequester || t.protReq >= protectedPerRequester {
				return applied{res: fail(http.StatusTooManyRequests, ackList(t.nums), nil)}
			}
			if t.openAll >= openGlobal {
				return applied{res: fail(http.StatusTooManyRequests, "store full: 20 questions are open", nil)}
			}
			if t.protAll >= protectedGlobal {
				return applied{res: fail(http.StatusTooManyRequests, "store full: 40 questions are open or unread", nil)}
			}
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
		doc.Hourly = marks
		if !byRule {
			doc.Hourly = append(marks, hourlyMark{At: now, RequesterHash: hash})
		}
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

func recentHourly(marks []hourlyMark, now time.Time) []hourlyMark {
	cut := now.Add(-hourlyWindow)
	out := make([]hourlyMark, 0, len(marks))
	for _, h := range marks {
		if !h.At.Before(cut) {
			out = append(out, h)
		}
	}
	return out
}

func countHash(marks []hourlyMark, hash string) int {
	n := 0
	for _, h := range marks {
		if h.RequesterHash == hash {
			n++
		}
	}
	return n
}

type counts struct {
	openAll int
	protAll int
	openReq int
	protReq int
	nums    []int
}

func tally(doc *document, requester string, now time.Time) counts {
	var t counts
	seen := map[int]bool{}
	add := func(n int) {
		if seen[n] {
			return
		}
		seen[n] = true
		t.nums = append(t.nums, n)
	}
	for _, q := range doc.Questions {
		if q.Status == StatusOpen {
			t.openAll++
		}
		if q.protected(now) {
			t.protAll++
		}
		if q.Requester != requester {
			continue
		}
		if q.Status == StatusOpen {
			t.openReq++
			add(q.Number)
		}
		if q.protected(now) {
			t.protReq++
		}
		if q.unread() {
			add(q.Number)
		}
	}
	sort.Ints(t.nums)
	return t
}

func ackList(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = numberLabel(n)
	}
	return "read and ack " + strings.Join(parts, ", ") + " first"
}

func recordBytes(q Question) (int, error) {
	raw, err := marshalJSON(q)
	if err != nil {
		return 0, err
	}
	return len(raw), nil
}
