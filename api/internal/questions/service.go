package questions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

// Service is the question record: one document, one write path.
type Service struct {
	mu           sync.Mutex
	store        *store
	audit        *actuation.AuditLog
	now          func() time.Time
	post         func(context.Context, notice.Message) ([]notice.Delivery, error)
	beforeNotify func() error
	newID        func() (string, error)
}

// Result is an HTTP status and a JSON body.
type Result struct {
	Code int
	Body any
}

type caller struct {
	session   string
	principal string
	role      string
	op        string
	ref       string
}

type event struct {
	action, target, status, extra string
}

type applied struct {
	res     Result
	created *Question
	changed bool
	extra   []event
}

// New loads the statefile at path (key "questions" when path ends in that name).
func New(path string, audit *actuation.AuditLog) *Service {
	return &Service{
		store: &store{path: path},
		audit: audit,
		now:   func() time.Time { return time.Now().UTC() },
		post:  notice.Send,
		newID: randomID,
	}
}

// SetClock replaces the clock. Tests use it to expire questions.
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
	return s.now().UTC()
}

// RunSweep expires due questions until ctx ends. Workers call it.
func (s *Service) RunSweep(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 15 * time.Second
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.Sweep()
			timer.Reset(every)
		}
	}
}

// Sweep expires due questions once. A store with nothing due is left untouched,
// including a missing file: creating it would race with a test deleting the
// data directory.
func (s *Service) Sweep() {
	if !s.due() {
		return
	}
	s.apply(context.Background(), caller{
		session: "-", principal: "platform", role: string(actuation.RoleAdmin), op: "sweep",
	}, func(doc *document, now time.Time) applied {
		return applied{res: Result{Code: http.StatusOK, Body: map[string]any{"ok": true}}}
	})
}

// view is a read. It writes only when a question has expired, and then through
// the same update that a sweep uses. Listing an empty store does not create
// the questions file or its lock.
func (s *Service) view(ctx context.Context, c caller, fn func(*document, time.Time) applied) Result {
	doc, err := s.load()
	if err != nil {
		return Result{Code: http.StatusInternalServerError, Body: map[string]any{"error": "store questions: " + err.Error()}}
	}
	now := s.clock()
	doc.asOf = now
	if len(sweep(doc, now)) > 0 {
		return s.apply(ctx, c, fn)
	}
	return fn(doc, now).res
}

func (s *Service) due() bool {
	doc, err := s.load()
	if err != nil || doc == nil {
		return false
	}
	now := s.clock()
	doc.asOf = now
	return len(sweep(doc, now)) > 0
}

// Lookup reads one question (its q_ id or its number) without sweeping: a
// read that writes nothing, for readers outside the question flow (the agent
// runtime's controller). err is a store read failure; ok is false only after
// a successful read that does not contain ref.
func (s *Service) Lookup(ref string) (Question, bool, error) {
	doc, err := s.load()
	if err != nil {
		return Question{}, false, err
	}
	i, ok := doc.find(ref)
	if !ok {
		return Question{}, false, nil
	}
	return doc.Questions[i], true, nil
}

func (s *Service) load() (*document, error) {
	raw, err := s.store.readBytes()
	if err != nil {
		return nil, err
	}
	return parse(raw)
}

func (s *Service) apply(ctx context.Context, c caller, fn func(*document, time.Time) applied) Result {
	var out applied
	var evs []event
	err := s.store.update(func(doc *document) error {
		out = applied{}
		evs = nil
		now := s.clock()
		doc.asOf = now
		evs = sweep(doc, now)
		out = fn(doc, now)
		evs = append(evs, out.extra...)
		if !out.changed && len(evs) == 0 {
			return errNoChange
		}
		return nil
	})
	if errors.Is(err, errTooBig) {
		if c.op == "create" {
			return Result{Code: http.StatusInsufficientStorage, Body: map[string]any{"error": "store over capacity"}}
		}
		s.writeAudit(c, []event{{"question.store", c.ref, "error", "over capacity"}})
		return Result{Code: http.StatusInternalServerError, Body: map[string]any{"error": "store over capacity"}}
	}
	if err != nil {
		return Result{Code: http.StatusInternalServerError, Body: map[string]any{"error": "store questions: " + err.Error()}}
	}
	s.writeAudit(c, evs)
	if out.created != nil {
		s.maybeNotify(ctx, c, *out.created)
	}
	return out.res
}

func sweep(doc *document, now time.Time) []event {
	var evs []event
	for i := range doc.Questions {
		q := &doc.Questions[i]
		if q.Status != StatusOpen || now.Before(q.ExpiresAt) {
			continue
		}
		q.Status = StatusExpired
		evs = append(evs, event{"question.expire", q.ID, StatusExpired, ""})
	}
	return evs
}

func (s *Service) writeAudit(c caller, evs []event) {
	if s.audit == nil || len(evs) == 0 {
		return
	}
	actor := c.principal
	if actor == "" {
		actor = "platform"
	}
	role := actuation.Role(c.role)
	if role == "" {
		role = actuation.RoleAdmin
	}
	session := c.session
	if session == "" {
		session = "-"
	}
	for _, e := range evs {
		detail := "session=" + session + " principal=" + actor
		if e.extra != "" {
			detail += " " + e.extra
		}
		s.audit.RecordDirect(actor, role, e.action, e.target, e.status, detail)
	}
}

func randomID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "q_" + hex.EncodeToString(b[:]), nil
}

func consoleURL(id string) string {
	return "http://ops.bifrost.lan/#questions?id=" + url.QueryEscape(id)
}

func fail(code int, msg string, extra map[string]any) Result {
	body := map[string]any{"error": msg}
	for k, v := range extra {
		body[k] = v
	}
	return Result{Code: code, Body: body}
}

func (s *Service) maybeNotify(ctx context.Context, c caller, q Question) {
	if s.beforeNotify != nil {
		if err := s.beforeNotify(); err != nil {
			return
		}
	}
	s.sendCreated(ctx, c, q)
}
