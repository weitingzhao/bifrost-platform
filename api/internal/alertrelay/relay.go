// Package alertrelay pages the Owner through ntfy from outside the cluster.
//
// Until 2026-10-07 Alertmanager's only receiver was a webhook into STG
// platform-api's in-memory audit log, so a failed backup or a stalled WAL
// archive was "delivered" and nobody was told (TD-209). The relay runs in the
// operator plane on a Mac mini, which shares fate with neither the cluster nor
// the platform:
//
//   - POST /alerts/alertmanager takes Alertmanager's webhook body and publishes
//     one ntfy message per alert (firing and resolved);
//   - POST /alerts/heartbeat takes the always-firing Watchdog alert. When none
//     arrives for HeartbeatMax, the relay pages that Alertmanager (or the path
//     to it) is down: a dead-man's switch, since a dead Alertmanager sends
//     nothing at all.
//   - POST /alerts/notify takes {title, message, click_url, priority} and
//     publishes one ntfy message. click_url is sent as the Click header so a
//     phone tap opens the Console approval. Same bearer as the other posts.
//     The answer carries one delivery per target ({channel, target, result,
//     error}) so the platform can store it on the approval; target is a hash,
//     never the topic.
//
// It is cluster-free (no client-go) like the rest of the operator plane.
package alertrelay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Config comes from the plane's environment; the relay is off unless Enabled.
type Config struct {
	Enabled      bool
	NtfyURL      string // e.g. https://ntfy.sh
	Topic        string // the topic is the read credential: never commit it
	Token        string // bearer token Alertmanager sends
	HeartbeatMax time.Duration
	RepeatEvery  time.Duration // re-page while the heartbeat stays missing
}

// ConfigFromEnv reads ALERT_RELAY (on|off), NTFY_URL, NTFY_TOPIC,
// ALERT_RELAY_TOKEN and ALERT_RELAY_HEARTBEAT_MAX (Go duration, default 15m).
func ConfigFromEnv() Config {
	c := Config{
		Enabled:      strings.EqualFold(strings.TrimSpace(os.Getenv("ALERT_RELAY")), "on"),
		NtfyURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("NTFY_URL")), "/"),
		Topic:        strings.TrimSpace(os.Getenv("NTFY_TOPIC")),
		Token:        strings.TrimSpace(os.Getenv("ALERT_RELAY_TOKEN")),
		HeartbeatMax: 15 * time.Minute,
		RepeatEvery:  time.Hour,
	}
	if c.NtfyURL == "" {
		c.NtfyURL = "https://ntfy.sh"
	}
	if d, err := time.ParseDuration(strings.TrimSpace(os.Getenv("ALERT_RELAY_HEARTBEAT_MAX"))); err == nil && d > 0 {
		c.HeartbeatMax = d
	}
	return c
}

// Problem says why an enabled relay cannot work, or "" when it can.
func (c Config) Problem() string {
	switch {
	case c.Topic == "":
		return "NTFY_TOPIC is not set"
	case c.Token == "":
		return "ALERT_RELAY_TOKEN is not set"
	}
	return ""
}

// Message is one ntfy publication.
type Message struct {
	Title    string
	Body     string
	Priority int // 1..5
	Tags     []string
	ClickURL string // ntfy Click header; empty means no header
}

// Delivery is the outcome of one publication to one target, as returned by
// POST /alerts/notify.
type Delivery struct {
	Channel string `json:"channel"`
	// Target names the destination without revealing it: ntfy:<hash>.
	Target string `json:"target"`
	// Result is accepted (the push service took it) or failed.
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

const (
	DeliveryAccepted = "accepted"
	DeliveryFailed   = "failed"
)

// Relay receives alerts and publishes them.
type Relay struct {
	cfg    Config
	client *http.Client
	now    func() time.Time
	// target is the ntfy destination as a hash (the topic is a credential).
	target string

	mu          sync.Mutex
	started     time.Time
	lastBeat    time.Time
	missingFrom time.Time // zero while the heartbeat is healthy
	lastPage    time.Time
	sent        int
	lastErr     string
	seen        map[string]time.Time // fingerprint|status -> sent at
}

func New(cfg Config) *Relay {
	r := &Relay{cfg: cfg, client: &http.Client{Timeout: 10 * time.Second}, now: time.Now, seen: map[string]time.Time{}}
	r.started = r.now()
	sum := sha256.Sum256([]byte(cfg.NtfyURL + "/" + cfg.Topic))
	r.target = "ntfy:" + hex.EncodeToString(sum[:])[:12]
	return r
}

// Mount registers the relay's routes on an /api/v1 router.
func (r *Relay) Mount(rt chi.Router) {
	rt.Get("/alerts/relay", r.handleStatus)
	rt.Post("/alerts/alertmanager", r.authed(r.handleAlerts))
	rt.Post("/alerts/heartbeat", r.authed(r.handleHeartbeat))
	rt.Post("/alerts/notify", r.authed(r.handleNotify))
}

func (r *Relay) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		got := strings.TrimSpace(strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "))
		if r.cfg.Token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(r.cfg.Token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "relay token required"})
			return
		}
		next(w, req)
	}
}

// webhook is the part of Alertmanager's webhook body (version 4) the relay reads.
type webhook struct {
	Status string `json:"status"`
	Alerts []struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		Fingerprint string            `json:"fingerprint"`
	} `json:"alerts"`
}

const dedupeWindow = 30 * time.Minute

func (r *Relay) handleAlerts(w http.ResponseWriter, req *http.Request) {
	var body webhook
	if err := json.NewDecoder(io.LimitReader(req.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "alertmanager body: " + err.Error()})
		return
	}
	var failed []string
	published := 0
	for _, a := range body.Alerts {
		// Two routes (critical, backup) can both carry one alert: send it once.
		key := a.Fingerprint + "|" + a.Status
		if a.Fingerprint != "" && r.recentlySent(key) {
			continue
		}
		if err := r.publish(req.Context(), FormatAlert(a.Status, a.Labels, a.Annotations, a.StartsAt)); err != nil {
			failed = append(failed, err.Error())
			continue
		}
		published++
		if a.Fingerprint != "" {
			r.markSent(key)
		}
	}
	if len(failed) > 0 {
		// non-2xx makes Alertmanager retry the batch
		writeJSON(w, http.StatusBadGateway, map[string]any{"published": published, "errors": failed})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"published": published})
}

// FormatAlert turns one Alertmanager alert into an ntfy message.
func FormatAlert(status string, labels, annotations map[string]string, startsAt time.Time) Message {
	name := labels["alertname"]
	sev := labels["severity"]
	title := name
	if ns := labels["namespace"]; ns != "" {
		title += " · " + ns
	}
	var b strings.Builder
	if s := annotations["summary"]; s != "" {
		b.WriteString(s)
	}
	if d := annotations["description"]; d != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(d)
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		if k != "alertname" && k != "severity" && k != "namespace" && k != "prometheus" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+labels[k])
		}
		b.WriteString("\n" + strings.Join(parts, " "))
	}
	if !startsAt.IsZero() {
		b.WriteString("\nsince " + startsAt.UTC().Format("2006-01-02 15:04Z"))
	}
	m := Message{Title: title, Body: b.String()}
	if status == "resolved" {
		m.Title = "RESOLVED " + title
		m.Priority = 2
		m.Tags = []string{"white_check_mark"}
		return m
	}
	switch sev {
	case "critical":
		m.Priority = 5
		m.Tags = []string{"rotating_light"}
	case "warning":
		m.Priority = 4
		m.Tags = []string{"warning"}
	default:
		m.Priority = 3
	}
	if sev != "" {
		m.Title = strings.ToUpper(sev) + " " + m.Title
	}
	return m
}

func (r *Relay) recentlySent(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	at, ok := r.seen[key]
	return ok && r.now().Sub(at) < dedupeWindow
}

func (r *Relay) markSent(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for k, at := range r.seen {
		if now.Sub(at) >= dedupeWindow {
			delete(r.seen, k)
		}
	}
	r.seen[key] = now
}

// notifyRequest is the body of POST /alerts/notify.
type notifyRequest struct {
	Title    string `json:"title"`
	Message  string `json:"message"`
	ClickURL string `json:"click_url"`
	Priority int    `json:"priority"`
}

func (r *Relay) handleNotify(w http.ResponseWriter, req *http.Request) {
	var body notifyRequest
	if err := json.NewDecoder(io.LimitReader(req.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "notify body: " + err.Error()})
		return
	}
	title := strings.TrimSpace(body.Title)
	message := strings.TrimSpace(body.Message)
	if title == "" || message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title and message are required"})
		return
	}
	priority := body.Priority
	if priority == 0 {
		priority = 3
	}
	if priority < 1 || priority > 5 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "priority must be 1..5"})
		return
	}
	err := r.publish(req.Context(), Message{
		Title:    title,
		Body:     message,
		Priority: priority,
		ClickURL: strings.TrimSpace(body.ClickURL),
	})
	d := Delivery{Channel: "ntfy", Target: r.target, Result: DeliveryAccepted}
	if err != nil {
		d.Result, d.Error = DeliveryFailed, err.Error()
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "deliveries": []Delivery{d}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent", "deliveries": []Delivery{d}})
}

func (r *Relay) handleHeartbeat(w http.ResponseWriter, req *http.Request) {
	_, _ = io.Copy(io.Discard, io.LimitReader(req.Body, 1<<20))
	r.mu.Lock()
	r.lastBeat = r.now()
	wasMissing := !r.missingFrom.IsZero()
	since := r.missingFrom
	r.missingFrom = time.Time{}
	r.mu.Unlock()
	if wasMissing {
		_ = r.publish(req.Context(), Message{
			Title:    "RESOLVED Alertmanager heartbeat is back",
			Body:     "Watchdog arrives again; it was missing since " + since.UTC().Format("2006-01-02 15:04Z") + ".",
			Priority: 2, Tags: []string{"white_check_mark"},
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Check pages when the heartbeat has been missing for HeartbeatMax, and again
// every RepeatEvery while it stays missing. Before the first heartbeat, the
// relay's own start time stands in for it.
func (r *Relay) Check(ctx context.Context) {
	r.mu.Lock()
	now := r.now()
	last := r.lastBeat
	if last.IsZero() {
		last = r.started
	}
	if now.Sub(last) < r.cfg.HeartbeatMax {
		r.mu.Unlock()
		return
	}
	if r.missingFrom.IsZero() {
		r.missingFrom = last
	} else if now.Sub(r.lastPage) < r.cfg.RepeatEvery {
		r.mu.Unlock()
		return
	}
	r.lastPage = now
	r.mu.Unlock()

	seen := "never since the relay started at " + r.started.UTC().Format("2006-01-02 15:04Z")
	if !r.lastBeatTime().IsZero() {
		seen = "last at " + last.UTC().Format("2006-01-02 15:04Z")
	}
	_ = r.publish(ctx, Message{
		Title: "CRITICAL Alertmanager heartbeat missing",
		Body: fmt.Sprintf("No Watchdog for %s (%s). Alertmanager, Prometheus or the path to this Mac mini is down: "+
			"no other alert can reach you until it is back.", now.Sub(last).Round(time.Minute), seen),
		Priority: 5, Tags: []string{"rotating_light", "skull"},
	})
}

func (r *Relay) lastBeatTime() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastBeat
}

// Start runs Check every minute until ctx ends.
func (r *Relay) Start(ctx context.Context) {
	safego.Go("alertrelay.heartbeat", func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				safego.Do("alertrelay.check", func() { r.Check(ctx) })
			}
		}
	})
}

func (r *Relay) publish(ctx context.Context, m Message) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.NtfyURL+"/"+r.cfg.Topic, bytes.NewBufferString(m.Body))
	if err != nil {
		return err
	}
	req.Header.Set("Title", m.Title)
	if m.Priority > 0 {
		req.Header.Set("Priority", fmt.Sprint(m.Priority))
	}
	if len(m.Tags) > 0 {
		req.Header.Set("Tags", strings.Join(m.Tags, ","))
	}
	if m.ClickURL != "" {
		req.Header.Set("Click", m.ClickURL)
	}
	resp, err := r.client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			err = fmt.Errorf("ntfy: status %d", resp.StatusCode)
		}
	} else if r.cfg.Topic != "" {
		// A transport error quotes the URL, and the topic in it is the read
		// credential; the error reaches /alerts/relay and approval records.
		err = fmt.Errorf("%s", strings.ReplaceAll(err.Error(), r.cfg.Topic, "<topic>"))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.lastErr = err.Error()
		slog.Warn("alert relay publish failed", "title", m.Title, "err", err)
		return err
	}
	r.sent++
	return nil
}

func (r *Relay) handleStatus(w http.ResponseWriter, _ *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]any{
		"enabled":         r.cfg.Enabled,
		"started_at":      r.started.UTC(),
		"heartbeat_max_s": int(r.cfg.HeartbeatMax / time.Second),
		"published":       r.sent,
		"heartbeat_ok":    r.missingFrom.IsZero(),
	}
	if !r.lastBeat.IsZero() {
		out["last_heartbeat"] = r.lastBeat.UTC()
	}
	if r.lastErr != "" {
		out["last_error"] = r.lastErr
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
