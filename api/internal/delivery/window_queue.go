package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/threadinbox"
)

// Release-window queue (LANE-agent-tool-comm, locks-queue-design-v1 §2, §8).
// The queue lives in the same window.json and is written with the same CAS.
// A reservation for the queue head is the window record itself (who=head,
// reason=reserved, short expiry), so every existing gate sees it as held by
// the head and the head's PUT is the ordinary renew path.

const windowQueueLimit = 100

type queueEntry struct {
	Who    string `json:"who"`
	What   string `json:"what"`
	Thread string `json:"thread,omitempty"`
	Resume string `json:"resume,omitempty"`
	Since  string `json:"since"`
	Until  string `json:"until"`
}

// WindowQueued is returned when a caller asked to queue behind a live holder.
type WindowQueued struct {
	Position int
	Holder   windowRecord
	Queue    []queueEntry
}

func (e *WindowQueued) Error() string {
	return fmt.Sprintf("release window held by %s; queued at position %d", e.Holder.Who, e.Position)
}

type windowQueueConfig struct {
	Reserve      time.Duration
	QueueDefault int
	QueueMax     int
	StillHeld    int
}

func envPositive(name string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && n > 0 {
		return n
	}
	return def
}

func loadWindowQueueConfig() windowQueueConfig {
	return windowQueueConfig{
		Reserve:      time.Duration(envPositive("RELEASE_WINDOW_RESERVE_SECONDS", 120)) * time.Second,
		QueueDefault: envPositive("RELEASE_WINDOW_QUEUE_MINUTES_DEFAULT", 30),
		QueueMax:     envPositive("RELEASE_WINDOW_QUEUE_MINUTES_MAX", 120),
		StillHeld:    envPositive("RELEASE_WINDOW_STILL_HELD_RENEWALS", 3),
	}
}

func validateQueueInput(in windowInput) (windowInput, error) {
	in.Thread = strings.TrimSpace(in.Thread)
	in.Resume = strings.TrimSpace(in.Resume)
	if in.Thread != "" {
		to := threadinbox.Recipient(in.Thread)
		if to == "" {
			return in, fmt.Errorf("thread must be a thread address such as thread:<id>")
		}
		in.Thread = to
	}
	if len(in.Resume) > 256 || strings.ContainsAny(in.Resume, "\n\r") {
		return in, fmt.Errorf("resume must not contain newlines and must be at most 256 characters")
	}
	cfg := loadWindowQueueConfig()
	if in.QueueMinutes == 0 {
		in.QueueMinutes = cfg.QueueDefault
	}
	if in.QueueMinutes < 1 || in.QueueMinutes > cfg.QueueMax {
		return in, fmt.Errorf("queue_minutes must be between 1 and %d", cfg.QueueMax)
	}
	return in, nil
}

func entryExpired(e queueEntry, now time.Time) bool {
	until, err := time.Parse(time.RFC3339, e.Until)
	return err != nil || !now.Before(until)
}

// queueHead is the first entry that has not timed out.
func queueHead(q []queueEntry, now time.Time) (queueEntry, bool) {
	for _, e := range q {
		if !entryExpired(e, now) {
			return e, true
		}
	}
	return queueEntry{}, false
}

func removeQueued(q []queueEntry, who string) []queueEntry {
	var out []queueEntry
	for _, e := range q {
		if e.Who != who {
			out = append(out, e)
		}
	}
	return out
}

// enqueue appends in.Who, or refreshes its entry: one position per who.
// It returns the 1-based position.
func enqueue(r *windowRecord, in windowInput, now time.Time) (int, error) {
	r.QueueRev++
	until := now.UTC().Add(time.Duration(in.QueueMinutes) * time.Minute).Format(time.RFC3339)
	for i := range r.Queue {
		if r.Queue[i].Who == in.Who {
			r.Queue[i].Until = until
			if in.Thread != "" {
				r.Queue[i].Thread = in.Thread
			}
			if in.Resume != "" {
				r.Queue[i].Resume = in.Resume
			}
			return i + 1, nil
		}
	}
	if len(r.Queue) >= windowQueueLimit {
		return 0, fmt.Errorf("release window queue is full (%d)", windowQueueLimit)
	}
	r.Queue = append(r.Queue, queueEntry{Who: in.Who, What: in.What, Thread: in.Thread, Resume: in.Resume,
		Since: now.UTC().Format(time.RFC3339), Until: until})
	return len(r.Queue), nil
}

// reserveHead turns a free record into a reservation for the first live entry.
func reserveHead(r *windowRecord, now time.Time, reserve time.Duration) bool {
	head, ok := queueHead(r.Queue, now)
	if !ok {
		return false
	}
	queue := removeQueued(r.Queue, head.Who)
	*r = windowRecord{Who: head.Who, What: head.What, Env: "hold",
		StartedAt: now.UTC().Format(time.RFC3339), ExpiresAt: now.UTC().Add(reserve).Format(time.RFC3339),
		Reason: "reserved", Queue: queue, Reserved: &head}
	return true
}

// writeWindowCM writes rec on the version read (cm nil creates).
func writeWindowCM(ctx context.Context, cs kubernetes.Interface, ns string, cm *corev1.ConfigMap, rec windowRecord) error {
	body, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	cms := cs.CoreV1().ConfigMaps(ns)
	if cm == nil {
		_, err = cms.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: ns},
			Data:       map[string]string{releaseWindowDataKey: string(body)},
		}, metav1.CreateOptions{})
		return err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[releaseWindowDataKey] = string(body)
	_, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

type windowNotice struct {
	Topic, Key, To string
	Entry          queueEntry
	Holder         windowRecord
}

func notice(topic string, e queueEntry, holder windowRecord, extra string) windowNotice {
	return windowNotice{Topic: topic, Key: "window:" + strings.TrimPrefix(topic, "window.") + ":" + e.Who + ":" + e.Since + extra,
		To: e.Thread, Entry: e, Holder: holder}
}

// advanceWindowQueue is one advancer step (§2): expired reservations are
// missed, timed-out entries dropped, a free window reserved for the head.
// It returns the notices for the state it wrote or read.
func advanceWindowQueue(ctx context.Context, cs kubernetes.Interface, ns string, now time.Time, cfg windowQueueConfig) ([]windowNotice, error) {
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		cm, err := cs.CoreV1().ConfigMaps(ns).Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		rec, ok := parseWindowCM(cm)
		if !ok {
			return nil, nil
		}
		var notices []windowNotice
		changed := false
		if rec.Reserved != nil && !rec.live(now) {
			notices = append(notices, notice("window.missed", *rec.Reserved, windowRecord{}, ""))
			rec.Reserved = nil
			if rec.EndedAt == "" {
				rec.EndedAt = rec.ExpiresAt
			}
			changed = true
		}
		var keep []queueEntry
		for _, e := range rec.Queue {
			if entryExpired(e, now) {
				holder := windowRecord{}
				if rec.live(now) {
					holder = windowRecord{Who: rec.Who, What: rec.What, ExpiresAt: rec.ExpiresAt}
				}
				notices = append(notices, notice("window.queue_timeout", e, holder, ""))
				if !changed {
					rec.QueueRev++
				}
				changed = true
				continue
			}
			keep = append(keep, e)
		}
		rec.Queue = keep
		if !rec.live(now) && len(rec.Queue) > 0 {
			changed = reserveHead(&rec, now, cfg.Reserve) || changed
		}
		if changed {
			err = writeWindowCM(ctx, cs, ns, cm, rec)
			if apierrors.IsConflict(err) {
				last = err
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		if rec.live(now) && rec.Reserved != nil {
			notices = append(notices, notice("window.your_turn", *rec.Reserved, windowRecord{}, ""))
		}
		if rec.live(now) && rec.Reserved == nil && rec.Renewals >= cfg.StillHeld && len(rec.Queue) > 0 {
			holder := windowRecord{Who: rec.Who, What: rec.What, StartedAt: rec.StartedAt, ExpiresAt: rec.ExpiresAt}
			notices = append(notices, notice("window.still_held", rec.Queue[0], holder, ":"+rec.Who+":"+rec.StartedAt))
		}
		return notices, nil
	}
	return nil, last
}

type queueView struct {
	Position      int    `json:"position"`
	Who           string `json:"who"`
	What          string `json:"what"`
	WaitedSeconds int64  `json:"waited_seconds"`
	Until         string `json:"until"`
}

func viewQueue(q []queueEntry, now time.Time) []queueView {
	out := []queueView{}
	for i, e := range q {
		v := queueView{Position: i + 1, Who: e.Who, What: e.What, Until: e.Until}
		if since, err := time.Parse(time.RFC3339, e.Since); err == nil {
			v.WaitedSeconds = int64(now.Sub(since).Seconds())
		}
		out = append(out, v)
	}
	return out
}

func viewHolder(r windowRecord, now time.Time) map[string]any {
	held := int64(0)
	if since, err := time.Parse(time.RFC3339, r.StartedAt); err == nil {
		held = int64(now.Sub(since).Seconds())
	}
	return map[string]any{"who": r.Who, "what": r.What, "held_seconds": held, "expires_at": r.ExpiresAt, "reserved": r.Reserved != nil}
}
