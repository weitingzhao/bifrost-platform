package gatecluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ConfigMapGVR is the core ConfigMap, read through the dynamic client like
// the runs.
var ConfigMapGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

// The release window's ConfigMap and its key (delivery's window store
// writes them; the gate only reads).
const (
	WindowConfigMap = "bifrost-release-window"
	windowDataKey   = "window.json"
)

// windowRecord is what the gate reads of window.json: who holds it, for
// what, since when, until when, whether it ended, and who waits.
type windowRecord struct {
	Who       string `json:"who"`
	What      string `json:"what"`
	StartedAt string `json:"started_at"`
	ExpiresAt string `json:"expires_at"`
	EndedAt   string `json:"ended_at"`
	Queue     []struct {
		Who string `json:"who"`
	} `json:"queue"`
	Reserved *struct {
		Who string `json:"who"`
	} `json:"reserved"`
}

// parseWindow reads the record from the ConfigMap; a missing or unreadable
// record is no window.
func parseWindow(cm *unstructured.Unstructured) windowRecord {
	var r windowRecord
	if cm == nil {
		return r
	}
	raw, _, _ := unstructured.NestedString(cm.Object, "data", windowDataKey)
	_ = json.Unmarshal([]byte(raw), &r)
	return r
}

// live is delivery's rule: someone holds it and its expiry has not passed (a
// release ends it by setting the expiry to now).
func (r windowRecord) live(now time.Time) bool {
	if strings.TrimSpace(r.Who) == "" {
		return false
	}
	exp, err := time.Parse(time.RFC3339, strings.TrimSpace(r.ExpiresAt))
	return err != nil || now.Before(exp)
}

// digest is the window as a reader waits on it: holder, scope, since, live
// or ended, the queue and the reservation. A renewal moves only the expiry
// and changes none of it, so renewals are not events (delivery's window
// events have none either).
func (r windowRecord) digest(now time.Time) string {
	state := "ended"
	if r.live(now) {
		state = "live"
	}
	parts := []string{r.Who, r.What, r.StartedAt, state}
	for _, q := range r.Queue {
		parts = append(parts, "q:"+q.Who)
	}
	if r.Reserved != nil {
		parts = append(parts, "r:"+r.Reserved.Who)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:8])
}

// releasedWithin: an end this close to when the gate saw it is a release
// (delivery ends a released window now); an older one is an expiry written
// later (or not written at all).
const releasedWithin = 90 * time.Second

// windowChange is what the window's state says changed against the stream's
// last event: the body to append, or false when nothing a reader waits on
// changed. An ended window the ledger never saw held is not news.
func windowChange(r windowRecord, now time.Time, lastBody map[string]any) (map[string]any, bool) {
	d := r.digest(now)
	if lastBody != nil && lastBody["digest"] == d {
		return nil, false
	}
	sameHolder := lastBody != nil && lastBody["who"] == r.Who && lastBody["since"] == r.StartedAt && r.Who != ""
	lastChange, _ := lastBody["change"].(string)
	var change string
	switch {
	case r.live(now) && sameHolder:
		change = ChangeQueued // only the queue or the reservation moved
	case r.live(now):
		change = ChangeHeld
	case sameHolder && (lastChange == ChangeReleased || lastChange == ChangeExpired):
		change = ChangeQueued // it had ended; the queue moved
	case lastBody == nil && len(r.Queue) == 0:
		return nil, false
	case lastBody == nil:
		change = ChangeQueued // ended before the gate first looked; someone waits
	case !sameHolder && lastChange != ChangeHeld && lastChange != ChangeQueued:
		change = ChangeQueued // no holder either side; the queue moved
	default:
		change = ChangeExpired
		if end, err := time.Parse(time.RFC3339, strings.TrimSpace(r.EndedAt)); err == nil && now.Sub(end) <= releasedWithin {
			change = ChangeReleased
		}
	}
	b := map[string]any{"subject": "window", "ref": WindowRef, "key": WindowKey, "window": WindowKey,
		"change": change, "queue": len(r.Queue), "digest": d}
	for k, v := range map[string]string{"who": r.Who, "what": r.What, "since": r.StartedAt} {
		if v = strings.TrimSpace(v); v != "" {
			if len(v) > 256 {
				v = v[:256]
			}
			b[k] = v
		}
	}
	return b, true
}

// observeWindow says the window's state to the ledger when it changed.
func (g *Gate) observeWindow(ctx context.Context, r windowRecord) {
	now := g.now()
	d := r.digest(now)
	if d == g.winDigest {
		return // nothing moved since the ledger was last asked
	}
	if g.append(ctx, WindowStream, TypeWindowChanged, func(_ string, lastBody map[string]any) (map[string]any, bool) {
		return windowChange(r, now, lastBody)
	}) {
		g.winDigest = d
	}
}
