package approvals

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// Service stores approvals and runs them on approve.
type Service struct {
	mu    sync.Mutex
	store *store
	audit *actuation.AuditLog
	now   func() time.Time
}

// New loads the statefile at path (key "approvals" when path is $PLATFORM_DATA_DIR/approvals).
func New(path string, audit *actuation.AuditLog) *Service {
	return &Service{
		store: newStore(path),
		audit: audit,
		now:   func() time.Time { return time.Now().UTC() },
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

// HasExecuted reports whether an executed approval matches action and params_hash.
func (s *Service) HasExecuted(action, hash string) bool {
	if s == nil || hash == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.store.load()
	for _, a := range s.store.items {
		if a.Status == StatusExecuted && a.Action == action && a.ParamsHash == hash {
			return true
		}
	}
	return false
}

type createResult struct {
	Approval Approval
	Status   int
	Body     map[string]any
}

func (s *Service) create(ctx context.Context, requester, action, reason, rollback string, params map[string]any) createResult {
	action = strings.TrimSpace(action)
	reason = strings.TrimSpace(reason)
	rollback = strings.TrimSpace(rollback)
	requester = strings.TrimSpace(requester)
	if action == "" {
		return fail(400, "action is required")
	}
	if reason == "" {
		return fail(400, "reason is required")
	}
	act, ok := actions.ByID(action)
	if !ok {
		return fail(404, "unknown action")
	}
	norm, err := act.Normalize(params)
	if err != nil {
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
	case actions.TierC, actions.TierD:
	default:
		return fail(400, "action cannot be requested")
	}
	hash, err := actions.ParamsHash(norm)
	if err != nil {
		return fail(400, err.Error())
	}
	now := s.now()
	rec := Approval{
		ID:         newID(),
		Action:     act.ID,
		Tier:       string(tier),
		Params:     norm,
		ParamsHash: hash,
		Status:     StatusPending,
		Reason:     reason,
		Rollback:   rollback,
		Requester:  requester,
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.load(); err != nil {
		return fail(500, "load approvals: "+err.Error())
	}
	s.store.put(rec)
	if err := s.store.save(); err != nil {
		return fail(500, "store approvals: "+err.Error())
	}
	return createResult{Approval: rec, Status: 201}
}

func (s *Service) list(status string) ([]Approval, int, map[string]any) {
	switch status {
	case "", "pending", "all":
	default:
		return nil, 400, map[string]any{"error": "status must be pending or all"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.store.load()
	s.expireLocked(nil)
	out := make([]Approval, 0)
	for _, a := range s.store.items {
		if status == "all" || a.Status == StatusPending {
			out = append(out, a)
		}
	}
	sortNewest(out)
	return out, 200, nil
}

func (s *Service) get(id string) (Approval, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.store.load()
	s.expireLocked(nil)
	return s.store.get(id)
}

type decided struct {
	Status int
	Body   map[string]any
}

func (s *Service) approve(ctx context.Context, id, channel string) decided {
	channel = strings.TrimSpace(channel)
	switch channel {
	case "chat", "phone", "console":
	default:
		return decided{Status: 400, Body: map[string]any{"error": "channel must be chat, phone, or console"}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.load(); err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "load approvals: " + err.Error()}}
	}
	s.expireLocked(nil)
	rec, ok := s.store.get(id)
	if !ok {
		return decided{Status: 404, Body: map[string]any{"error": "not found"}}
	}
	if rec.Status == StatusExpired {
		return decided{Status: 409, Body: map[string]any{"error": "expired", "status": StatusExpired}}
	}
	if rec.Status != StatusPending {
		return decided{Status: 409, Body: map[string]any{"error": "already decided", "status": rec.Status}}
	}
	hash, err := actions.ParamsHash(rec.Params)
	if err != nil || hash != rec.ParamsHash {
		return decided{Status: 409, Body: map[string]any{"error": "params hash mismatch"}}
	}
	// The approve body cannot change params. Execution uses the stored map.
	result, execErr := actions.Execute(ctx, rec.Action, rec.Params)
	now := s.now()
	rec.DecidedAt = now
	rec.Channel = channel
	if execErr != nil {
		rec.Status = StatusFailed
		rec.Error = execErr.Error()
		rec.Result = nil
	} else {
		rec.Status = StatusExecuted
		rec.Result = result
		rec.Error = ""
	}
	s.store.put(rec)
	if err := s.store.save(); err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "store approvals: " + err.Error()}}
	}
	body := map[string]any{"status": rec.Status}
	if rec.Status == StatusExecuted {
		body["result"] = rec.Result
	} else {
		body["error"] = rec.Error
	}
	return decided{Status: 200, Body: body}
}

func (s *Service) reject(id, reason string) decided {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return decided{Status: 400, Body: map[string]any{"error": "reason is required"}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.load(); err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "load approvals: " + err.Error()}}
	}
	s.expireLocked(nil)
	rec, ok := s.store.get(id)
	if !ok {
		return decided{Status: 404, Body: map[string]any{"error": "not found"}}
	}
	if rec.Status == StatusExpired {
		return decided{Status: 409, Body: map[string]any{"error": "expired", "status": StatusExpired}}
	}
	if rec.Status != StatusPending {
		return decided{Status: 409, Body: map[string]any{"error": "already decided", "status": rec.Status}}
	}
	rec.Status = StatusRejected
	rec.RejectReason = reason
	rec.DecidedAt = s.now()
	s.store.put(rec)
	if err := s.store.save(); err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "store approvals: " + err.Error()}}
	}
	return decided{Status: 200, Body: map[string]any{"id": rec.ID, "status": rec.Status}}
}

// expireLocked marks pending approvals past their deadline. audit may be nil
// when the caller records the lines itself; pass the request-less audit func.
func (s *Service) expireLocked(audit func(id string)) {
	now := s.now()
	changed := false
	for i := range s.store.items {
		a := &s.store.items[i]
		if a.Status != StatusPending || !now.After(a.ExpiresAt) {
			continue
		}
		a.Status = StatusExpired
		a.DecidedAt = now
		changed = true
		if audit != nil {
			audit(a.ID)
		} else if s.audit != nil {
			s.audit.RecordDirect("platform", actuation.RoleAdmin, "approval.expire", a.ID, StatusExpired, "ttl 24h")
		}
	}
	if changed {
		_ = s.store.save()
	}
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
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("appr_%d", time.Now().UnixNano())
	}
	return "appr_" + hex.EncodeToString(b[:])
}

func fail(status int, msg string) createResult {
	return createResult{Status: status, Body: map[string]any{"error": msg}}
}

func failFields(status int, body map[string]any) createResult {
	return createResult{Status: status, Body: body}
}
