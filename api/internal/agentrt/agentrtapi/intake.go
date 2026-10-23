package agentrtapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The intake routes (W-31 BI-1 slice 2, DESIGN-crew-intake-2026-10-22
// §2.1): the seven an external agent's intake token opens, and nothing else
// (its role is below viewer, actuation.RoleIntake; the full-route test in
// the server package holds it to them). Its writes and reads are as its
// runtime actor agent:<name> (intakeWriter), bound to one person and its
// products, checked in each transaction against its client row (token,
// revocation, scope), replays of its keys included.
//
//	GET  /intake/guide                     intake_guide: who it is, its limits and what it used, its acknowledgement
//	POST /intake/issues                    submit_issue
//	GET  /intake/issues                    my_issues
//	GET  /intake/issues/{id}               get_issue: the side-that-brought-it view
//	POST /intake/issues/{id}/additions     add_to_issue
//	POST /intake/issues/{id}/responses     relay_user_response
//	POST /intake/updates                   get_updates and its acknowledgement
//
// Every text it sends or reads is data: her words reach no other route.

// intakeRoutes are the paths of the seven intake routes under /agentrt, by
// method; the server's full-route test reads them.
var intakeRoutes = []string{
	"GET /intake/guide", "POST /intake/issues", "GET /intake/issues", "GET /intake/issues/{id}",
	"POST /intake/issues/{id}/additions", "POST /intake/issues/{id}/responses", "POST /intake/updates",
}

// IntakeRoutes are the intake routes, as "METHOD /api/v1/agentrt/…".
func IntakeRoutes() []string {
	out := make([]string, len(intakeRoutes))
	for i, r := range intakeRoutes {
		m, p, _ := strings.Cut(r, " ")
		out[i] = m + " /api/v1/agentrt" + p
	}
	return out
}

// intakeWriter is who an intake token writes and reads as (§5.2, R2-A1):
// intake:<name> is agent:<name>. writer() stays as it was for every other
// role; only the intake routes call this.
func intakeWriter(r *http.Request) (store.Writer, error) {
	p := actuation.PrincipalFromContext(r.Context())
	actor, ok := actuation.IntakeClient(p)
	if !ok {
		return store.Writer{}, store.ErrNotYours
	}
	return store.Writer{Actor: actor, Token: p.Name}, nil
}

func (a *API) mountIntake(r chi.Router, auth *actuation.AuthService, cfg func(http.Handler) http.Handler) {
	intake := auth.RequireAny(actuation.RoleIntake)
	r.With(intake, cfg).Get("/intake/guide", a.intakeGuide)
	r.With(intake, cfg).Post("/intake/issues", a.submitIssue)
	r.With(intake, cfg).Get("/intake/issues", a.myIssues)
	r.With(intake, cfg).Get("/intake/issues/{id}", a.seeIssue)
	r.With(intake, cfg).Post("/intake/issues/{id}/additions", a.intakeAdd)
	r.With(intake, cfg).Post("/intake/issues/{id}/responses", a.relayResponse)
	r.With(intake, cfg).Post("/intake/updates", a.intakeUpdates)
}

// asIntake runs a handler's work as the calling intake client.
func (a *API) asIntake(w http.ResponseWriter, r *http.Request, do func(store.Writer) (map[string]any, error)) {
	who, err := intakeWriter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "only an intake token calls this")
		return
	}
	out, err := do(who)
	if err != nil {
		a.failIssue(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) intakeGuide(w http.ResponseWriter, r *http.Request) {
	a.asIntake(w, r, func(who store.Writer) (map[string]any, error) {
		return store.IntakeGuide(r.Context(), a.DB, who, r.URL.Query().Get("product"))
	})
}

// wordsBody is a segment of a person's own words.
type wordsBody struct {
	Text   string    `json:"text"`
	SaidAt time.Time `json:"said_at"`
	Where  string    `json:"where"`
}

func words(in []wordsBody) []store.IssueWords {
	out := make([]store.IssueWords, 0, len(in))
	for _, w := range in {
		out = append(out, store.IssueWords{Text: w.Text, SaidAt: w.SaidAt, Where: w.Where})
	}
	return out
}

// submitBody is submit_issue (§2.1). An intake client names no channel: its
// own is its local bridge.
type submitBody struct {
	IdempotencyKey string      `json:"idempotency_key"`
	Product        string      `json:"product"`
	Kind           string      `json:"kind"`
	OnBehalfOf     string      `json:"on_behalf_of"`
	Title          string      `json:"title"`
	PersonWords    []wordsBody `json:"person_words"`
	AgentReading   string      `json:"agent_reading"`
	AgentProposal  string      `json:"agent_proposal"`
	DedupeKey      string      `json:"dedupe_key"`
}

func (b submitBody) submission(channel string) store.IssueSubmission {
	return store.IssueSubmission{Product: b.Product, Kind: b.Kind, Title: b.Title, OnBehalfOf: b.OnBehalfOf, Words: words(b.PersonWords),
		Reading: b.AgentReading, Proposal: b.AgentProposal, DedupeKey: b.DedupeKey, Channel: channel}
}

func (a *API) submitIssue(w http.ResponseWriter, r *http.Request) {
	var b submitBody
	if !decode(w, r, &b) {
		return
	}
	a.asIntake(w, r, func(who store.Writer) (map[string]any, error) {
		return store.SubmitIssue(r.Context(), a.DB, who, b.IdempotencyKey, b.submission(""))
	})
}

func (a *API) myIssues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, ok := atoiParam(w, q.Get("limit"))
	if !ok {
		return
	}
	a.asIntake(w, r, func(who store.Writer) (map[string]any, error) {
		return store.MyIssues(r.Context(), a.DB, who, store.MyIssuesFilter{State: q.Get("state"), Product: q.Get("product"), Limit: limit})
	})
}

func (a *API) seeIssue(w http.ResponseWriter, r *http.Request) {
	a.asIntake(w, r, func(who store.Writer) (map[string]any, error) {
		return store.IssueSeen(r.Context(), a.DB, who, chi.URLParam(r, "id"))
	})
}

// additionBody is add_to_issue: at least one of the three.
type additionBody struct {
	IdempotencyKey string      `json:"idempotency_key"`
	PersonWords    []wordsBody `json:"person_words"`
	AgentReading   string      `json:"agent_reading"`
	AgentProposal  string      `json:"agent_proposal"`
}

func (a *API) intakeAdd(w http.ResponseWriter, r *http.Request) {
	var b additionBody
	if !decode(w, r, &b) {
		return
	}
	a.asIntake(w, r, func(who store.Writer) (map[string]any, error) {
		return store.AddToIssue(r.Context(), a.DB, who, b.IdempotencyKey, store.IssueAddition{IssueID: chi.URLParam(r, "id"),
			Words: words(b.PersonWords), Reading: b.AgentReading, Proposal: b.AgentProposal})
	})
}

// responseBody is relay_user_response (§2.1): her words, verbatim, to one
// ask, and what they say; the agent's own remark apart.
type responseBody struct {
	IdempotencyKey  string    `json:"idempotency_key"`
	ExpectedVersion *int      `json:"expected_version"`
	InReplyTo       int64     `json:"in_reply_to"`
	UserWords       string    `json:"user_words"`
	SaidAt          time.Time `json:"said_at"`
	UserVerdict     string    `json:"user_verdict"`
	AgentNote       string    `json:"agent_note"`
}

func (b responseBody) answer(issue, channel string) store.AskAnswer {
	return store.AskAnswer{IssueID: issue, ExpectedVersion: *b.ExpectedVersion, AskID: b.InReplyTo, Words: b.UserWords, SaidAt: b.SaidAt,
		Verdict: b.UserVerdict, Note: b.AgentNote, Channel: channel}
}

func (a *API) relayResponse(w http.ResponseWriter, r *http.Request) {
	var b responseBody
	if !decode(w, r, &b) {
		return
	}
	if b.ExpectedVersion == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "expected_version is the issue's version the answer was read at")
		return
	}
	a.asIntake(w, r, func(who store.Writer) (map[string]any, error) {
		return store.AnswerAsk(r.Context(), a.DB, who, b.IdempotencyKey, b.answer(chi.URLParam(r, "id"), ""))
	})
}

// updatesBody is get_updates: where to read on from, what was stored, and
// how many (0 acknowledges only).
type updatesBody struct {
	Cursor    string `json:"cursor"`
	AckCursor string `json:"ack_cursor"`
	Limit     *int   `json:"limit"`
}

func (a *API) intakeUpdates(w http.ResponseWriter, r *http.Request) {
	var b updatesBody
	if !decode(w, r, &b) {
		return
	}
	req := store.UpdatesRequest{Limit: 20}
	if b.Limit != nil {
		req.Limit = *b.Limit
	}
	for _, c := range []struct {
		raw  string
		into **store.EventCursor
	}{{b.Cursor, &req.Cursor}, {b.AckCursor, &req.Ack}} {
		if c.raw == "" {
			continue
		}
		cur, ok := store.ParseEventCursor(c.raw)
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "a cursor is one a read gave as next_cursor (<txid>.<event_id>)")
			return
		}
		*c.into = &cur
	}
	a.asIntake(w, r, func(who store.Writer) (map[string]any, error) {
		return store.IntakeUpdates(r.Context(), a.DB, who, req)
	})
}

func atoiParam(w http.ResponseWriter, s string) (int, bool) {
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "limit is a number")
		return 0, false
	}
	return n, true
}

// failIssue maps the issue store's own errors (§2.1): a daily limit is 429;
// a write on a version or an ask that moved on is 409 with the issue as it
// is now; a revoked client is 403; the rest as fail.
func (a *API) failIssue(w http.ResponseWriter, err error) {
	var stale *store.StaleError
	switch {
	case errors.As(err, &stale):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "stale", "message": err.Error(), "issue": stale.Issue.Doc()})
	case errors.Is(err, store.ErrQuota):
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, "quota", err.Error())
	case errors.Is(err, store.ErrRevoked):
		writeError(w, http.StatusForbidden, "revoked", err.Error())
	case errors.Is(err, store.ErrBadCursor):
		writeError(w, http.StatusBadRequest, "bad_cursor", err.Error())
	default:
		a.fail(w, err)
	}
}
