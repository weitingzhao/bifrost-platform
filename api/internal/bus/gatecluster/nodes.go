package gatecluster

import (
	"context"
	"errors"
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

// The cluster's nodes, said on the bus when one turns ready or not ready, or
// is cordoned or uncordoned (cluster.node.changed, 2026-11-20). Origin: on
// 11-20 02:50Z ubt-k3s-05 was cordoned and drained for a disk swap; the bus's
// own NATS had its disk on that node, sat Pending for an hour, and nothing on
// the bus said a node had changed (the Owner: the Bus page showed nothing of
// the modules underneath). The workers' gate watches the nodes; the ledger's
// last event of a node is what "changed" compares with, so a restart neither
// repeats a state nor loses one.

// TypeNodeChanged is the event; NodesRef where a reader reads the nodes.
const (
	TypeNodeChanged = "cluster.node.changed"
	NodesRef        = "/api/v1/cluster/nodes"
)

// NodeGVR is the core Node.
var NodeGVR = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}

var (
	nodeRE     = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)
	readyRE    = regexp.MustCompile(`^(ready|notready|unknown)$`)
	scheduleRE = regexp.MustCompile(`^(schedulable|cordoned)$`)
)

// NodeState is what the event says of a node.
type NodeState struct{ Node, Ready, Schedule string }

// NodeStateOf reads a Node: its Ready condition and whether it is cordoned.
func NodeStateOf(obj *unstructured.Unstructured) (NodeState, bool) {
	s := NodeState{Node: obj.GetName(), Ready: "unknown", Schedule: "schedulable"}
	if cordoned, _, _ := unstructured.NestedBool(obj.Object, "spec", "unschedulable"); cordoned {
		s.Schedule = "cordoned"
	}
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conds {
		m, _ := c.(map[string]any)
		if m == nil || m["type"] != "Ready" {
			continue
		}
		switch strings.ToLower(asString(m["status"])) {
		case "true":
			s.Ready = "ready"
		case "false":
			s.Ready = "notready"
		}
	}
	return s, nodeRE.MatchString(s.Node)
}

func asString(v any) string { s, _ := v.(string); return s }

// NodeStream is the ledger stream of one node.
func NodeStream(node string) string { return "node:" + node }

func nodeBody(s NodeState) map[string]any {
	return map[string]any{"node": s.Node, "ready": s.Ready, "schedule": s.Schedule, "subject": s.Node, "ref": NodesRef, "change": s.Ready + "-" + s.Schedule}
}

// nodeChange decides the event: none when the last one says the same.
func nodeChange(s NodeState, last map[string]any) (map[string]any, bool) {
	if last != nil && last["ready"] == s.Ready && last["schedule"] == s.Schedule {
		return nil, false
	}
	return nodeBody(s), true
}

func (g *Gate) observeNode(ctx context.Context, obj *unstructured.Unstructured) {
	readerseen.Saw("node-watch")
	s, ok := NodeStateOf(obj)
	if !ok {
		return
	}
	g.nodeMu.Lock()
	same := g.nodes[s.Node] == s
	g.nodeMu.Unlock()
	if same {
		return
	}
	if g.append(ctx, NodeStream(s.Node), TypeNodeChanged, 0, func(_ string, last map[string]any) (map[string]any, bool) {
		return nodeChange(s, last)
	}) {
		g.nodeMu.Lock()
		g.nodes[s.Node] = s
		g.nodeMu.Unlock()
	}
}

// runNodes lists the nodes and watches them from there; each state seen is
// compared with the ledger, and the list is taken again every ResyncEvery.
// A node's heartbeat changes its object every few seconds; only a change of
// Ready or of cordon is written.
func (g *Gate) runNodes(ctx context.Context) {
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
			if page, err = dyn.Resource(NodeGVR).List(ctx, metav1.ListOptions{}); err == nil {
				for i := range page.Items {
					g.observeNode(ctx, &page.Items[i])
				}
				rv, list, listed = page.GetResourceVersion(), false, g.now()
			}
		}
		var w watch.Interface
		if err == nil {
			w, err = dyn.Resource(NodeGVR).Watch(ctx, metav1.ListOptions{
				ResourceVersion: rv, AllowWatchBookmarks: true, TimeoutSeconds: watchTimeout()})
		}
		if err != nil {
			metricErrors.Add(1)
			g.logf("bus gate-cluster: nodes: %v", err)
			list = true
			if !g.sleep(ctx) {
				return
			}
			continue
		}
		rv, err = consume(ctx, w, rv, func(t watch.EventType, obj *unstructured.Unstructured) {
			if t != watch.Deleted {
				g.observeNode(ctx, obj)
			}
		})
		if ctx.Err() != nil {
			return
		}
		metricRestarts.Add(1)
		if err != nil {
			if !errors.Is(err, errGone) {
				g.logf("bus gate-cluster: nodes watch ended: %v", err)
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
		Bodies: map[string][]eventlog.Field{TypeNodeChanged: {
			{Name: "node", Kind: eventlog.Str}, {Name: "ready", Kind: eventlog.Str}, {Name: "schedule", Kind: eventlog.Str},
			{Name: "subject", Kind: eventlog.Str}, {Name: "ref", Kind: eventlog.Str}, {Name: "change", Kind: eventlog.Str},
		}},
		Values: map[string]map[string]eventlog.Value{TypeNodeChanged: {
			"node": eventlog.Shaped("a node name", nodeRE), "ready": eventlog.Shaped("ready, notready or unknown", readyRE),
			"schedule": eventlog.Shaped("schedulable or cordoned", scheduleRE),
		}},
		After: map[string]func(map[string]any) error{TypeNodeChanged: func(b map[string]any) error {
			node, _ := b["node"].(string)
			if b["subject"] != node || b["ref"] != NodesRef || b["change"] != asString(b["ready"])+"-"+asString(b["schedule"]) {
				return eventlog.Invalid("%s: subject, ref and change are made of node, ready and schedule", TypeNodeChanged)
			}
			return nil
		}},
	})
}
