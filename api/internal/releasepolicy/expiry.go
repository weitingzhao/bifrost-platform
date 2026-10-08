package releasepolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// Reminder windows, the label values of the reminders counter.
const (
	WindowExpired = "expired"
)

var (
	expiresIn     atomic.Int64
	remindersMu   sync.Mutex
	remindersSent = map[string]int64{"48h": 0, "24h": 0, "2h": 0, WindowExpired: 0}
)

func setExpiresIn(sec int64) {
	if sec < 0 {
		sec = 0
	}
	expiresIn.Store(sec)
}

func countReminder(window string) {
	remindersMu.Lock()
	remindersSent[window]++
	remindersMu.Unlock()
}

// WriteMetrics appends the release policy series to a /metrics body.
func WriteMetrics(b *strings.Builder) {
	b.WriteString("# HELP bifrost_release_policy_expires_in_seconds Seconds until the signed release policy expires (0 when there is no valid policy)\n")
	b.WriteString("# TYPE bifrost_release_policy_expires_in_seconds gauge\n")
	fmt.Fprintf(b, "bifrost_release_policy_expires_in_seconds %d\n", expiresIn.Load())
	b.WriteString("# HELP bifrost_release_policy_reminders_sent_total Release policy reminders pushed to the Owner, by window\n")
	b.WriteString("# TYPE bifrost_release_policy_reminders_sent_total counter\n")
	remindersMu.Lock()
	defer remindersMu.Unlock()
	for _, w := range []string{"48h", "24h", "2h", WindowExpired} {
		fmt.Fprintf(b, "bifrost_release_policy_reminders_sent_total{window=%q} %d\n", w, remindersSent[w])
	}
}

// Notify pushes one message to the Owner's phone.
type Notify func(ctx context.Context, title, message string) error

// Checker sends the expiry reminders. Run it in one process only (workers).
type Checker struct {
	eng       *Engine
	pending   func() int
	notify    Notify
	statePath string
	mu        sync.Mutex
	mem       reminderState
}

type reminderState struct {
	Sent map[string]string `json:"sent"`
}

// NewChecker returns a checker. pending counts release approvals waiting for
// the Owner. statePath (statefile) keeps "sent once per window" across
// restarts; empty keeps it in memory.
func NewChecker(eng *Engine, pending func() int, notify Notify, statePath string) *Checker {
	return &Checker{eng: eng, pending: pending, notify: notify, statePath: statePath}
}

// Start runs Tick now and then every interval until ctx ends.
func (c *Checker) Start(ctx context.Context, interval time.Duration) {
	go func() {
		defer safego.Recover("releasepolicy.expiryCheck")
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			c.Tick(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Tick checks once and pushes at most one reminder.
func (c *Checker) Tick(ctx context.Context) {
	st := c.eng.Status(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	state, err := c.load()
	if err != nil {
		slog.Warn("release_policy_expiry_check", "err", err)
		return
	}
	window, key, title, msg := c.plan(st)
	slog.Info("release_policy_expiry_check",
		"valid", st.Valid, "policy_id", st.PolicyID, "remaining_seconds", st.RemainingSeconds,
		"frozen", st.Frozen, "window", window, "already_sent", window != "" && state.Sent[key] != "")
	if window == "" || state.Sent[key] != "" || c.notify == nil {
		return
	}
	if err := c.notify(ctx, title, msg); err != nil {
		slog.Warn("release_policy_expiry_check", "window", window, "err", err)
		return
	}
	now := c.eng.d.Now()
	state.Sent[key] = now.Format(time.RFC3339)
	if window != WindowExpired {
		// At 20h left the 48h push is moot: a window covers every larger one.
		due := st.RemainingSeconds
		for _, h := range st.policy.ReminderHours() {
			if int64(h)*3600 >= due {
				if wk := fmt.Sprintf("%s/%dh", st.PolicyID, h); state.Sent[wk] == "" {
					state.Sent[wk] = now.Format(time.RFC3339)
				}
			}
		}
	}
	prune(state.Sent, now.Add(-30*24*time.Hour))
	countReminder(window)
	if err := c.store(state); err != nil {
		slog.Warn("release_policy_expiry_check", "store", err)
	}
}

// plan picks the window due now and its message, or "" for none.
func (c *Checker) plan(st Status) (window, key, title, msg string) {
	if st.Valid && st.policy != nil {
		hours := st.policy.ReminderHours()
		for i := len(hours) - 1; i >= 0; i-- {
			h := hours[i]
			if st.RemainingSeconds <= int64(h)*3600 {
				window = fmt.Sprintf("%dh", h)
				key = st.PolicyID + "/" + window
				title = "Release policy expires in " + humanHours(st.RemainingSeconds)
				msg = fmt.Sprintf("Policy %s expires at %s. Releases will wait for manual approval after that. Sign a new one: %s",
					st.PolicyID, st.ExpiresAt, SignCommand)
				return window, key, title, msg
			}
		}
		return "", "", "", ""
	}
	if st.policy != nil && st.policy.Reminders.OnBlockedAfterExpiry != nil && !*st.policy.Reminders.OnBlockedAfterExpiry {
		return "", "", "", ""
	}
	n := 0
	if c.pending != nil {
		n = c.pending()
	}
	if n == 0 {
		return "", "", "", ""
	}
	id := st.PolicyID
	if id == "" {
		id = "none"
	}
	what := "No valid release policy"
	if st.Expired {
		what = "Release policy expired"
	}
	return WindowExpired, id + "/" + WindowExpired,
		fmt.Sprintf("%s, %d releases waiting", what, n),
		fmt.Sprintf("%s, %d releases waiting. Sign a new one: %s", what, n, SignCommand)
}

func humanHours(sec int64) string {
	if sec < 2*3600 {
		return fmt.Sprintf("%dm", sec/60)
	}
	return fmt.Sprintf("%dh", sec/3600)
}

func prune(sent map[string]string, before time.Time) {
	for k, v := range sent {
		if at, err := time.Parse(time.RFC3339, v); err != nil || at.Before(before) {
			delete(sent, k)
		}
	}
}

func ensure(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func (c *Checker) load() (reminderState, error) {
	if c.statePath == "" {
		c.mem.Sent = ensure(c.mem.Sent)
		return c.mem, nil
	}
	raw, err := statefile.ReadFile(c.statePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return reminderState{Sent: map[string]string{}}, nil
		}
		return reminderState{}, err
	}
	var st reminderState
	if err := json.Unmarshal(raw, &st); err != nil {
		return reminderState{}, fmt.Errorf("reminder state: %w", err)
	}
	st.Sent = ensure(st.Sent)
	return st, nil
}

func (c *Checker) store(st reminderState) error {
	if c.statePath == "" {
		c.mem = st
		return nil
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return statefile.WriteFile(c.statePath, raw, 0o644)
}
