// Package rulesapi is the verification domain's HTTP for the signed rules
// (design v5 §5.2): GET /agentrt/rules/{rule_id}, POST
// /agentrt/rules/{rule_id}/check, /proposals, /sign and /tighten.
//
// The handlers were agentrtapi's (rules.go) and are the same code
// (LAW-10, 2026-11-19: a domain's routes sit in its own package, so
// its contract can say what it offers). agentrtapi still mounts them, behind
// the same abilities, and lends them what every /agentrt route shares
// (Host). The signature check a signed rule is verified with is the release
// gate's (delivery), so the assembly binds it (Routes.Verify) and this
// package imports no delivery package.
package rulesapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Host is what the agent runtime's HTTP layer lends the rule routes. Each
// read is live: the platform connects the database after the routes are
// mounted.
type Host interface {
	// DB is the agent runtime's database.
	DB() eventlog.DB
	// Anchor is the fingerprints (space separated) of the keys a signed rule
	// may verify against; empty refuses every signed rule.
	Anchor() string
	// Writer is who a request's write is recorded as.
	Writer(*http.Request) (actor.Writer, error)
	// ActorOf is Writer, except that an agentd token writes as its host.
	ActorOf(*http.Request) (actor.Writer, error)
	// Decode reads a JSON body into v, refusing unknown fields (400).
	Decode(w http.ResponseWriter, r *http.Request, v any) bool
	// JSON answers v with status.
	JSON(w http.ResponseWriter, status int, v any)
	// Fail answers an error of the store with its status.
	Fail(w http.ResponseWriter, err error)
}

// Routes are the verification domain's handlers for the signed rules.
type Routes struct {
	Host Host
	// Verify checks an Owner's signature against the anchor (the release
	// gate's check, bound by the assembly); nil refuses every signed rule as
	// unsigned (store.SignRule).
	Verify store.SignatureCheck
}

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

// HandleCheckRule is POST /rules/{rule_id}/check.
func (rt Routes) HandleCheckRule(w http.ResponseWriter, r *http.Request) {
	h := rt.Host
	var b ruleBody
	if !h.Decode(w, r, &b) {
		return
	}
	out, err := store.CheckRule(r.Context(), h.DB(), chi.URLParam(r, "rule_id"), []byte(b.Text))
	if err != nil {
		h.Fail(w, err)
		return
	}
	h.JSON(w, http.StatusOK, out)
}

// HandleProposeRule is POST /rules/{rule_id}/proposals.
func (rt Routes) HandleProposeRule(w http.ResponseWriter, r *http.Request) {
	h := rt.Host
	var b ruleProposalBody
	if !h.Decode(w, r, &b) {
		return
	}
	who, err := h.Writer(r)
	if err != nil {
		h.Fail(w, err)
		return
	}
	out, err := store.ProposeRule(r.Context(), h.DB(), who, b.IdempotencyKey, chi.URLParam(r, "rule_id"), []byte(b.Text), b.Reason)
	if err != nil {
		h.Fail(w, err)
		return
	}
	h.JSON(w, http.StatusOK, out)
}

// HandleSignRule is POST /rules/{rule_id}/sign.
func (rt Routes) HandleSignRule(w http.ResponseWriter, r *http.Request) {
	h := rt.Host
	var b ruleSignBody
	if !h.Decode(w, r, &b) {
		return
	}
	who, err := h.Writer(r)
	if err != nil {
		h.Fail(w, err)
		return
	}
	out, err := store.SignRule(r.Context(), h.DB(), who, chi.URLParam(r, "rule_id"), []byte(b.Text), b.Signature, h.Anchor(), rt.Verify)
	if err != nil {
		h.Fail(w, err)
		return
	}
	h.JSON(w, http.StatusOK, out)
}

// HandleTightenRule is POST /rules/{rule_id}/tighten.
func (rt Routes) HandleTightenRule(w http.ResponseWriter, r *http.Request) {
	h := rt.Host
	var b ruleBody
	if !h.Decode(w, r, &b) {
		return
	}
	who, err := h.ActorOf(r)
	if err != nil {
		h.Fail(w, err)
		return
	}
	out, err := store.TightenRule(r.Context(), h.DB(), who, chi.URLParam(r, "rule_id"), []byte(b.Text))
	if err != nil {
		h.Fail(w, err)
		return
	}
	h.JSON(w, http.StatusOK, out)
}

// HandleGetRule is GET /rules/{rule_id}: the rule in force now (null when
// none is: the strictest default holds) with its text.
func (rt Routes) HandleGetRule(w http.ResponseWriter, r *http.Request) {
	h := rt.Host
	id := chi.URLParam(r, "rule_id")
	rule, err := store.RuleInForce(r.Context(), h.DB(), id)
	if err != nil {
		h.Fail(w, err)
		return
	}
	if rule == nil {
		h.JSON(w, http.StatusOK, map[string]any{"rule_id": id, "in_force": nil})
		return
	}
	h.JSON(w, http.StatusOK, map[string]any{"rule_id": id, "in_force": rule.Doc(), "signed_by": rule.SignedBy})
}
