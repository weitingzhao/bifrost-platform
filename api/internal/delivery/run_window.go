package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	authv1 "k8s.io/api/authorization/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// A run lease uses the exact ConfigMap version it acquired. A forced takeover
// fences renewal and release; neither operation retries against a newer owner.
type runWindow struct {
	cs       kubernetes.Interface
	ns, who  string
	version  string
	recorder WindowRecorder
}

func (s *Service) takeRunWindow(ctx context.Context, pipeline, run string) (*runWindow, error) {
	cs, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return nil, err
	}
	repos, _ := requiredRepos(pipeline)
	ns := s.PipelinesNamespace()
	who := run
	if len(who) > 128 {
		who = who[:128]
	}
	_, err = putReleaseWindow(windowContext(ctx, s.windowRecorder), cs, ns, windowInput{Who: who, What: sortedKeys(repos), Reason: "PipelineRun/" + run}, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	cm, err := cs.CoreV1().ConfigMaps(ns).Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	r, ok := parseWindowCM(cm)
	if !ok || r.Who != who {
		return nil, &WindowChanged{}
	}
	return &runWindow{cs: cs, ns: ns, who: who, version: cm.ResourceVersion, recorder: s.windowRecorder}, nil
}

func (w *runWindow) update(ctx context.Context, end bool) error {
	cms := w.cs.CoreV1().ConfigMaps(w.ns)
	cm, err := cms.Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if err != nil {
		return err
	}
	r, ok := parseWindowCM(cm)
	if !ok || cm.ResourceVersion != w.version || r.Who != w.who || !r.live(time.Now().UTC()) {
		return &WindowChanged{}
	}
	expires := time.Now().UTC()
	if !end {
		expires = expires.Add(windowLifetime)
	}
	previous := r
	if end {
		r.EndedAt = expires.Format(time.RFC3339)
	}
	r.ExpiresAt = expires.Format(time.RFC3339)
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	cm.Data[releaseWindowDataKey] = string(body)
	cm, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
	if err == nil {
		w.version = cm.ResourceVersion
		if end {
			recordWindow(windowContext(ctx, w.recorder), w.ns, "released", previous, r.ExpiresAt, "")
		}
	}
	return err
}

func runEnded(run *unstructured.Unstructured) bool {
	conditions, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	for _, raw := range conditions {
		c, ok := raw.(map[string]any)
		if ok && c["type"] == "Succeeded" && (c["status"] == "True" || c["status"] == "False") {
			return true
		}
	}
	return false
}

// observe renews only after a successful run read, and ends on a terminal
// condition or proven deletion. Unknown observations leave the TTL as fallback.
func (w *runWindow) observe(ctx context.Context, res dynamic.ResourceInterface, name string) (bool, error) {
	run, err := res.Get(ctx, name, metav1.GetOptions{})
	ended := apierrors.IsNotFound(err) || (err == nil && runEnded(run))
	if err != nil && !ended {
		return false, err
	}
	return ended, w.update(ctx, ended)
}

func (w *runWindow) follow(res dynamic.ResourceInterface, name string, created bool) {
	safego.Go("delivery-run-window", func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		w.followTicks(context.Background(), res, name, created, ticker.C)
	})
}

const runWindowFailureLimit = 5

func windowFenced(err error) bool {
	var changed *WindowChanged
	return errors.As(err, &changed) || apierrors.IsConflict(err)
}

func (w *runWindow) followTicks(parent context.Context, res dynamic.ResourceInterface, name string, created bool, ticks <-chan time.Time) {
	failures := 0
	for {
		select {
		case <-parent.Done():
			return
		case _, open := <-ticks:
			if !open {
				return
			}
			ctx, cancel := context.WithTimeout(parent, 10*time.Second)
			var err error
			ended := false
			// A lost create reply has not proved that a run ever existed.
			// Do not turn a 404 into an early release of an uncertain POST.
			if !created {
				_, err = res.Get(ctx, name, metav1.GetOptions{})
				created = err == nil
			}
			if err == nil {
				ended, err = w.observe(ctx, res, name)
			}
			cancel()
			if windowFenced(err) || (err == nil && ended) {
				return
			}
			if err == nil {
				failures = 0
				continue
			}
			failures++
			if failures == runWindowFailureLimit {
				w.failed("follow_failed", err)
				return
			}
			slog.Warn("release window follow failed; retrying", "run", name, "attempt", failures, "err", err)
		}
	}
}

func (w *runWindow) failed(action string, err error) {
	slog.Error("release window retries exhausted; TTL fallback", "action", action, "run", w.who, "err", err)
	recordWindow(windowContext(context.Background(), w.recorder), w.ns, action, windowRecord{Who: w.who, Reason: err.Error()}, "", "")
}

// Both API and workers scan. Fresh renewals are left alone; a stale snapshot
// can be claimed by exactly one CAS, fencing the old follower and other scanners.
// Actual Kubernetes permissions, not the UI's viewer environment, select the writer.
func (h *Handler) StartRunWindowRecovery() {
	safego.Go("delivery-run-window-recovery", func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		h.recoverRunWindowTicks(context.Background(), ticker.C)
	})
}

func (h *Handler) recoverRunWindowTicks(ctx context.Context, ticks <-chan time.Time) {
	failures := 0
	for {
		if err := h.recoverRunWindowOnce(); err == nil {
			failures = 0
		} else {
			failures++
			slog.Warn("release window recovery failed; retrying with TTL fallback", "err", err)
			if failures == runWindowFailureLimit {
				w := &runWindow{ns: h.svc.PipelinesNamespace(), recorder: h.svc.windowRecorder}
				w.failed("recovery_failed", err)
				failures = 0
			}
		}
		select {
		case <-ctx.Done():
			return
		case _, open := <-ticks:
			if !open {
				return
			}
		}
	}
}

func (h *Handler) recoverRunWindowOnce() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := h.svc
	cs, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return err
	}
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return err
	}
	res := dyn.Resource(pipelineRunGVR).Namespace(s.PipelinesNamespace())
	w, name, err := recoverRunWindow(ctx, cs, res, s.PipelinesNamespace(), s.windowRecorder)
	if windowFenced(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if w == nil {
		slog.Info("release window recovery: no run to follow", "reason", name)
		return nil
	}
	slog.Info("release window recovery: following run", "run", name)
	w.follow(res, name, true)
	return nil
}

func recoverRunWindow(ctx context.Context, cs kubernetes.Interface, res dynamic.ResourceInterface, ns string, recorder WindowRecorder) (*runWindow, string, error) {
	review, err := cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authv1.ResourceAttributes{
			Namespace: ns, Resource: "configmaps", Name: releaseWindowConfigMap, Verb: "update",
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		return nil, "", err
	}
	if !review.Status.Allowed || review.Status.Denied || review.Status.EvaluationError != "" {
		return nil, "identity cannot update release window", nil
	}
	cm, err := cs.CoreV1().ConfigMaps(ns).Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, "no window", nil
	}
	if err != nil {
		return nil, "", err
	}
	r, ok := parseWindowCM(cm)
	if !ok || r.EndedAt != "" || !r.live(time.Now().UTC()) {
		return nil, "no live window", nil
	}
	name := strings.TrimPrefix(r.Reason, "PipelineRun/")
	if r.Reason != "PipelineRun/"+name || name == "" || r.Who != name {
		return nil, "session-owned window", nil
	}
	// Recovery cannot prove an ambiguous create ever succeeded. A missing run
	// leaves the TTL intact, just like follow(created=false); never renew it.
	run, err := res.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, "run missing; TTL remains", nil
		}
		return nil, "", err
	}
	// Two minutes without a renewal leaves three minutes of the five-minute
	// TTL for recovery. A healthy follower renews every twenty seconds.
	if !runEnded(run) {
		expires, err := time.Parse(time.RFC3339, r.ExpiresAt)
		if err != nil {
			return nil, "", err
		}
		if time.Until(expires) > windowLifetime-2*time.Minute {
			return nil, "run has a recent renewal", nil
		}
	}
	w := &runWindow{cs: cs, ns: ns, who: r.Who, version: cm.ResourceVersion, recorder: recorder}
	// Immediate observation also releases a terminal run at startup. Its CAS
	// fences competing recoveries/old pods: only one version can advance.
	ended, err := w.observe(ctx, res, name)
	if err != nil {
		return nil, "", err
	}
	if ended {
		return nil, "released completed run " + name, nil
	}
	return w, name, nil
}
