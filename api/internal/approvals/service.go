package approvals

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
)

// Service stores approvals, runs platform actions on approve, and hands the
// rest to an executor that claims them.
type Service struct {
	mu    sync.Mutex
	store *store
	audit *actuation.AuditLog
	now   func() time.Time
	auto  AutoApprover
	// runnerID names this platform-api process as the executor of platform runs.
	runnerID string
	// requireConfirm makes confirm_number mandatory for tier D approvals from
	// phone and console. Off until the Console sends it; a value that is sent
	// is always checked.
	requireConfirm bool
	// notices counts Owner notices still being sent in the background.
	notices sync.WaitGroup
}

// AutoApprover returns the id of a signed release policy that already covers
// a tier C request, or "" when the request must wait for the Owner. The
// direct call is re-checked by the route guard, which runs and audits it.
type AutoApprover func(ctx context.Context, action string, tier actions.Tier, params map[string]any) string

// SetAutoApprover installs the release policy check. Tier D never consults it.
func (s *Service) SetAutoApprover(fn AutoApprover) {
	s.mu.Lock()
	s.auto = fn
	s.mu.Unlock()
}

// Pending returns the approvals still waiting for a decision.
func (s *Service) Pending() []Approval {
	out, _, _ := s.list(StatusPending)
	return out
}

// New loads the statefile at path (key "approvals" when path is $PLATFORM_DATA_DIR/approvals).
func New(path string, audit *actuation.AuditLog) *Service {
	host, _ := os.Hostname()
	if host == "" {
		host = "local"
	}
	return &Service{
		store:          newStore(path),
		audit:          audit,
		now:            func() time.Time { return time.Now().UTC() },
		runnerID:       "platform:" + host,
		requireConfirm: truthy(os.Getenv("APPROVAL_CONFIRM_NUMBER_REQUIRED")),
	}
}

// SetClock replaces the clock. Tests use it to expire approvals.
func (s *Service) SetClock(now func() time.Time) {
	if now == nil {
		return
	}
	s.mu.Lock()
	s.now = now
	s.mu.Unlock()
}

func (s *Service) clock() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now()
}

type createResult struct {
	Approval Approval
	Status   int
	Body     map[string]any
}

// createInput is POST /approvals. Thread and work id are labels; they are not
// params and are not in params_hash.
type createInput struct {
	Requester string
	Action    string
	Reason    string
	Rollback  string
	Params    map[string]any
	Thread    string
	WorkID    string
}

var workIDPattern = regexp.MustCompile(`^(W-[0-9]+|TD-[0-9]+|LANE-[A-Za-z0-9-]+)$`)

func (s *Service) create(ctx context.Context, requester, action, reason, rollback string, params map[string]any) createResult {
	return s.createWith(ctx, createInput{Requester: requester, Action: action, Reason: reason, Rollback: rollback, Params: params})
}

func (s *Service) createWith(ctx context.Context, in createInput) createResult {
	action := strings.TrimSpace(in.Action)
	reason := strings.TrimSpace(in.Reason)
	rollback := strings.TrimSpace(in.Rollback)
	requester := strings.TrimSpace(in.Requester)
	thread := strings.TrimSpace(in.Thread)
	work := strings.TrimSpace(in.WorkID)
	if action == "" {
		return fail(400, "action is required")
	}
	if reason == "" {
		return fail(400, "reason is required")
	}
	if strings.ContainsAny(thread, "\r\n") || len(thread) > 120 {
		return fail(400, "requester_thread must be one line of at most 120 bytes")
	}
	if work != "" && (len(work) > 40 || !workIDPattern.MatchString(work)) {
		return fail(400, "work_id must look like W-n, TD-n or LANE-name")
	}
	act, ok := actions.ByID(action)
	if !ok {
		return fail(404, "unknown action")
	}
	norm, err := act.Normalize(in.Params)
	if err != nil {
		return fail(400, err.Error())
	}
	if err = actions.NormalizeRunner(act.ID, norm); err != nil {
		return fail(400, err.Error())
	}
	if missing := act.Missing(norm); missing != "" {
		return fail(400, "missing param: "+missing)
	}
	tier := act.TierOf(ctx, norm)
	switch tier {
	case actions.TierB:
		return failFields(400, map[string]any{
			"error":  "call directly",
			"action": act.ID,
			"tier":   string(actions.TierB),
		})
	case actions.TierX:
		return failFields(403, map[string]any{
			"error":  "forbidden",
			"action": act.ID,
			"tier":   string(actions.TierX),
		})
	case actions.TierC:
		s.mu.Lock()
		auto := s.auto
		s.mu.Unlock()
		if auto != nil {
			if policyID := auto(ctx, act.ID, tier, norm); policyID != "" {
				return failFields(400, map[string]any{
					"error":            "call directly",
					"action":           act.ID,
					"tier":             string(actions.TierC),
					"auto_approved_by": policyID,
				})
			}
		}
	case actions.TierD:
	default:
		return fail(400, "action cannot be requested")
	}
	hash, err := actions.ParamsHash(norm)
	if err != nil {
		return fail(400, err.Error())
	}
	desc := act.Describe(ctx, norm, reason)
	now := s.clock()
	rec := Approval{
		ID:              newID(),
		Action:          act.ID,
		Tier:            string(tier),
		Params:          norm,
		ParamsHash:      hash,
		Status:          StatusPending,
		Reason:          reason,
		Rollback:        rollback,
		Requester:       requester,
		CreatedAt:       now,
		ExpiresAt:       now.Add(ttl),
		Env:             desc.Env,
		Summary:         desc.Summary,
		KeyParams:       desc.KeyParams,
		Runner:          act.RunnerOf(norm),
		RequesterThread: thread,
		WorkID:          work,
	}
	err = s.store.update(func(doc *file) error {
		rec.Number = doc.nextNumber()
		doc.put(rec)
		return nil
	})
	if err != nil {
		return fail(500, "store approvals: "+err.Error())
	}
	return createResult{Approval: rec, Status: 201}
}

// listFilter: "" and pending (default), open (pending, approved, running,
// unknown), all, or one exact status.
func listFilter(status string) (func(Approval) bool, bool) {
	switch status {
	case "", StatusPending:
		return func(a Approval) bool { return a.Status == StatusPending }, true
	case "open":
		return Approval.open, true
	case "all":
		return func(Approval) bool { return true }, true
	case StatusApproved, StatusRunning, StatusExecuted, StatusFailed, StatusRejected, StatusExpired, StatusUnknown:
		return func(a Approval) bool { return a.Status == status }, true
	}
	return nil, false
}

func (s *Service) list(status string) ([]Approval, int, map[string]any) {
	keep, ok := listFilter(status)
	if !ok {
		return nil, 400, map[string]any{"error": "status must be pending, open, all, or one status"}
	}
	doc, err := s.sweptDoc()
	if err != nil {
		return nil, 500, map[string]any{"error": "load approvals: " + err.Error()}
	}
	out := make([]Approval, 0)
	for _, a := range doc.Approvals {
		if keep(a) {
			out = append(out, a)
		}
	}
	sortNewest(out)
	return out, 200, nil
}

func (s *Service) get(ref string) (Approval, bool) {
	doc, err := s.sweptDoc()
	if err != nil {
		return Approval{}, false
	}
	return doc.find(strings.TrimSpace(ref))
}

// sweptDoc reads the document and, when a deadline has passed, writes the
// transitions first (pending → expired, approved → expired, running → unknown).
func (s *Service) sweptDoc() (file, error) {
	doc, err := s.store.read()
	if err != nil {
		return doc, err
	}
	now := s.clock()
	probe := doc
	probe.Approvals = append([]Approval(nil), doc.Approvals...)
	if len(sweep(&probe, now)) == 0 {
		return doc, nil
	}
	var evs []event
	var out file
	err = s.store.update(func(d *file) error {
		evs = sweep(d, now)
		out = *d
		if len(evs) == 0 {
			return errNoChange
		}
		return nil
	})
	if err != nil {
		return doc, err
	}
	s.record(nil, evs)
	if len(evs) == 0 {
		return s.store.read()
	}
	return out, nil
}

// sweep applies the time-based transitions in place and returns their audit lines.
func sweep(doc *file, now time.Time) []event {
	var evs []event
	for i := range doc.Approvals {
		a := &doc.Approvals[i]
		switch a.Status {
		case StatusPending:
			if now.After(a.ExpiresAt) {
				a.Status = StatusExpired
				a.DecidedAt = now
				evs = append(evs, event{"approval.expire", a.ID, StatusExpired, "ttl 24h"})
			}
		case StatusApproved:
			if e := a.Execution; e != nil && !e.Deadline.IsZero() && now.After(e.Deadline) {
				a.Status = StatusExpired
				a.Error = "not executed before the execution deadline"
				if e.LastRefusal != "" {
					a.Error += "; last refusal: " + e.LastRefusal
				}
				evs = append(evs, event{"approval.expire", a.ID, StatusExpired, "execution deadline " + e.Deadline.Format(time.RFC3339)})
			}
		case StatusRunning:
			if e := a.Execution; e != nil && !e.LeaseExpiresAt.IsZero() && now.After(e.LeaseExpiresAt.Add(unknownGrace)) {
				a.Status = StatusUnknown
				a.Error = "executor lost: lease lapsed with no result"
				evs = append(evs, event{"approval.unknown", a.ID, StatusUnknown, "executor=" + e.ExecutorID})
			}
		}
	}
	return evs
}

type decided struct {
	Status int
	Body   map[string]any
	// events are audit lines for the handler to record with the caller.
	events []event
}

// approveInput is POST /approvals/{id}/approve.
type approveInput struct {
	Channel string
	// Confirm is confirm_number as sent; nil when absent.
	Confirm *int
	// ApprovalLine and ParamsHash are the caller's echo of the canonical line
	// and the stored params hash. Both are required and compared strictly.
	ApprovalLine string
	ParamsHash   string
}

func (s *Service) approve(ctx context.Context, id, channel string) decided {
	in := approveInput{Channel: channel}
	if rec, ok := s.find(id); ok {
		in.ApprovalLine = CanonicalApprovalLine(rec)
		in.ParamsHash = rec.ParamsHash
	}
	return s.approveWith(ctx, id, in)
}

func (s *Service) approveWith(ctx context.Context, ref string, in approveInput) decided {
	channel := strings.TrimSpace(in.Channel)
	switch channel {
	case "chat", "phone", "console":
	default:
		return decided{Status: 400, Body: map[string]any{"error": "channel must be chat, phone, or console"}}
	}
	now := s.clock()
	var rec Approval
	var refusal *decided
	var evs []event
	err := s.store.update(func(doc *file) error {
		refusal = nil
		evs = sweep(doc, now)
		found, ok := doc.find(strings.TrimSpace(ref))
		if !ok {
			refusal = &decided{Status: 404, Body: map[string]any{"error": "not found"}}
			return keepSwept(evs)
		}
		if found.Status == StatusExpired {
			refusal = &decided{Status: 409, Body: map[string]any{"error": "expired", "status": StatusExpired}}
			return keepSwept(evs)
		}
		if found.Status != StatusPending {
			refusal = &decided{Status: 409, Body: map[string]any{"error": "already decided", "status": found.Status}}
			return keepSwept(evs)
		}
		hash, err := actions.ParamsHash(found.Params)
		if err != nil || hash != found.ParamsHash {
			refusal = &decided{Status: 409, Body: map[string]any{"error": "params hash mismatch"}}
			return keepSwept(evs)
		}
		if d := consoleOnly(found, channel); d != nil {
			refusal = d
			return keepSwept(evs)
		}
		line := CanonicalApprovalLine(found)
		if in.ApprovalLine != line || in.ParamsHash != found.ParamsHash {
			msg := "approval_line does not match"
			if in.ApprovalLine == line {
				msg = "params_hash does not match"
			} else if in.ParamsHash != found.ParamsHash {
				msg = "approval_line and params_hash do not match"
			}
			refusal = &decided{Status: http.StatusConflict, Body: map[string]any{
				"error":         msg,
				"approval_line": line,
				"params_hash":   found.ParamsHash,
			}}
			return keepSwept(evs)
		}
		if d := s.checkConfirm(found, channel, in.Confirm); d != nil {
			refusal = d
			return keepSwept(evs)
		}
		found.DecidedAt = now
		found.Channel = channel
		found.ApprovedLine = line
		found.Status = StatusApproved
		found.Result = nil
		found.Error = ""
		runner := found.Runner
		if runner == "" {
			runner = actions.RunnerPlatform
			found.Runner = runner
		}
		found.Execution = &Execution{Deadline: now.Add(deadlineFor(runner))}
		if runner == actions.RunnerPlatform {
			s.claimLocked(&found, s.runnerID, now)
		} else {
			found.Result = actions.Handoff(found.Action, found.ID, found.Params)
		}
		doc.put(found)
		rec = found
		return nil
	})
	if err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "store approvals: " + err.Error()}}
	}
	s.record(nil, evs)
	if refusal != nil {
		return *refusal
	}
	if rec.Status == StatusRunning {
		rec, _ = s.runPlatform(ctx, rec)
	}
	return decided{Status: decisionCode(rec), Body: decisionBody(rec)}
}

// keepSwept writes sweep transitions found on the way to a refusal.
func keepSwept(evs []event) error {
	if len(evs) == 0 {
		return errNoChange
	}
	return nil
}

// consoleOnly refuses a chat approval of tier D (ADR §5, Owner 2026-10-10):
// a token holder could otherwise skip the MCP client's own refusal.
// Rejection carries no channel and stays open on every route.
func consoleOnly(rec Approval, channel string) *decided {
	if rec.Tier != string(actions.TierD) || channel != "chat" {
		return nil
	}
	ref := rec.ID
	if rec.Number != 0 {
		ref = fmt.Sprintf("#%d (%s)", rec.Number, rec.ID)
	}
	body := map[string]any{
		"error":       "tier D is approved on Console only: " + ref,
		"id":          rec.ID,
		"tier":        rec.Tier,
		"console_url": approvalnotify.ConsoleClickPrefix + rec.ID,
	}
	if rec.Number != 0 {
		body["number"] = rec.Number
	}
	return &decided{Status: http.StatusForbidden, Body: body}
}

// checkConfirm: tier D (phone or console; chat is refused above) must repeat
// the number.
func (s *Service) checkConfirm(rec Approval, channel string, confirm *int) *decided {
	if rec.Tier != string(actions.TierD) || channel == "chat" || rec.Number == 0 {
		return nil
	}
	if confirm == nil {
		if s.requireConfirm {
			return &decided{Status: 400, Body: map[string]any{"error": "confirm_number required", "number": rec.Number}}
		}
		return nil
	}
	if *confirm != rec.Number {
		return &decided{Status: 409, Body: map[string]any{"error": "confirm_number does not match", "number": rec.Number}}
	}
	return nil
}

func decisionCode(rec Approval) int {
	if rec.Status == StatusApproved || rec.Status == StatusRunning {
		return http.StatusAccepted
	}
	return http.StatusOK
}

func decisionBody(rec Approval) map[string]any {
	body := map[string]any{"status": rec.Status, "id": rec.ID, "runner": rec.Runner}
	if rec.Number != 0 {
		body["number"] = rec.Number
	}
	switch rec.Status {
	case StatusExecuted:
		body["result"] = rec.Result
	case StatusApproved:
		if rec.Result != nil {
			body["result"] = rec.Result
		}
		if e := rec.Execution; e != nil {
			body["deadline"] = e.Deadline
			if e.LastRefusal != "" {
				body["last_refusal"] = e.LastRefusal
				body["next_attempt_at"] = e.NextAttemptAt
			}
		}
	default:
		body["error"] = rec.Error
	}
	return body
}

func (s *Service) reject(ref, reason string) decided {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return decided{Status: 400, Body: map[string]any{"error": "reason is required"}}
	}
	now := s.clock()
	var rec Approval
	var refusal *decided
	var evs []event
	err := s.store.update(func(doc *file) error {
		refusal = nil
		evs = sweep(doc, now)
		found, ok := doc.find(strings.TrimSpace(ref))
		if !ok {
			refusal = &decided{Status: 404, Body: map[string]any{"error": "not found"}}
			return keepSwept(evs)
		}
		if found.Status == StatusExpired {
			refusal = &decided{Status: 409, Body: map[string]any{"error": "expired", "status": StatusExpired}}
			return keepSwept(evs)
		}
		if found.Status != StatusPending {
			refusal = &decided{Status: 409, Body: map[string]any{"error": "already decided", "status": found.Status}}
			return keepSwept(evs)
		}
		found.Status = StatusRejected
		found.RejectReason = reason
		found.DecidedAt = now
		doc.put(found)
		rec = found
		return nil
	})
	if err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "store approvals: " + err.Error()}}
	}
	s.record(nil, evs)
	if refusal != nil {
		return *refusal
	}
	return decided{Status: 200, Body: map[string]any{"id": rec.ID, "status": rec.Status}}
}

// RecordDeliveries appends notification results to an approval (S0-0b).
func (s *Service) RecordDeliveries(ref string, ds []Delivery) error {
	if len(ds) == 0 {
		return nil
	}
	return s.store.update(func(doc *file) error {
		found, ok := doc.find(strings.TrimSpace(ref))
		if !ok {
			return fmt.Errorf("approval %s not found", ref)
		}
		found.Deliveries = append(found.Deliveries, ds...)
		doc.put(found)
		return nil
	})
}

// event is one audit line, written after the state change it describes is stored.
type event struct {
	action, target, status, detail string
}

// record audits evs and pushes the ones that need the Owner.
func (s *Service) record(r *http.Request, evs []event) {
	s.writeAudit(r, evs)
	s.noticeEvents(evs)
}

func (s *Service) writeAudit(r *http.Request, evs []event) {
	if s.audit == nil {
		return
	}
	for _, e := range evs {
		if r != nil {
			s.audit.Record(r, e.action, e.target, e.status, e.detail)
			continue
		}
		s.audit.RecordDirect("platform", actuation.RoleAdmin, e.action, e.target, e.status, e.detail)
	}
}

// parseNumber accepts "57" and "#57".
func parseNumber(ref string) (int, bool) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "#")
	if ref == "" || len(ref) > 9 {
		return 0, false
	}
	n, err := strconv.Atoi(ref)
	if err != nil || n <= 0 || strconv.Itoa(n) != ref {
		return 0, false
	}
	return n, true
}

func sortNewest(list []Approval) {
	// newest created first; stable enough for the console
	for i := 1; i < len(list); i++ {
		j := i
		for j > 0 && list[j].CreatedAt.After(list[j-1].CreatedAt) {
			list[j], list[j-1] = list[j-1], list[j]
			j--
		}
	}
}

func newID() string {
	return "appr_" + randomHex(8)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

func fail(status int, msg string) createResult {
	return createResult{Status: status, Body: map[string]any{"error": msg}}
}

func failFields(status int, body map[string]any) createResult {
	return createResult{Status: status, Body: body}
}
