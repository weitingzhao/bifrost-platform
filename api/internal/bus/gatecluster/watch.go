package gatecluster

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// The watch rows (registry/watches/<tenant>.yaml, 2026-11-20): each row is a
// kind of cluster object whose state the gate says on the bus when it
// changes. The row names the resource, the fields that make the state and
// the topic; the topic's body is checked against the register. No kind of
// object has code of its own here: the Owner's rule is that a module that
// joins or leaves is a configuration change, not a code change.

// WatchSpec is a row and the ref its events carry (the topic's register ref).
type WatchSpec struct {
	registry.Watch
	Ref string
	// Window is how far back a full list looks in the ledger for objects it
	// no longer holds (the topic's retention, as the state readout reads it);
	// 0 is 30 days.
	Window time.Duration
}

var braceField = regexp.MustCompile(`\{([^}]*)\}`)

// fieldAt reads a dot path from an object; a segment may pick a list item by
// [key=value] or by its position, [0] (2026-11-21: a workload's version is
// its first container's image, spec.template.spec.containers[0].image). A
// bool or a number is read as its text; a missing field is "".
func fieldAt(obj map[string]any, path string) (string, bool) {
	var cur any = obj
	for _, seg := range strings.Split(path, ".") {
		name, sel := seg, ""
		if i := strings.Index(seg, "["); i >= 0 {
			name, sel = seg[:i], strings.TrimSuffix(seg[i+1:], "]")
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = m[name]
		if !ok {
			return "", false
		}
		if sel != "" {
			list, ok := cur.([]any)
			if !ok {
				return "", false
			}
			if n, err := strconv.Atoi(sel); err == nil {
				if n < 0 || n >= len(list) {
					return "", false
				}
				cur = list[n]
				continue
			}
			k, v, _ := strings.Cut(sel, "=")
			cur = nil
			for _, it := range list {
				if im, ok := it.(map[string]any); ok && fmt.Sprint(im[k]) == v {
					cur = im
					break
				}
			}
			if cur == nil {
				return "", false
			}
		}
	}
	switch v := cur.(type) {
	case string:
		return v, true
	case bool, int64, float64, int:
		return fmt.Sprint(v), true
	default:
		return "", false
	}
}

// None is a state field's value when the object leaves the field out or empty.
const None = "none"

// stateOf reads an object's subject and state values by the row.
func stateOf(w registry.Watch, obj map[string]any) (subject string, values []string) {
	subject = braceField.ReplaceAllStringFunc(w.Subject, func(m string) string {
		v, _ := fieldAt(obj, m[1:len(m)-1])
		return v
	})
	values = make([]string, len(w.State))
	for i, s := range w.State {
		raw, ok := fieldAt(obj, s.Path)
		if s.CompareTo != "" {
			// A count the API leaves out is 0 (status.readyReplicas of a
			// workload scaled to 0, or with no ready pod); the field compared
			// to must be there.
			if !ok {
				raw = "0"
			}
			other, ok2 := fieldAt(obj, s.CompareTo)
			raw, ok = fmt.Sprint(ok2 && raw == other), true
		}
		v := raw
		if v == "" && s.Map == nil {
			// A field the object leaves out or empty (a cluster with no primary
			// yet, no successful backup yet, a pod template with no image) is
			// said as none: the ledger refuses an empty string, and the state
			// would otherwise never be written (2026-11-21).
			v = None
		}
		if s.Map != nil {
			mapped, hit := s.Map[raw]
			if !ok || !hit {
				mapped = s.Map["*"]
			}
			v = mapped
		}
		values[i] = v
	}
	return subject, values
}

// watchBody is the event's body: the subject field, each state field, and
// subject, ref and change (the state joined by "-").
func watchBody(w WatchSpec, subject string, values []string) map[string]any {
	b := map[string]any{w.SubjectField: subject, "subject": subject, "ref": w.Ref, "change": strings.Join(values, "-")}
	for i, s := range w.State {
		b[s.Name] = values[i]
	}
	return b
}

func watchKey(w WatchSpec, subject string) string { return w.Stream + ":" + subject }

// validSubject is false for a subject a field of the object left empty.
func validSubject(subject string) bool {
	return subject != "" && !strings.Contains(subject, "//") && !strings.HasSuffix(subject, "/") && !strings.HasPrefix(subject, "/")
}

// allGone is true when every state field of a body says registry.Gone.
func allGone(w WatchSpec, body map[string]any) bool {
	if body == nil || len(w.State) == 0 {
		return false
	}
	for _, s := range w.State {
		if body[s.Name] != registry.Gone {
			return false
		}
	}
	return true
}

// sayGone writes that the object left the cluster: every state field
// registry.Gone. A subject the ledger never held, or holds as gone already,
// is not written.
func (g *Gate) sayGone(ctx context.Context, w WatchSpec, subject string) {
	values := make([]string, len(w.State))
	for i := range values {
		values[i] = registry.Gone
	}
	key := watchKey(w, subject)
	said := strings.Join(values, "\x00")
	g.watchMu.Lock()
	same := g.watched[key] == said
	g.watchMu.Unlock()
	if same {
		return
	}
	if g.append(ctx, key, w.Type, 0, func(_ string, last map[string]any) (map[string]any, bool) {
		if last == nil || allGone(w, last) {
			return nil, false
		}
		return watchBody(w, subject, values), true
	}) {
		g.watchMu.Lock()
		g.watched[key] = said
		g.watchMu.Unlock()
	}
}

// sweepGone follows a full list: each object of the row the ledger still
// holds as there, in the listed namespace, that the list did not return,
// left while no watch was running (a restart, a lost watch) and is said
// gone now.
func (g *Gate) sweepGone(ctx context.Context, w WatchSpec, namespace string, listed map[string]bool) {
	window := w.Window
	if window <= 0 {
		window = 30 * 24 * time.Hour
	}
	prefix := watchKey(w, "")
	rows, err := g.Ledger.Latest(ctx, w.Type, prefix, g.now().Add(-window))
	if err != nil {
		metricErrors.Add(1)
		g.logf("bus gate-cluster: watch %s: the ledger's objects not read: %v", w.ID, err)
		return
	}
	for _, r := range rows {
		subject := strings.TrimPrefix(r.Stream, prefix)
		if listed[subject] || allGone(w, r.Body) {
			continue
		}
		if namespace != "" && !strings.HasPrefix(subject, namespace+"/") {
			continue
		}
		g.sayGone(ctx, w, subject)
	}
}

func (g *Gate) observeWatched(ctx context.Context, w WatchSpec, obj *unstructured.Unstructured) {
	readerseen.Saw("watch-" + w.ID)
	subject, values := stateOf(w.Watch, obj.Object)
	if !validSubject(subject) {
		return
	}
	key := watchKey(w, subject)
	said := strings.Join(values, "\x00")
	g.watchMu.Lock()
	same := g.watched[key] == said
	g.watchMu.Unlock()
	if same {
		return
	}
	if g.append(ctx, key, w.Type, 0, func(_ string, last map[string]any) (map[string]any, bool) {
		if last != nil {
			unchanged := true
			for i, s := range w.State {
				if last[s.Name] != values[i] {
					unchanged = false
					break
				}
			}
			if unchanged {
				return nil, false
			}
		}
		return watchBody(w, subject, values), true
	}) {
		g.watchMu.Lock()
		g.watched[key] = said
		g.watchMu.Unlock()
	}
}

// runWatch lists the row's objects and watches them from there, in one
// namespace ("" is every namespace, or a cluster-wide resource); each state
// seen is compared with the ledger, and the list is taken again every
// ResyncEvery. An object's other fields change often (a node's heartbeat);
// only a change of the row's state is written.
func (g *Gate) runWatch(ctx context.Context, w WatchSpec, namespace string) {
	gvr := schema.GroupVersionResource{Group: w.Group, Version: w.Version, Resource: w.Resource}
	resync := g.ResyncEvery
	if resync <= 0 {
		resync = 30 * time.Minute
	}
	list := true
	var rv string
	var listed time.Time
	for ctx.Err() == nil {
		dyn, err := g.Dynamic()
		if err == nil && list {
			var page *unstructured.UnstructuredList
			if page, err = dyn.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{}); err == nil {
				present := make(map[string]bool, len(page.Items))
				for i := range page.Items {
					g.observeWatched(ctx, w, &page.Items[i])
					if subject, _ := stateOf(w.Watch, page.Items[i].Object); validSubject(subject) {
						present[subject] = true
					}
				}
				g.sweepGone(ctx, w, namespace, present)
				rv, list, listed = page.GetResourceVersion(), false, g.now()
			}
		}
		var wi watch.Interface
		if err == nil {
			wi, err = dyn.Resource(gvr).Namespace(namespace).Watch(ctx, metav1.ListOptions{
				ResourceVersion: rv, AllowWatchBookmarks: true, TimeoutSeconds: watchTimeout()})
		}
		if err != nil {
			metricErrors.Add(1)
			g.logf("bus gate-cluster: watch %s: %v", w.ID, err)
			list = true
			if !g.sleep(ctx) {
				return
			}
			continue
		}
		rv, err = consume(ctx, wi, rv, func(t watch.EventType, obj *unstructured.Unstructured) {
			if t != watch.Deleted {
				g.observeWatched(ctx, w, obj)
				return
			}
			if subject, _ := stateOf(w.Watch, obj.Object); validSubject(subject) {
				g.sayGone(ctx, w, subject)
			}
		})
		if ctx.Err() != nil {
			return
		}
		metricRestarts.Add(1)
		if err != nil {
			if !errors.Is(err, errGone) {
				g.logf("bus gate-cluster: watch %s ended: %v", w.ID, err)
			}
			list = true
		}
		if g.now().Sub(listed) >= resync {
			list = true
		}
	}
}
