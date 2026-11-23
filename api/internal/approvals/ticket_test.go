package approvals

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// Origin: Owner 2026-11-21 (law 53, TD-578): the Mac Pro and the two Mac minis
// run execution cards the Owner approves with his code. A host card is
// claimed only with a ticket the code earned, because a Console click alone
// must never let a Mac run a command.

var ticketKey = []byte("test-ticket-key-0123456789abcdef")

func hostCard(t *testing.T, svc *Service, host string) Approval {
	t.Helper()
	c := svc.create(context.Background(), "s", "owner_run_command", twoLines, "", map[string]any{
		"command": "true", "reason": "r", "host": host})
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	out := svc.approveWith(context.Background(), c.Approval.ID, withLine(c.Approval, approveInput{Channel: "console"}))
	if out.Status != http.StatusAccepted {
		t.Fatalf("approve = %d %v", out.Status, out.Body)
	}
	rec, _ := svc.get(c.Approval.ID)
	return rec
}

func ticketFor(svc *Service, rec Approval, host string, ttl time.Duration) (until time.Time, mac string) {
	until = svc.clock().Add(ttl)
	return until, TicketMAC(ticketKey, rec.ID, rec.ParamsHash, host, until)
}

func claimAs(svc *Service, role actuation.Role, host string) decided {
	return svc.claim(context.Background(), role, claimInput{ExecutorID: "exec", Host: host})
}

func TestAHostCardNamesItsHostAndRunsOnHost(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec := hostCard(t, svc, " Mini-1 ")
	if rec.Runner != "host" || rec.Params["host"] != "mini-1" || rec.Params["runner"] != "host" {
		t.Fatalf("runner %q params %v", rec.Runner, rec.Params)
	}
	bad := svc.create(context.Background(), "s", "owner_run_command", twoLines, "", map[string]any{"command": "true", "reason": "r", "host": "Mac Pro!"})
	if bad.Status != http.StatusBadRequest {
		t.Fatalf("a bad host name = %d %v", bad.Status, bad.Body)
	}
	owner := svc.create(context.Background(), "s", "owner_run_command", twoLines, "", map[string]any{"command": "true", "reason": "r", "host": "mini-1", "runner": "owner"})
	if owner.Status != http.StatusBadRequest {
		t.Fatalf("host with runner owner = %d %v", owner.Status, owner.Body)
	}
}

// Positive control: a verified ticket lets that host's executor claim once.
func TestATicketLetsItsHostClaimOnce(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	svc.SetTicketKey(ticketKey)
	rec := hostCard(t, svc, "mini-1")
	until, mac := ticketFor(svc, rec, "mini-1", 5*time.Minute)
	if out := svc.ticket(rec.ID, "mini-1", until, mac); out.Status != http.StatusOK {
		t.Fatalf("ticket = %d %v", out.Status, out.Body)
	}
	if out := claimAs(svc, actuation.RoleExecutor, "mini-1"); out.Status != http.StatusOK {
		t.Fatalf("the host's claim = %d %v", out.Status, out.Body)
	}
	got, _ := svc.get(rec.ID)
	if got.Execution.TicketUntil != nil || got.Execution.TicketHost != "" {
		t.Fatalf("the claim left the ticket: %+v", got.Execution)
	}
}

// Negative controls: each of these must not hand the card over.
func TestAHostCardIsNotClaimedWithoutItsTicket(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec := hostCard(t, svc, "mini-1")
	// No key at all: tickets are off.
	until, mac := ticketFor(svc, rec, "mini-1", 5*time.Minute)
	if out := svc.ticket(rec.ID, "mini-1", until, mac); out.Status != http.StatusServiceUnavailable {
		t.Fatalf("a ticket with no key = %d", out.Status)
	}
	svc.SetTicketKey(ticketKey)
	for name, run := range map[string]func() decided{
		"no ticket yet, the host":     func() decided { return claimAs(svc, actuation.RoleExecutor, "mini-1") },
		"an admin sign-in":            func() decided { return claimAs(svc, actuation.RoleAdmin, "") },
		"a legacy executor (no host)": func() decided { return claimAs(svc, actuation.RoleExecutor, "") },
		"another machine's executor":  func() decided { return claimAs(svc, actuation.RoleExecutor, "mini-2") },
	} {
		if out := run(); out.Status == http.StatusOK {
			t.Fatalf("%s took a host card without a ticket: %v", name, out.Body)
		}
	}
	// A forged ticket, a ticket for another host or of another card, a stale or too distant one.
	other := hostCard(t, svc, "mini-2")
	_, otherMac := ticketFor(svc, other, "mini-2", 5*time.Minute)
	farUntil, farMac := ticketFor(svc, rec, "mini-1", time.Hour)
	pastUntil, pastMac := ticketFor(svc, rec, "mini-1", -time.Minute)
	for name, out := range map[string]decided{
		"forged":             svc.ticket(rec.ID, "mini-1", until, "00"+mac[2:]),
		"another card's mac": svc.ticket(rec.ID, "mini-1", until, otherMac),
		"another host":       svc.ticket(rec.ID, "mini-2", until, mac),
		"an hour ahead":      svc.ticket(rec.ID, "mini-1", farUntil, farMac),
		"already expired":    svc.ticket(rec.ID, "mini-1", pastUntil, pastMac),
	} {
		if out.Status == http.StatusOK {
			t.Fatalf("a ticket that is %s was taken", name)
		}
	}
	if out := claimAs(svc, actuation.RoleExecutor, "mini-1"); out.Status == http.StatusOK {
		t.Fatalf("the card was claimed after only bad tickets: %v", out.Body)
	}
	// A valid ticket for mini-1 does not open the card to another machine or to the admin.
	if out := svc.ticket(rec.ID, "mini-1", until, mac); out.Status != http.StatusOK {
		t.Fatalf("the good ticket = %d %v", out.Status, out.Body)
	}
	for name, out := range map[string]decided{
		"another machine":   claimAs(svc, actuation.RoleExecutor, "mini-2"),
		"the admin":         claimAs(svc, actuation.RoleAdmin, ""),
		"a legacy executor": claimAs(svc, actuation.RoleExecutor, ""),
	} {
		if out.Status == http.StatusOK {
			t.Fatalf("%s took the card on mini-1's ticket", name)
		}
	}
	// And a ticket expires: after its time the host cannot claim either.
	svc.SetClock(func() time.Time { return until.Add(time.Second) })
	if out := claimAs(svc, actuation.RoleExecutor, "mini-1"); out.Status == http.StatusOK {
		t.Fatalf("an expired ticket claimed the card: %v", out.Body)
	}
}

// A machine's executor claims only cards that name its machine; the old host
// cards (no host) stay with the old executors.
func TestAMachineExecutorClaimsNothingElse(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	svc.SetTicketKey(ticketKey)
	c := svc.create(context.Background(), "s", "owner_run_command", twoLines, "", map[string]any{"command": "true", "reason": "r", "runner": "host"})
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	svc.approveWith(context.Background(), c.Approval.ID, withLine(c.Approval, approveInput{Channel: "console"}))
	if out := claimAs(svc, actuation.RoleExecutor, "mini-1"); out.Status == http.StatusOK {
		t.Fatalf("a machine executor took a card with no host: %v", out.Body)
	}
	if out := claimAs(svc, actuation.RoleExecutor, ""); out.Status != http.StatusOK {
		t.Fatalf("the old executor lost its host card: %d %v", out.Status, out.Body)
	}
}
