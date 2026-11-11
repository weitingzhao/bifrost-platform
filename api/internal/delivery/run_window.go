package delivery

import (
	"context"
	"encoding/json"
	"time"

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
		ticker := time.NewTicker(windowLifetime / 5)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			// A lost create reply has not proved that a run ever existed.
			// Do not turn a 404 into an early release of an uncertain POST.
			if !created {
				_, err := res.Get(ctx, name, metav1.GetOptions{})
				if err != nil {
					cancel()
					return
				} // leave the bounded TTL intact
				created = true
			}
			ended, err := w.observe(ctx, res, name)
			cancel()
			if err != nil || ended {
				return
			}
		}
	})
}
