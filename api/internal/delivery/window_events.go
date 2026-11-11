package delivery

import (
	"context"
	"time"
)

// WindowEvent describes a decision already made by the window store. Renewals
// deliberately have no event. End times describe occupancy, not observation time.
type WindowEvent struct {
	Namespace string `json:"namespace"`
	Action    string `json:"action"`
	Who       string `json:"who"`
	What      string `json:"what"`
	Env       string `json:"env"`
	Reason    string `json:"reason"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
	Holder    string `json:"holder,omitempty"`
}

// WindowRecorder observes decisions without changing their outcome. Delivery
// does not retry a Kubernetes mutation when recording fails. The runtime sink
// must deduplicate expiry observations, which can recur after a restart.
type WindowRecorder func(context.Context, WindowEvent)
type windowRecorderKey struct{}

func windowContext(ctx context.Context, recorder WindowRecorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, windowRecorderKey{}, recorder)
}
func recordWindow(ctx context.Context, ns, action string, r windowRecord, end, holder string) {
	if recorder, ok := ctx.Value(windowRecorderKey{}).(WindowRecorder); ok {
		recorder(ctx, WindowEvent{Namespace: ns, Action: action, Who: r.Who, What: r.What, Env: r.Env, Reason: r.Reason, StartedAt: r.StartedAt, EndedAt: end, Holder: holder})
	}
}
func recordWindowExpiry(ctx context.Context, ns string, r windowRecord, now time.Time) {
	if r.Who != "" && r.EndedAt == "" && !r.live(now) {
		recordWindow(ctx, ns, "expired", r, r.ExpiresAt, "")
	}
}

// BindWindowRecorder connects release-window decisions to the runtime ledger.
func (h *Handler) BindWindowRecorder(recorder WindowRecorder) { h.svc.windowRecorder = recorder }

// ObserveWindowExpiry permits the runtime to observe TTL endings after a crash,
// even when no session is reading or taking the window.
func (h *Handler) ObserveWindowExpiry(ctx context.Context) error {
	cs, _, err := h.svc.cluster.KubernetesClient()
	if err != nil {
		return err
	}
	_, _, err = getReleaseWindow(windowContext(ctx, h.svc.windowRecorder), cs, h.svc.PipelinesNamespace(), time.Now().UTC())
	return err
}
