package agentrtapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The crew's issue routes (W-31 BI-1 slice 2, DESIGN-crew-intake §2.2,
// §5.3, §8): for the issue readers only, the operator and admin tokens the
// readers list names (actuation.RequireIssueReader; not viewer, reporter,
// agentd or trade-satellite). They read issues in full and work them as the
// writer writer() gives, unchanged: the Owner by his admin token, a thread
// by its session. Only a thread or the Owner works an issue (the store
// refuses a seat).
//
//	GET  /issues                       list_issues (?view=, state=, product=, waiting_on=, before=, limit=)
//	GET  /issues/{id}                  get_issue_full; the Console's read-only issue page
//	POST /issues                       file_issue
//	POST /issues/merge                 merge_issues
//	POST /issues/{id}/additions        add_to_issue (the Owner's words, or his relay of hers)
//	POST /issues/{id}/responses        relay_user_response (the Owner's, or his relay of hers)
//	POST /issues/{id}/brief            draft_brief
//	POST /issues/{id}/classification   classify_issue
//	POST /issues/{id}/design           put_design
//	POST /issues/{id}/reviews          record_review
//	POST /issues/{id}/close-checks     a seat's check of an answer, which a close rests on
//	POST /issues/{id}/owner-cards      request_owner: the runtime asks the Owner, by its template
//	POST /issues/{id}/take             take_issue
//	POST /issues/{id}/replies          reply_to_requester
//	POST /issues/{id}/close            close_issue
//	POST /issues/{id}/park|resume|reopen

func (a *API) mountIssues(r chi.Router, auth *actuation.AuthService, cfg func(http.Handler) http.Handler) {
	readers := auth.RequireIssueReader()
	r.With(readers, cfg).Get("/issues", a.listIssues)
	r.With(readers, cfg).Get("/issues/{id}", a.issueFull)
	r.With(readers, cfg).Post("/issues", a.fileIssue)
	r.With(readers, cfg).Post("/issues/merge", a.mergeIssues)
	for path, h := range map[string]http.HandlerFunc{
		"additions": a.crewAdd, "responses": a.crewResponse, "brief": a.draftBrief, "classification": a.classifyIssue,
		"design": a.putDesign, "reviews": a.recordReview, "close-checks": a.recordCloseCheck, "owner-cards": a.requestOwner,
		"take": a.takeIssue, "replies": a.replyIssue, "close": a.closeIssue, "park": a.parkIssue, "resume": a.resumeIssue,
		"reopen": a.reopenIssue,
	} {
		r.With(readers, cfg).Post("/issues/{id}/"+path, h)
	}
}

// asCrew runs a crew route's work as its writer.
func (a *API) asCrew(w http.ResponseWriter, r *http.Request, do func(store.Writer) (map[string]any, error)) {
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := do(who)
	if err != nil {
		a.failIssue(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) listIssues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, ok := atoiParam(w, q.Get("limit"))
	if !ok {
		return
	}
	out, err := store.ListIssues(r.Context(), a.DB, store.IssueListFilter{View: q.Get("view"), State: q.Get("state"), Product: q.Get("product"),
		WaitingOn: q.Get("waiting_on"), Before: q.Get("before"), Limit: limit})
	if err != nil {
		a.failIssue(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) issueFull(w http.ResponseWriter, r *http.Request) {
	out, err := store.IssueFull(r.Context(), a.DB, chi.URLParam(r, "id"))
	if err != nil {
		a.failIssue(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// fileBody is file_issue: submit_issue with the channel (thread, or
// owner_relay when the Owner relays another's words).
type fileBody struct {
	submitBody
	Channel string `json:"channel"`
}

func (a *API) fileIssue(w http.ResponseWriter, r *http.Request) {
	var b fileBody
	if !decode(w, r, &b) {
		return
	}
	a.asCrew(w, r, func(who store.Writer) (map[string]any, error) {
		return store.SubmitIssue(r.Context(), a.DB, who, b.IdempotencyKey, b.submission(b.Channel))
	})
}

type crewAddBody struct {
	additionBody
	Channel string `json:"channel"`
}

func (a *API) crewAdd(w http.ResponseWriter, r *http.Request) {
	var b crewAddBody
	if !decode(w, r, &b) {
		return
	}
	a.asCrew(w, r, func(who store.Writer) (map[string]any, error) {
		return store.AddToIssue(r.Context(), a.DB, who, b.IdempotencyKey, store.IssueAddition{IssueID: chi.URLParam(r, "id"),
			Words: words(b.PersonWords), Reading: b.AgentReading, Proposal: b.AgentProposal, Channel: b.Channel})
	})
}

type crewResponseBody struct {
	responseBody
	Channel string `json:"channel"`
}

func (a *API) crewResponse(w http.ResponseWriter, r *http.Request) {
	var b crewResponseBody
	if !decode(w, r, &b) {
		return
	}
	if b.ExpectedVersion == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "expected_version is the issue's version the answer was read at")
		return
	}
	a.asCrew(w, r, func(who store.Writer) (map[string]any, error) {
		return store.AnswerAsk(r.Context(), a.DB, who, b.IdempotencyKey, b.answer(chi.URLParam(r, "id"), b.Channel))
	})
}

// versioned is what every crew write on one issue carries.
type versioned struct {
	IdempotencyKey  string `json:"idempotency_key"`
	ExpectedVersion *int   `json:"expected_version"`
}

// onIssue runs a crew write on the issue in the path at the version read.
func (a *API) onIssue(w http.ResponseWriter, r *http.Request, v versioned, do func(who store.Writer, id string, version int) (map[string]any, error)) {
	if v.ExpectedVersion == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "expected_version is the issue's version the write was read at")
		return
	}
	a.asCrew(w, r, func(who store.Writer) (map[string]any, error) {
		return do(who, chi.URLParam(r, "id"), *v.ExpectedVersion)
	})
}

type textBody struct {
	versioned
	Text string `json:"text"`
}

func (a *API) draftBrief(w http.ResponseWriter, r *http.Request) {
	var b textBody
	if decode(w, r, &b) {
		a.onIssue(w, r, b.versioned, func(who store.Writer, id string, v int) (map[string]any, error) {
			return store.DraftBrief(r.Context(), a.DB, who, b.IdempotencyKey, store.BriefDraft{IssueID: id, ExpectedVersion: v, Text: b.Text})
		})
	}
}

type classifyBody struct {
	versioned
	RiskClass  string `json:"risk_class"`
	Category   string `json:"category"`
	SeatReport string `json:"seat_report"`
	Reason     string `json:"reason"`
}

func (a *API) classifyIssue(w http.ResponseWriter, r *http.Request) {
	var b classifyBody
	if decode(w, r, &b) {
		a.onIssue(w, r, b.versioned, func(who store.Writer, id string, v int) (map[string]any, error) {
			return store.ClassifyIssue(r.Context(), a.DB, who, b.IdempotencyKey, store.IssueClassification{IssueID: id, ExpectedVersion: v,
				RiskClass: b.RiskClass, Category: b.Category, SeatReport: b.SeatReport, Reason: b.Reason})
		})
	}
}

type decisionBody struct {
	Decision string   `json:"decision"`
	Proceed  []string `json:"proceed"`
}

type designBody struct {
	versioned
	Text      string         `json:"text"`
	Decisions []decisionBody `json:"decisions"`
}

func (a *API) putDesign(w http.ResponseWriter, r *http.Request) {
	var b designBody
	if decode(w, r, &b) {
		a.onIssue(w, r, b.versioned, func(who store.Writer, id string, v int) (map[string]any, error) {
			ds := make([]store.DesignDecision, 0, len(b.Decisions))
			for _, d := range b.Decisions {
				ds = append(ds, store.DesignDecision{Key: d.Decision, Proceed: d.Proceed})
			}
			return store.PutDesign(r.Context(), a.DB, who, b.IdempotencyKey, store.IssueDesign{IssueID: id, ExpectedVersion: v, Text: b.Text, Decisions: ds})
		})
	}
}

type reviewBody struct {
	versioned
	Report       string `json:"report"`
	ReviewerTool string `json:"reviewer_tool"`
	AuthorTool   string `json:"author_tool"`
	Round        int    `json:"round"`
}

func (a *API) recordReview(w http.ResponseWriter, r *http.Request) {
	var b reviewBody
	if decode(w, r, &b) {
		a.onIssue(w, r, b.versioned, func(who store.Writer, id string, v int) (map[string]any, error) {
			return store.RecordReview(r.Context(), a.DB, who, b.IdempotencyKey, store.IssueReview{IssueID: id, ExpectedVersion: v, Report: b.Report,
				ReviewerTool: b.ReviewerTool, AuthorTool: b.AuthorTool, Round: b.Round})
		})
	}
}

type closeCheckBody struct {
	versioned
	AskID      int64  `json:"ask_id"`
	Result     string `json:"result"`
	SeatReport string `json:"seat_report"`
	Excerpt    string `json:"excerpt"`
}

func (a *API) recordCloseCheck(w http.ResponseWriter, r *http.Request) {
	var b closeCheckBody
	if decode(w, r, &b) {
		a.onIssue(w, r, b.versioned, func(who store.Writer, id string, v int) (map[string]any, error) {
			return store.RecordCloseCheck(r.Context(), a.DB, who, b.IdempotencyKey, store.IssueCloseCheck{IssueID: id, ExpectedVersion: v,
				AskID: b.AskID, Result: b.Result, SeatReport: b.SeatReport, Excerpt: b.Excerpt})
		})
	}
}

// ownerCardBody is request_owner (§2.2): the purpose and, for a design's
// point, the point and its letters; never a word of text (R2-A2).
type ownerCardBody struct {
	versioned
	Purpose  string `json:"purpose"`
	Decision string `json:"decision"`
	Letters  int    `json:"letters"`
}

func (a *API) requestOwner(w http.ResponseWriter, r *http.Request) {
	var b ownerCardBody
	if decode(w, r, &b) {
		a.onIssue(w, r, b.versioned, func(who store.Writer, id string, v int) (map[string]any, error) {
			return store.RequestOwner(r.Context(), a.DB, who, b.IdempotencyKey, store.OwnerCardRequest{IssueID: id, ExpectedVersion: v,
				Purpose: b.Purpose, Decision: b.Decision, Letters: b.Letters}, a.Ask)
		})
	}
}

// takeBody is take_issue: the commitment and the mission to make (its work
// id is the issue's).
type takeBody struct {
	versioned
	Commitment string `json:"commitment"`
	Mission    struct {
		Title      string      `json:"title"`
		Goal       string      `json:"goal"`
		Tenant     string      `json:"tenant"`
		DeadlineAt *time.Time  `json:"deadline_at"`
		Success    []string    `json:"success"`
		Repos      []string    `json:"repos"`
		MaxAction  string      `json:"max_action"`
		Budget     *budgetBody `json:"budget"`
		NoMerge    string      `json:"no_merge"`
		NeedsOwner string      `json:"needs_owner"`
	} `json:"mission"`
}

func (a *API) takeIssue(w http.ResponseWriter, r *http.Request) {
	var b takeBody
	if decode(w, r, &b) {
		a.onIssue(w, r, b.versioned, func(who store.Writer, id string, v int) (map[string]any, error) {
			m := b.Mission
			p := store.MissionProposal{Title: m.Title, Goal: m.Goal, Tenant: m.Tenant, Success: m.Success, Repos: m.Repos,
				MaxAction: m.MaxAction, NoMerge: m.NoMerge, NeedsOwner: m.NeedsOwner}
			if m.DeadlineAt != nil {
				p.Deadline = *m.DeadlineAt
			}
			if m.Budget != nil {
				p.MaxAttempts, p.MaxWallHours = m.Budget.MaxAttempts, m.Budget.MaxWallHours
			}
			return store.TakeIssue(r.Context(), a.DB, who, b.IdempotencyKey, store.IssueTake{IssueID: id, ExpectedVersion: v,
				Commitment: b.Commitment, Mission: p, Ask: a.Ask})
		})
	}
}

type replyBody struct {
	versioned
	Purpose    string   `json:"purpose"`
	Text       string   `json:"text"`
	Commitment int      `json:"commitment"`
	MissionID  string   `json:"mission_id"`
	Env        string   `json:"env"`
	Commits    []string `json:"commits"`
}

func (a *API) replyIssue(w http.ResponseWriter, r *http.Request) {
	var b replyBody
	if decode(w, r, &b) {
		a.onIssue(w, r, b.versioned, func(who store.Writer, id string, v int) (map[string]any, error) {
			return store.Reply(r.Context(), a.DB, who, b.IdempotencyKey, store.IssueReply{IssueID: id, ExpectedVersion: v, Purpose: b.Purpose,
				Text: b.Text, Commitment: b.Commitment, MissionID: b.MissionID, Env: b.Env, Commits: b.Commits})
		})
	}
}

type closeBody struct {
	versioned
	Conclusion string `json:"conclusion"`
	AskID      int64  `json:"ask_id"`
	Reason     string `json:"reason"`
}

func (a *API) closeIssue(w http.ResponseWriter, r *http.Request) {
	var b closeBody
	if decode(w, r, &b) {
		a.onIssue(w, r, b.versioned, func(who store.Writer, id string, v int) (map[string]any, error) {
			return store.CloseIssue(r.Context(), a.DB, who, b.IdempotencyKey, store.IssueClose{IssueID: id, ExpectedVersion: v,
				Conclusion: b.Conclusion, AskID: b.AskID, Reason: b.Reason})
		})
	}
}

type noteBody struct {
	versioned
	Reason string `json:"reason"`
}

func (a *API) noteWrite(w http.ResponseWriter, r *http.Request,
	do func(ctx *http.Request, who store.Writer, key string, n store.IssueNote) (map[string]any, error)) {
	var b noteBody
	if decode(w, r, &b) {
		a.onIssue(w, r, b.versioned, func(who store.Writer, id string, v int) (map[string]any, error) {
			return do(r, who, b.IdempotencyKey, store.IssueNote{IssueID: id, ExpectedVersion: v, Reason: b.Reason})
		})
	}
}

func (a *API) parkIssue(w http.ResponseWriter, r *http.Request) {
	a.noteWrite(w, r, func(r *http.Request, who store.Writer, key string, n store.IssueNote) (map[string]any, error) {
		return store.ParkIssue(r.Context(), a.DB, who, key, n)
	})
}

func (a *API) resumeIssue(w http.ResponseWriter, r *http.Request) {
	a.noteWrite(w, r, func(r *http.Request, who store.Writer, key string, n store.IssueNote) (map[string]any, error) {
		return store.ResumeIssue(r.Context(), a.DB, who, key, n)
	})
}

func (a *API) reopenIssue(w http.ResponseWriter, r *http.Request) {
	a.noteWrite(w, r, func(r *http.Request, who store.Writer, key string, n store.IssueNote) (map[string]any, error) {
		return store.ReopenIssue(r.Context(), a.DB, who, key, n)
	})
}

type mergeBody struct {
	IdempotencyKey string `json:"idempotency_key"`
	From           string `json:"from"`
	Into           string `json:"into"`
	FromVersion    *int   `json:"from_version"`
	IntoVersion    *int   `json:"into_version"`
}

func (a *API) mergeIssues(w http.ResponseWriter, r *http.Request) {
	var b mergeBody
	if !decode(w, r, &b) {
		return
	}
	if b.FromVersion == nil || b.IntoVersion == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "from_version and into_version are the versions the merge was read at")
		return
	}
	a.asCrew(w, r, func(who store.Writer) (map[string]any, error) {
		return store.MergeIssues(r.Context(), a.DB, who, b.IdempotencyKey, store.IssueMerge{From: b.From, Into: b.Into,
			FromVersion: *b.FromVersion, IntoVersion: *b.IntoVersion})
	})
}
