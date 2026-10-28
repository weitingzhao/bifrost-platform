package agentrtapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The rule routes (design v5 §5.2): check reads a rule's text and says what
// it would be, writing nothing; a seat or a thread proposes one for the Owner
// to sign; the Owner's signed text is taken by sign (the signature, not the
// caller, is the authority); anyone may tighten, without a signature, when
// the comparison with the version before shows it grants nothing more.

type ruleBody struct {
	Text string `json:"text"`
}

type ruleSignBody struct {
	Text      string `json:"text"`
	Signature string `json:"signature"`
}

type ruleProposalBody struct {
	IdempotencyKey string `json:"idempotency_key"`
	Text           string `json:"text"`
	Reason         string `json:"reason"`
}

func (a *API) checkRule(w http.ResponseWriter, r *http.Request) {
	var b ruleBody
	if !decode(w, r, &b) {
		return
	}
	out, err := store.CheckRule(r.Context(), a.DB, chi.URLParam(r, "rule_id"), []byte(b.Text))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) proposeRule(w http.ResponseWriter, r *http.Request) {
	var b ruleProposalBody
	if !decode(w, r, &b) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.ProposeRule(r.Context(), a.DB, who, b.IdempotencyKey, chi.URLParam(r, "rule_id"), []byte(b.Text), b.Reason)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) signRule(w http.ResponseWriter, r *http.Request) {
	var b ruleSignBody
	if !decode(w, r, &b) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.SignRule(r.Context(), a.DB, who, chi.URLParam(r, "rule_id"), []byte(b.Text), b.Signature, a.Anchor)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) tightenRule(w http.ResponseWriter, r *http.Request) {
	var b ruleBody
	if !decode(w, r, &b) {
		return
	}
	who, err := actorOf(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.TightenRule(r.Context(), a.DB, who, chi.URLParam(r, "rule_id"), []byte(b.Text))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// getRule is the rule in force now (null when none is: the strictest default
// holds) with its text.
func (a *API) getRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "rule_id")
	rule, err := store.RuleInForce(r.Context(), a.DB, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if rule == nil {
		writeJSON(w, http.StatusOK, map[string]any{"rule_id": id, "in_force": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule_id": id, "in_force": rule.Doc(), "signed_by": rule.SignedBy})
}

// getLandingCard is GET /agentrt/landing/cards/{question_id} (S3 B3, CUT
// §11.6 v3 item 8): what a landing card asked and what became of its answer,
// readable after the question store trimmed the question.
func (a *API) getLandingCard(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "question_id")
	v, found, err := store.ReadLandingCard(r.Context(), a.DB, "bifrost-platform", id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "no landing card was asked with this question")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"question_id": v.QuestionID, "prep": v.Prep, "kind": v.Kind, "root": v.Root, "op": v.Op,
		"trigger_seq": v.TriggerSeq, "stop_generation": v.StopGeneration, "acked": v.Acked, "closed": v.Closed, "closed_why": v.ClosedWhy,
		"options": v.Options, "outcome": v.Outcome, "next_step": v.NextStep})
}

// getMissionLanding is GET /agentrt/landing/missions/{id} (S3 B4, CUT §11.8
// v3 item 9): each live-scope root's class, reason and card, the rule as
// read now and the closing round. in_scope false: the mission closes by the
// rules before S3.
func (a *API) getMissionLanding(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	st, err := store.ReadMissionLandingStatus(r.Context(), a.DB, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	roots := make([]map[string]any, 0, len(st.Roots))
	for _, x := range st.Roots {
		roots = append(roots, map[string]any{"root": x.Root, "op": x.Op, "class": x.Class, "reason": x.Reason,
			"needs_evidence": x.NeedsEvidence, "card_kind": x.CardKind, "question_id": x.QuestionID, "card_alert": x.CardAlert})
	}
	body := map[string]any{"mission_id": id, "in_scope": st.InScope, "roots": roots, "rule": st.Rule}
	if st.Round != nil {
		body["closing_round"] = map[string]any{"round": st.Round.Round, "at": st.Round.At, "until": st.Round.Until, "ops": st.Round.Ops}
	}
	writeJSON(w, http.StatusOK, body)
}
