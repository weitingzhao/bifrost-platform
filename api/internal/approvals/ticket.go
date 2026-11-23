package approvals

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// A host card (params.host: the card runs on one named machine, such as a Mac
// mini) is claimed only with a ticket (TD-578, law 53, 2026-11-21). The
// Owner's one-time code is the lock of every owner card, and only the cluster's
// owner-exec checks it. A host card is therefore never claimable on the
// strength of a Console click: owner-exec checks the code and then tells the
// platform with a ticket it alone can make, a keyed hash of the card's id, its
// params hash (the exact command and host), the host and an expiry. The
// platform holds the key but never the one-time seed. The machine's executor
// claims the card once, and the claim uses the ticket up.
//
// Without a key (APPROVAL_TICKET_KEY unset) no ticket is ever accepted, so no
// host card is claimed: the feature is off until the key is installed.

const (
	// ticketMaxAge is how far ahead a ticket may expire.
	ticketMaxAge = 15 * time.Minute
	// hostTokenPrefix names the tokens of machine executors: host-<name>.
	hostTokenPrefix = "host-"
)

// SetTicketKey installs the key tickets are made with. Empty turns tickets off.
func (s *Service) SetTicketKey(key []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ticketKey = append([]byte(nil), key...)
}

// TicketMAC is the ticket owner-exec makes: v1|id|params_hash|host|unix expiry.
func TicketMAC(key []byte, id, paramsHash, host string, until time.Time) string {
	m := hmac.New(sha256.New, key)
	fmt.Fprintf(m, "v1|%s|%s|%s|%d", id, paramsHash, host, until.Unix())
	return hex.EncodeToString(m.Sum(nil))
}

// hostOfPrincipal is the machine a token speaks for: host-<name> gives name.
func hostOfPrincipal(name string) string {
	if strings.HasPrefix(name, hostTokenPrefix) {
		return strings.TrimPrefix(name, hostTokenPrefix)
	}
	return ""
}

// hostOfCard is the machine a card is for, "" for a card with no host.
func hostOfCard(a Approval) string {
	h, _ := a.Params["host"].(string)
	return h
}

// hostClaimable says whether the claimer may take this card. A card with a
// host is claimable only by that machine's own token, with a live ticket for
// exactly that host; a machine's token claims nothing else. The Owner's admin
// token has no host and so claims no host card (its owner-run fallback is for
// cards that name no machine).
func hostClaimable(a Approval, claimerHost string, now time.Time) bool {
	cardHost := hostOfCard(a)
	if cardHost == "" {
		return claimerHost == ""
	}
	e := a.Execution
	return claimerHost == cardHost && e != nil && e.TicketHost == cardHost && e.TicketUntil != nil && e.TicketUntil.After(now)
}

// HandleTicket serves the ticket route, admin role, body {host, until (unix
// seconds), mac}. The platform checks the mac with its key and that the card
// is approved, runs on that host and the ticket is neither expired nor too far
// ahead; then the ticket is kept on the card until one claim uses it.
func (s *Service) HandleTicket(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Host  string `json:"host"`
		Until int64  `json:"until"`
		MAC   string `json:"mac"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out := s.ticket(chi.URLParam(r, "id"), strings.ToLower(strings.TrimSpace(body.Host)), time.Unix(body.Until, 0), strings.TrimSpace(body.MAC))
	if out.Status == http.StatusOK && s.audit != nil {
		id, _ := out.Body["id"].(string)
		s.writeAudit(r, []event{{"approval.ticket", id, StatusApproved, "host=" + strings.ToLower(strings.TrimSpace(body.Host))}})
	}
	writeJSON(w, out.Status, out.Body)
}

func (s *Service) ticket(ref, host string, until time.Time, mac string) decided {
	s.mu.Lock()
	key := append([]byte(nil), s.ticketKey...)
	s.mu.Unlock()
	if len(key) == 0 {
		return decided{Status: http.StatusServiceUnavailable, Body: map[string]any{"error": "tickets are not configured"}}
	}
	now := s.clock()
	if !until.After(now) || until.After(now.Add(ticketMaxAge)) {
		return decided{Status: http.StatusBadRequest, Body: map[string]any{"error": "a ticket expires within the next 15 minutes"}}
	}
	var rec Approval
	var refusal *decided
	err := s.store.update(func(doc *file) error {
		refusal = nil
		found, ok := doc.find(strings.TrimSpace(ref))
		if !ok {
			refusal = &decided{Status: 404, Body: map[string]any{"error": "not found"}}
			return errNoChange
		}
		switch {
		case found.Status != StatusApproved:
			refusal = &decided{Status: 409, Body: map[string]any{"error": "a ticket is for an approved card", "status": found.Status}}
		case hostOfCard(found) == "" || hostOfCard(found) != host:
			refusal = &decided{Status: 409, Body: map[string]any{"error": "the card is not for that host"}}
		case !hmac.Equal([]byte(mac), []byte(TicketMAC(key, found.ID, found.ParamsHash, host, until))):
			refusal = &decided{Status: 403, Body: map[string]any{"error": "the ticket does not verify"}}
		}
		if refusal != nil {
			return errNoChange
		}
		if found.Execution == nil {
			found.Execution = &Execution{}
		}
		found.Execution.TicketHost, found.Execution.TicketUntil = host, &until
		doc.put(found)
		rec = found
		return nil
	})
	if err != nil && err != errNoChange {
		return decided{Status: 500, Body: map[string]any{"error": "store approvals: " + err.Error()}}
	}
	if refusal != nil {
		return *refusal
	}
	return decided{Status: 200, Body: map[string]any{"id": rec.ID, "host": host, "ticket_until": until}}
}
