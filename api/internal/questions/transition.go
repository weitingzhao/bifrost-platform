package questions

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

func (s *Service) Answer(ctx context.Context, session, principal, role, ref string, raw []byte) Result {
	if err := ValidSession(session); err != nil {
		return fail(http.StatusBadRequest, err.Error(), nil)
	}
	c := caller{session: session, principal: clipPrincipal(principal), role: role, op: "answer", ref: ref}
	return s.apply(ctx, c, func(doc *document, now time.Time) applied {
		i, ok := doc.find(strings.TrimSpace(ref))
		if !ok {
			return applied{res: fail(http.StatusNotFound, "not found", nil)}
		}
		q := doc.Questions[i]
		if q.Status == StatusExpired {
			return applied{res: fail(http.StatusConflict, "expired", map[string]any{"status": StatusExpired})}
		}
		option, text, channel, err := parseAnswer(raw, q)
		if err != nil {
			return applied{res: fail(http.StatusBadRequest, err.Error(), nil)}
		}
		if q.Status == StatusAnswered {
			if q.Answer != nil && q.Answer.Option == option && q.Answer.Text == text {
				return applied{res: Result{Code: http.StatusOK, Body: answerBody(q, true)}}
			}
			return applied{res: fail(http.StatusConflict, "already answered", map[string]any{
				"status": StatusAnswered,
				"id":     q.ID,
				"number": q.Number,
				"answer": q.Answer,
			})}
		}
		if q.Status != StatusOpen {
			return applied{res: fail(http.StatusConflict, "not open", map[string]any{"status": q.Status})}
		}
		q.Status = StatusAnswered
		q.Answer = &Answer{
			Option:   option,
			Text:     text,
			By:       c.principal,
			At:       now,
			Channel:  channel,
			Approval: false,
		}
		doc.Questions[i] = q
		return applied{
			res:     Result{Code: http.StatusOK, Body: answerBody(q, false)},
			changed: true,
			extra:   []event{{"question.answer", q.ID, StatusAnswered, ""}},
		}
	})
}

func answerBody(q Question, replayed bool) map[string]any {
	body := map[string]any{
		"id":     q.ID,
		"number": q.Number,
		"status": q.Status,
		"answer": q.Answer,
	}
	if replayed {
		body["replayed"] = true
	}
	return body
}

type answerJSON struct {
	Option  json.RawMessage `json:"option"`
	Text    json.RawMessage `json:"text"`
	Channel json.RawMessage `json:"channel"`
}

func parseAnswer(raw []byte, q Question) (option, text, channel string, err error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", "", "", fieldErr("body", "invalid json body")
	}
	var in answerJSON
	if err := decodeStrict(raw, &in); err != nil {
		return "", "", "", err
	}
	opt, _, err := cleanedString(in.Option, false)
	if err != nil {
		return "", "", "", fieldErr("option", "option must be a string")
	}
	text, _, err = cleanedString(in.Text, true)
	if err != nil {
		return "", "", "", fieldErr("text", "text must be a string")
	}
	if err := limit(text, "text", bodyLimit); err != nil {
		return "", "", "", err
	}
	ch, hasCh, err := rawString(in.Channel)
	if err != nil {
		return "", "", "", fieldErr("channel", "channel must be a string")
	}
	channel = "console"
	if hasCh && strings.TrimSpace(ch) != "console" {
		return "", "", "", fieldErr("channel", "channel must be console")
	}
	if opt != "" && opt != "none" && !hasOption(q, opt) {
		return "", "", "", fieldErr("option", "option is invalid")
	}
	if q.businessIssueCard() {
		// One of its options and nothing else (issuecard.go), whatever
		// allow_text says; before the checks below, which ask for text.
		if text != "" {
			return "", "", "", fieldErr("text", issueCardNoText)
		}
		if opt == "" || opt == "none" {
			return "", "", "", fieldErr("option", issueCardNoNone)
		}
		return opt, "", channel, nil
	}
	if opt == "" && text == "" {
		return "", "", "", fieldErr("option", "option or text is required")
	}
	if opt == "none" && text == "" {
		return "", "", "", fieldErr("text", "text is required")
	}
	if !q.AllowText && text != "" && opt != "none" {
		return "", "", "", fieldErr("text", "text is not allowed")
	}
	return opt, text, channel, nil
}

func hasOption(q Question, key string) bool {
	for _, o := range q.Options {
		if o.Key == key {
			return true
		}
	}
	return false
}

func (s *Service) Withdraw(ctx context.Context, session, principal, role, ref string, raw []byte) Result {
	if err := ValidSession(session); err != nil {
		return fail(http.StatusBadRequest, err.Error(), nil)
	}
	reason, err := parseReason(raw)
	if err != nil {
		return fail(http.StatusBadRequest, err.Error(), nil)
	}
	c := caller{session: session, principal: clipPrincipal(principal), role: role, op: "withdraw", ref: ref}
	return s.apply(ctx, c, func(doc *document, now time.Time) applied {
		i, ok := doc.find(strings.TrimSpace(ref))
		if !ok {
			return applied{res: fail(http.StatusNotFound, "not found", nil)}
		}
		q := doc.Questions[i]
		if q.Status != StatusOpen {
			return applied{res: fail(http.StatusConflict, "not open", map[string]any{"status": q.Status})}
		}
		if session != q.Requester {
			return applied{res: fail(http.StatusForbidden, "session is not the requester", nil)}
		}
		q.Status = StatusWithdrawn
		q.Withdrawal = &Withdrawal{Session: session, Principal: c.principal, At: now, Reason: reason}
		doc.Questions[i] = q
		return applied{
			res:     Result{Code: http.StatusOK, Body: q.public(session)},
			changed: true,
			extra:   []event{{"question.withdraw", q.ID, StatusWithdrawn, ""}},
		}
	})
}

type reasonJSON struct {
	Reason json.RawMessage `json:"reason"`
}

func parseReason(raw []byte) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", fieldErr("reason", "reason is required")
	}
	var in reasonJSON
	if err := decodeStrict(raw, &in); err != nil {
		return "", err
	}
	reason, ok, err := cleanedString(in.Reason, false)
	if err != nil {
		return "", fieldErr("reason", "reason must be a string")
	}
	if !ok {
		return "", fieldErr("reason", "reason is required")
	}
	if err := limit(reason, "reason", lineLimit); err != nil {
		return "", err
	}
	return reason, nil
}

func (s *Service) Ack(ctx context.Context, session, principal, role, ref string, raw []byte) Result {
	if err := ValidSession(session); err != nil {
		return fail(http.StatusBadRequest, err.Error(), nil)
	}
	if err := parseAck(raw); err != nil {
		return fail(http.StatusBadRequest, err.Error(), nil)
	}
	c := caller{session: session, principal: clipPrincipal(principal), role: role, op: "ack", ref: ref}
	return s.apply(ctx, c, func(doc *document, now time.Time) applied {
		i, ok := doc.find(strings.TrimSpace(ref))
		if !ok {
			return applied{res: fail(http.StatusNotFound, "not found", nil)}
		}
		q := doc.Questions[i]
		if q.Status != StatusAnswered || q.Answer == nil {
			return applied{res: fail(http.StatusConflict, "not answered", map[string]any{"status": q.Status})}
		}
		if session != q.Requester {
			return applied{res: fail(http.StatusForbidden, "session is not the requester", nil)}
		}
		if q.Answer.Read != nil {
			return applied{res: Result{Code: http.StatusOK, Body: q.public(session)}}
		}
		read := &ReadReceipt{At: now, Session: session, Principal: c.principal}
		q.Answer.Read = read
		doc.Questions[i] = q
		return applied{
			res:     Result{Code: http.StatusOK, Body: q.public(session)},
			changed: true,
			extra:   []event{{"question.ack", q.ID, StatusAnswered, ""}},
		}
	})
}

func parseAck(raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var in struct{}
	return decodeStrict(raw, &in)
}

func (s *Service) Get(ctx context.Context, session, principal, role, ref string) Result {
	if err := ValidSession(session); err != nil {
		return fail(http.StatusBadRequest, err.Error(), nil)
	}
	c := caller{session: session, principal: clipPrincipal(principal), role: role, op: "get", ref: ref}
	return s.view(ctx, c, func(doc *document, now time.Time) applied {
		i, ok := doc.find(strings.TrimSpace(ref))
		if !ok {
			return applied{res: fail(http.StatusNotFound, "not found", nil)}
		}
		return applied{res: Result{Code: http.StatusOK, Body: doc.Questions[i].public(session)}}
	})
}

func (s *Service) List(ctx context.Context, session, principal, role, status string, mine bool) Result {
	if err := ValidSession(session); err != nil {
		return fail(http.StatusBadRequest, err.Error(), nil)
	}
	if status == "" {
		if mine {
			status = "inbox"
		} else {
			status = StatusOpen
		}
	}
	switch status {
	case "inbox", "unread", "all", StatusOpen, StatusAnswered, StatusWithdrawn, StatusExpired:
	default:
		return fail(http.StatusBadRequest, "status is invalid", nil)
	}
	c := caller{session: session, principal: clipPrincipal(principal), role: role, op: "list"}
	return s.view(ctx, c, func(doc *document, now time.Time) applied {
		var rows []Question
		for _, q := range doc.Questions {
			if !listed(q, status, session, mine, now) {
				continue
			}
			rows = append(rows, q.public(session))
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
				return rows[i].Number > rows[j].Number
			}
			return rows[i].CreatedAt.After(rows[j].CreatedAt)
		})
		if rows == nil {
			rows = []Question{}
		}
		return applied{res: Result{Code: http.StatusOK, Body: map[string]any{"questions": rows}}}
	})
}

func listed(q Question, status, session string, mine bool, now time.Time) bool {
	if status == "inbox" {
		return inInbox(q, session, now)
	}
	if mine && q.Requester != session {
		return false
	}
	switch status {
	case "all":
		return true
	case "unread":
		return q.unread()
	default:
		return q.Status == status
	}
}

func inInbox(q Question, session string, now time.Time) bool {
	if q.Requester != session {
		return false
	}
	switch q.Status {
	case StatusOpen:
		return true
	case StatusAnswered:
		return q.unread()
	case StatusExpired:
		return !q.ExpiresAt.Before(now.Add(-inboxExpiredWindow))
	default:
		return false
	}
}
