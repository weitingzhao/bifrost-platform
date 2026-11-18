package gatecluster

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
)

// The Argo CD Applications, said on the bus when their sync or health state
// changes (release.gitops.changed; channel X-A16, 2026-11-18: whoever wanted
// to know polled the Application every 2 seconds). The workers' gate watches
// them; the ledger's last event of an application is what "changed" compares
// with, so a restart neither repeats a state nor loses one.

// TypeGitopsChanged is the event; GitopsRef where a reader reads the state.
const (
	TypeGitopsChanged = "release.gitops.changed"
	GitopsRef         = "/api/v1/gitops/apps"
)

// ApplicationGVR is Argo CD's Application.
var ApplicationGVR = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}

var (
	appRE   = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)
	stateRE = regexp.MustCompile(`^[a-z]{1,30}$`)
)

// AppState is what the event says of an application.
type AppState struct{ App, Sync, Health, Revision string }

// AppStateOf reads an Application; ok is false for one without a usable state.
func AppStateOf(obj *unstructured.Unstructured) (AppState, bool) {
	s := AppState{App: obj.GetName()}
	sync, _, _ := unstructured.NestedString(obj.Object, "status", "sync", "status")
	health, _, _ := unstructured.NestedString(obj.Object, "status", "health", "status")
	s.Sync, s.Health = strings.ToLower(sync), strings.ToLower(health)
	s.Revision, _, _ = unstructured.NestedString(obj.Object, "status", "sync", "revision")
	if !sha40.MatchString(s.Revision) {
		s.Revision = ""
	}
	return s, appRE.MatchString(s.App) && stateRE.MatchString(s.Sync) && stateRE.MatchString(s.Health)
}

// GitopsStream is the ledger stream of one application.
func GitopsStream(app string) string { return "gitops:" + app }

func gitopsBody(s AppState) map[string]any {
	b := map[string]any{"app": s.App, "sync": s.Sync, "health": s.Health, "subject": s.App, "ref": GitopsRef, "change": s.Sync + "-" + s.Health}
	if s.Revision != "" {
		b["revision"] = s.Revision
	}
	return b
}

// gitopsChange decides the event: none when the last one says the same.
func gitopsChange(s AppState, last map[string]any) (map[string]any, bool) {
	rev, _ := last["revision"].(string)
	if last != nil && last["sync"] == s.Sync && last["health"] == s.Health && rev == s.Revision {
		return nil, false
	}
	return gitopsBody(s), true
}

func (g *Gate) observeApp(ctx context.Context, obj *unstructured.Unstructured) {
	readerseen.Saw("gitops-watch")
	s, ok := AppStateOf(obj)
	if !ok {
		return
	}
	g.appMu.Lock()
	same := g.apps[s.App] == s
	g.appMu.Unlock()
	if same {
		return
	}
	if g.append(ctx, GitopsStream(s.App), TypeGitopsChanged, 0, func(_ string, last map[string]any) (map[string]any, bool) {
		return gitopsChange(s, last)
	}) {
		g.appMu.Lock()
		g.apps[s.App] = s
		g.appMu.Unlock()
	}
}

// runApps lists the Applications and watches them from there; each state seen
// is compared with the ledger, and the list is taken again every ResyncEvery.
func (g *Gate) runApps(ctx context.Context) {
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
			if page, err = dyn.Resource(ApplicationGVR).Namespace(g.Namespace).List(ctx, metav1.ListOptions{}); err == nil {
				for i := range page.Items {
					g.observeApp(ctx, &page.Items[i])
				}
				rv, list, listed = page.GetResourceVersion(), false, g.now()
			}
		}
		var w watch.Interface
		if err == nil {
			w, err = dyn.Resource(ApplicationGVR).Namespace(g.Namespace).Watch(ctx, metav1.ListOptions{
				ResourceVersion: rv, AllowWatchBookmarks: true, TimeoutSeconds: watchTimeout()})
		}
		if err != nil {
			metricErrors.Add(1)
			g.logf("bus gate-cluster: applications: %v", err)
			list = true
			if !g.sleep(ctx) {
				return
			}
			continue
		}
		rv, err = consume(ctx, w, rv, func(t watch.EventType, obj *unstructured.Unstructured) {
			if t != watch.Deleted {
				g.observeApp(ctx, obj)
			}
		})
		if ctx.Err() != nil {
			return
		}
		metricRestarts.Add(1)
		if err != nil {
			if !errors.Is(err, errGone) {
				g.logf("bus gate-cluster: applications watch ended: %v", err)
			}
			list = true
		}
		if g.now().Sub(listed) >= resync {
			list = true
		}
	}
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{TypeGitopsChanged: {
			{Name: "app", Kind: eventlog.Str}, {Name: "sync", Kind: eventlog.Str}, {Name: "health", Kind: eventlog.Str},
			{Name: "revision", Kind: eventlog.ID, Optional: true},
			{Name: "subject", Kind: eventlog.Str}, {Name: "ref", Kind: eventlog.Str}, {Name: "change", Kind: eventlog.Str},
		}},
		Values: map[string]map[string]eventlog.Value{TypeGitopsChanged: {
			"app": eventlog.Shaped("an Application name", appRE), "sync": eventlog.Shaped("a sync status", stateRE),
			"health": eventlog.Shaped("a health status", stateRE), "revision": eventlog.Shaped("a 40-character commit", sha40),
		}},
		After: map[string]func(map[string]any) error{TypeGitopsChanged: func(b map[string]any) error {
			app, _ := b["app"].(string)
			if b["subject"] != app || b["ref"] != GitopsRef || b["change"] != fmt.Sprint(b["sync"], "-", b["health"]) {
				return eventlog.Invalid("%s: subject, ref and change are made of app, sync and health", TypeGitopsChanged)
			}
			return nil
		}},
	})
}
