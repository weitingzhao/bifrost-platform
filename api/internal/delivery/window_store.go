package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	windowTTLDefault = 5
	windowLifetime   = windowTTLDefault * time.Minute
	windowTTLMax     = 60
)

var (
	windowWhatPattern = regexp.MustCompile(`^[a-z0-9-]+(,[a-z0-9-]+)*$`)
	windowWhoPattern  = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,128}$`)
)

// WindowHeld is returned when another who already holds a live window.
type WindowHeld struct {
	Who       string
	What      string
	StartedAt string
	ExpiresAt string
}

func (e *WindowHeld) Error() string {
	return fmt.Sprintf("release window held by %s (what=%s held since=%s expires=%s)", e.Who, e.What, e.StartedAt, e.ExpiresAt)
}

// WindowNotHolder is returned when a non-holder tries to release the window.
type WindowNotHolder struct {
	Who string
}

func (e *WindowNotHolder) Error() string {
	return fmt.Sprintf("only the holder can release the window (who=%s)", e.Who)
}

// WindowChanged is returned when the window changes during a release.
type WindowChanged struct{}

func (e *WindowChanged) Error() string {
	return "release window changed; read it again before releasing"
}

type windowRecord struct {
	Who       string `json:"who"`
	What      string `json:"what"`
	Env       string `json:"env"`
	PID       int    `json:"pid"`
	Host      string `json:"host"`
	StartedAt string `json:"started_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Reason    string `json:"reason,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
	// Queue, Reserved and Renewals: window_queue.go. A release or an expiry
	// keeps the queue; only the queue's own writes change it.
	Queue    []queueEntry `json:"queue,omitempty"`
	Reserved *queueEntry  `json:"reserved,omitempty"`
	Renewals int          `json:"renewals,omitempty"`
	// QueueRev counts queue-only writes; a run lease is not fenced by one.
	QueueRev int `json:"queue_rev,omitempty"`
}

type windowInput struct {
	What       string
	Who        string
	Reason     string
	Env        string
	TTLMinutes int
	// Queue asks to wait behind a live holder instead of a 409.
	Queue        bool
	Thread       string
	Resume       string
	QueueMinutes int
}

func (r windowRecord) asMap() map[string]any {
	raw, _ := json.Marshal(r)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func (r windowRecord) live(now time.Time) bool {
	return r.Who != "" && !releaseWindowExpired(r.asMap(), now)
}

func validateWindowInput(in windowInput) (windowInput, error) {
	in.What = strings.TrimSpace(in.What)
	in.Who = strings.TrimSpace(in.Who)
	in.Reason = strings.TrimSpace(in.Reason)
	if !windowWhatPattern.MatchString(in.What) {
		return in, fmt.Errorf("what must be comma-separated repo names")
	}
	if !windowWhoPattern.MatchString(in.Who) {
		return in, fmt.Errorf("who has characters outside the allow-list")
	}
	if len(in.Reason) > 256 || strings.ContainsAny(in.Reason, "\n\r") {
		return in, fmt.Errorf("reason must not contain newlines and must be at most 256 characters")
	}
	in.Env = windowEnv(in.Env)
	if in.TTLMinutes == 0 {
		in.TTLMinutes = windowTTLDefault
	}
	if in.TTLMinutes < 1 || in.TTLMinutes > windowTTLMax {
		return in, fmt.Errorf("ttl_minutes must be between 1 and %d", windowTTLMax)
	}
	return validateQueueInput(in)
}

func windowEnv(reason string) string {
	switch reason {
	case "hold", "dev", "stg", "prod":
		return reason
	default:
		return "hold"
	}
}

func putReleaseWindow(ctx context.Context, cs kubernetes.Interface, ns string, in windowInput, now time.Time) (windowRecord, error) {
	in, err := validateWindowInput(in)
	if err != nil {
		return windowRecord{}, err
	}
	var last error
	attempts := 5
	if in.Queue {
		attempts = 20 // every concurrent enqueue is a conflict for the others
	}
	for attempt := 0; attempt < attempts; attempt++ {
		rec, err := writeReleaseWindowOnce(ctx, cs, ns, in, now)
		if err == nil {
			return rec, nil
		}
		if _, held := err.(*WindowHeld); held {
			return windowRecord{}, err
		}
		if _, queued := err.(*WindowQueued); queued {
			return windowRecord{}, err
		}
		if apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err) {
			last = err
			continue
		}
		return windowRecord{}, err
	}
	if last == nil {
		last = fmt.Errorf("release window busy")
	}
	return windowRecord{}, last
}

func writeReleaseWindowOnce(ctx context.Context, cs kubernetes.Interface, ns string, in windowInput, now time.Time) (windowRecord, error) {
	cms := cs.CoreV1().ConfigMaps(ns)
	cm, err := cms.Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	missing := apierrors.IsNotFound(err)
	if err != nil && !missing {
		return windowRecord{}, err
	}
	var current windowRecord
	if !missing {
		current, _ = parseWindowCM(cm)
	}
	next := windowRecord{
		Who:       in.Who,
		What:      in.What,
		Env:       in.Env,
		PID:       0,
		Host:      "",
		StartedAt: now.UTC().Format(time.RFC3339),
		ExpiresAt: now.UTC().Add(time.Duration(in.TTLMinutes) * time.Minute).Format(time.RFC3339),
		Reason:    in.Reason,
		Queue:     removeQueued(current.Queue, in.Who),
	}
	if in.Reason == "reserved" {
		next.Reason = ""
	}
	live := current.live(now)
	if !live {
		// A free window belongs to the queue head first: reserve it for the
		// head here too, so no caller slips in before the advancer's tick.
		if head, ok := queueHead(current.Queue, now); ok && head.Who != in.Who {
			reserved := current
			reserveHead(&reserved, now, loadWindowQueueConfig().Reserve)
			current, live = reserved, true
			if !in.Queue {
				if err := writeWindowCM(ctx, cs, ns, cm, reserved); err != nil {
					return windowRecord{}, err
				}
			}
		}
	}
	if live && current.Who != in.Who {
		if in.Queue {
			queued := current
			pos, err := enqueue(&queued, in, now)
			if err != nil {
				return windowRecord{}, err
			}
			if err := writeWindowCM(ctx, cs, ns, cm, queued); err != nil {
				return windowRecord{}, err
			}
			return windowRecord{}, &WindowQueued{Position: pos, Holder: queued, Queue: queued.Queue}
		}
		recordWindow(ctx, ns, "refused", next, "", current.Who)
		return windowRecord{}, &WindowHeld{Who: current.Who, What: current.What, StartedAt: current.StartedAt, ExpiresAt: current.ExpiresAt}
	}
	claimed := live && current.Reserved != nil
	if live && !claimed {
		next.StartedAt = current.StartedAt
		if next.StartedAt == "" {
			next.StartedAt = now.UTC().Format(time.RFC3339)
		}
		next.Renewals = current.Renewals + 1
	}
	var target *corev1.ConfigMap
	if !missing {
		target = cm
	}
	err = writeWindowCM(ctx, cs, ns, target, next)
	if err == nil {
		recordWindowExpiry(ctx, ns, current, now)
		if !live || claimed {
			recordWindow(ctx, ns, "held", next, "", "")
		}
	}
	return next, err
}

func getReleaseWindow(ctx context.Context, cs kubernetes.Interface, ns string, now time.Time) (windowRecord, bool, error) {
	cm, err := cs.CoreV1().ConfigMaps(ns).Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return windowRecord{}, false, nil
	}
	if err != nil {
		return windowRecord{}, false, err
	}
	// A read never deletes (CUT §2.6, S3): an expired record reads as free
	// and stays, so a reader can never delete a window another caller has
	// just taken. One that does not parse is not known to be free: it is an
	// error, and callers that gate on the window treat an error as held.
	rec, ok := parseWindowCM(cm)
	if !ok {
		return windowRecord{}, false, errWindowUnreadable
	}
	if !rec.live(now) {
		recordWindowExpiry(ctx, ns, rec, now)
		return windowRecord{}, false, nil
	}
	return rec, true, nil
}

func deleteReleaseWindow(ctx context.Context, cs kubernetes.Interface, ns, who string, now time.Time) error {
	cm, err := cs.CoreV1().ConfigMaps(ns).Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	rec, ok := parseWindowCM(cm)
	// Only the holder releases a live window. Clearing someone else's was
	// removed (walls review wall:104, LAW-2 二): a dead holder's window ends by
	// itself within its ttl, and clearing a live one collides with its release.
	if ok && rec.live(now) && rec.Who != strings.TrimSpace(who) {
		attempt := rec
		attempt.Who = strings.TrimSpace(who)
		attempt.StartedAt = now.UTC().Format(time.RFC3339)
		attempt.Reason = "not_holder"
		recordWindow(ctx, ns, "refused", attempt, "", rec.Who)
		return &WindowNotHolder{Who: rec.Who}
	}
	// Released by ending it now, on the version read (CUT §2.6, S3): an
	// Update carrying cm's resourceVersion, so a window taken meanwhile is
	// never ended by a stale release. No delete.
	if !ok {
		rec = windowRecord{Who: strings.TrimSpace(who), Reason: "cleared"}
	}
	previous := rec
	rec.Reserved = nil // a reservation given back is not a missed turn
	rec.ExpiresAt = now.UTC().Format(time.RFC3339)
	rec.EndedAt = rec.ExpiresAt
	body, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[releaseWindowDataKey] = string(body)
	_, err = cs.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
	if apierrors.IsConflict(err) {
		return &WindowChanged{}
	}
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err == nil {
		if previous.live(now) {
			recordWindow(ctx, ns, "released", previous, rec.ExpiresAt, "")
		} else {
			recordWindowExpiry(ctx, ns, previous, now)
		}
	}
	return err
}

// errWindowUnreadable: the window ConfigMap holds a record that does not
// parse; it is not known to be free.
var errWindowUnreadable = errors.New("release window record is unreadable")

func parseWindowCM(cm *corev1.ConfigMap) (windowRecord, bool) {
	if cm == nil || cm.Data == nil {
		return windowRecord{}, false
	}
	raw := strings.TrimSpace(cm.Data[releaseWindowDataKey])
	if raw == "" {
		return windowRecord{}, false
	}
	var rec windowRecord
	if json.Unmarshal([]byte(raw), &rec) != nil || strings.TrimSpace(rec.Who) == "" {
		return windowRecord{}, false
	}
	return rec, true
}
