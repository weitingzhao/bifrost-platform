package gatecluster

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

func nodeObj(name, ready string, cordoned bool, heartbeat string) *unstructured.Unstructured {
	spec := map[string]any{}
	if cordoned {
		spec["unschedulable"] = true
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": name},
		"spec":     spec,
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "MemoryPressure", "status": "False"},
			map[string]any{"type": "Ready", "status": ready, "lastHeartbeatTime": heartbeat},
		}},
	}}
}

func nodesRow(t *testing.T) WatchSpec {
	t.Helper()
	rows, err := registry.Watches()
	if err != nil {
		t.Fatal(err)
	}
	tenants, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range rows["rocket"] {
		if w.ID == "nodes" {
			return WatchSpec{Watch: w, Ref: tenants["rocket"].Channels[w.Type].Ref}
		}
	}
	t.Fatal("no nodes row in watches/rocket.yaml")
	return WatchSpec{}
}

// Origin: 2026-11-20 02:50Z, ubt-k3s-05 was cordoned and drained for a disk
// swap; NATS, whose disk was on it, sat Pending for an hour and the bus said
// nothing of the node. The node watch is now a row of configuration
// (ISSUE-34); each change
// of Ready or cordon is said once, the heartbeat says nothing.
func TestAWatchedObjectIsSaidOncePerChange(t *testing.T) {
	w := nodesRow(t)
	l := &memLedger{}
	g := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g.init()
	ctx := context.Background()
	g.observeWatched(ctx, w, nodeObj("ubt-k3s-05", "True", false, "t1"))
	g.observeWatched(ctx, w, nodeObj("ubt-k3s-05", "True", false, "t2")) // heartbeat only
	g.observeWatched(ctx, w, nodeObj("ubt-k3s-05", "True", true, "t3"))  // cordoned
	g.observeWatched(ctx, w, nodeObj("ubt-k3s-05", "Unknown", true, "t4"))
	got := l.events("node:ubt-k3s-05")
	if len(got) != 3 {
		t.Fatalf("%d events: %+v", len(got), got)
	}
	g2 := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g2.init()
	g2.observeWatched(ctx, w, nodeObj("ubt-k3s-05", "Unknown", true, "t5"))
	if n := len(l.events("node:ubt-k3s-05")); n != 3 {
		t.Fatalf("a restart repeated the state: %d events", n)
	}
}

func TestAWatchReadsStateByItsRowAndTheRegisterChecksTheBody(t *testing.T) {
	w := nodesRow(t)
	cases := []struct {
		obj        *unstructured.Unstructured
		ready, sch string
	}{
		{nodeObj("ubt-k3s-01", "True", false, ""), "ready", "schedulable"},
		{nodeObj("gpu-server", "Unknown", true, ""), "unknown", "cordoned"},
		{nodeObj("ubt-k3s-02", "False", false, ""), "notready", "schedulable"},
		// No Ready condition at all (a node just joining): the map's default.
		{&unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "ubt-k3s-08"}}}, "unknown", "schedulable"},
	}
	for _, c := range cases {
		subject, values := stateOf(w.Watch, c.obj.Object)
		if subject != c.obj.GetName() || values[0] != c.ready || values[1] != c.sch {
			t.Fatalf("%s: got %s %v, want %s/%s", c.obj.GetName(), subject, values, c.ready, c.sch)
		}
		if err := eventlog.CheckBody(w.Type, watchBody(w, subject, values)); err != nil {
			t.Fatal(err)
		}
	}
	// Positive control: a body whose change does not follow from its fields is refused by the register's rules.
	bad := watchBody(w, "ubt-k3s-01", []string{"ready", "schedulable"})
	bad["change"] = "notready-cordoned"
	if err := eventlog.CheckBody(w.Type, bad); err == nil {
		t.Fatal("a body with a made-up change passed")
	}
	bad = watchBody(w, "ubt-k3s-01", []string{"maybe", "schedulable"})
	if err := eventlog.CheckBody(w.Type, bad); err == nil {
		t.Fatal("a state value outside its pattern passed")
	}
}

func TestFieldPathsReadListsByKeyAndScalars(t *testing.T) {
	obj := map[string]any{
		"metadata": map[string]any{"name": "nats", "namespace": "data"},
		"status":   map[string]any{"readyReplicas": int64(1), "conditions": []any{map[string]any{"type": "Available", "status": "True"}}},
		"spec":     map[string]any{"paused": false},
	}
	for path, want := range map[string]string{
		"metadata.name": "nats", "status.readyReplicas": "1", "spec.paused": "false",
		"status.conditions[type=Available].status": "True",
	} {
		if got, ok := fieldAt(obj, path); !ok || got != want {
			t.Errorf("%s: got %q %v, want %q", path, got, ok, want)
		}
	}
	for _, path := range []string{"status.missing", "status.conditions[type=Gone].status", "metadata.name.deeper"} {
		if got, ok := fieldAt(obj, path); ok {
			t.Errorf("%s: got %q, want missing", path, got)
		}
	}
}

func workloadRow(t *testing.T, id string) WatchSpec {
	t.Helper()
	rows, err := registry.Watches()
	if err != nil {
		t.Fatal(err)
	}
	tenants, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range rows["rocket"] {
		if w.ID == id {
			return WatchSpec{Watch: w, Ref: tenants["rocket"].Channels[w.Type].Ref}
		}
	}
	t.Fatalf("no %s row", id)
	return WatchSpec{}
}

// 2026-11-20: NATS (a StatefulSet) sat Pending an hour and nothing on the bus
// said so. A workload is ready when its ready replicas equal its replicas;
// one scaled to 0 is ready with none (the API leaves readyReplicas out).
func TestAWorkloadIsReadyWhenItsReplicasAreReady(t *testing.T) {
	w := workloadRow(t, "statefulsets")
	obj := func(replicas int64, ready any) *unstructured.Unstructured {
		status := map[string]any{}
		if ready != nil {
			status["readyReplicas"] = ready
		}
		return &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": "nats", "namespace": "data"},
			"spec":     map[string]any{"replicas": replicas},
			"status":   status,
		}}
	}
	for _, c := range []struct {
		replicas int64
		ready    any
		want     string
	}{
		{1, int64(1), "ready"},
		{1, nil, "notready"}, // Pending: no ready pod, the field left out
		{3, int64(2), "notready"},
		{0, nil, "ready"}, // scaled to 0 (STG daemon)
	} {
		subject, values := stateOf(w.Watch, obj(c.replicas, c.ready).Object)
		if subject != "data/nats" || values[0] != c.want {
			t.Errorf("replicas %d ready %v: got %s %v, want %s", c.replicas, c.ready, subject, values, c.want)
		}
		if err := eventlog.CheckBody(w.Type, watchBody(w, subject, values)); err != nil {
			t.Fatal(err)
		}
	}
	// Positive control: one Pending after ready writes two events, a repeat none.
	l := &memLedger{}
	g := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g.init()
	ctx := context.Background()
	g.observeWatched(ctx, w, obj(1, int64(1)))
	g.observeWatched(ctx, w, obj(1, nil))
	g.observeWatched(ctx, w, obj(1, nil))
	if n := len(l.events("sts:data/nats")); n != 2 {
		t.Fatalf("%d events, want 2", n)
	}
}

func watchRow(t *testing.T, id string) WatchSpec {
	t.Helper()
	rows, err := registry.Watches()
	if err != nil {
		t.Fatal(err)
	}
	tenants, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range rows["rocket"] {
		if w.ID == id {
			return WatchSpec{Watch: w, Ref: tenants["rocket"].Channels[w.Type].Ref}
		}
	}
	t.Fatalf("no %s row in watches/rocket.yaml", id)
	return WatchSpec{}
}

func stsObj(namespace, name string, ready, replicas int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"namespace": namespace, "name": name},
		"spec":     map[string]any{"replicas": replicas},
		"status":   map[string]any{"readyReplicas": ready},
	}}
}

// Origin: 2026-11-21, the metrics line counted GET /api/v1/bus/states for
// cluster.workload.changed against the cluster: 392 workloads for 82 real
// Deployments and StatefulSets. The watch dropped deletions, so each Tekton
// affinity-assistant StatefulSet (one per run, deleted when it ends) stayed
// in the readout. A deleted object is now said gone, once; one the ledger
// never held is not written at all.
func TestADeletedObjectIsSaidGoneOnce(t *testing.T) {
	w := watchRow(t, "statefulsets")
	l := &memLedger{}
	g := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g.init()
	ctx := context.Background()
	g.observeWatched(ctx, w, stsObj("cicd", "affinity-assistant-1", 1, 1))
	g.sayGone(ctx, w, "cicd/affinity-assistant-1")
	g.sayGone(ctx, w, "cicd/affinity-assistant-1")
	evs := l.events("sts:cicd/affinity-assistant-1")
	if len(evs) != 2 || !allGone(w, evs[1].body) {
		t.Fatalf("events %+v, want ready then gone", evs)
	}
	g.sayGone(ctx, w, "cicd/never-seen")
	if n := len(l.events("sts:cicd/never-seen")); n != 0 {
		t.Fatalf("an object the ledger never held was written gone (%d events)", n)
	}
	if len(l.errs) != 0 {
		t.Fatalf("bodies refused by the register: %v", l.errs)
	}
}

// Same origin: deletions made while no watch ran (the 310 affinity
// assistants already in the ledger) are taken back by the next full list,
// in the listed namespace only and for the row's own stream only.
func TestAFullListTakesBackWhatItNoLongerHolds(t *testing.T) {
	sts, deploy := watchRow(t, "statefulsets"), watchRow(t, "deployments")
	l := &memLedger{}
	g := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g.init()
	ctx := context.Background()
	g.observeWatched(ctx, sts, stsObj("cicd", "affinity-assistant-1", 1, 1))
	g.observeWatched(ctx, sts, stsObj("cicd", "nats", 1, 1))
	g.observeWatched(ctx, sts, stsObj("data", "pg", 1, 1))
	g.observeWatched(ctx, deploy, stsObj("cicd", "affinity-assistant-1", 1, 1))
	g2 := &Gate{Ledger: l, Logf: func(string, ...any) {}} // a restart: nothing in memory
	g2.init()
	g2.sweepGone(ctx, sts, "cicd", map[string]bool{"cicd/nats": true})
	last := func(stream string) any {
		evs := l.events(stream)
		return evs[len(evs)-1].body["ready"]
	}
	if got := last("sts:cicd/affinity-assistant-1"); got != registry.Gone {
		t.Errorf("unlisted sts ready=%v, want gone", got)
	}
	if got := last("sts:cicd/nats"); got != "ready" {
		t.Errorf("listed sts ready=%v, want ready", got)
	}
	if got := last("sts:data/pg"); got != "ready" {
		t.Errorf("sts outside the listed namespace ready=%v, want ready", got)
	}
	if got := last("deploy:cicd/affinity-assistant-1"); got != "ready" {
		t.Errorf("another row's stream ready=%v, want ready", got)
	}
	n := len(l.events("sts:cicd/affinity-assistant-1"))
	g2.sweepGone(ctx, sts, "cicd", map[string]bool{"cicd/nats": true})
	if len(l.events("sts:cicd/affinity-assistant-1")) != n {
		t.Error("a second sweep wrote gone again")
	}
}

// Origin: 2026-11-21, the metrics line found the cluster's 5 DaemonSets never
// on the bus. A DaemonSet is ready when its ready pods equal the nodes it
// should run on, and not ready while one is missing.
func TestADaemonSetIsReadyWhenEveryNodeRunsIt(t *testing.T) {
	w := watchRow(t, "daemonsets")
	ds := func(ready, desired int64) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"namespace": "monitoring", "name": "node-exporter"},
			"status":   map[string]any{"numberReady": ready, "desiredNumberScheduled": desired},
		}}
	}
	l := &memLedger{}
	g := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g.init()
	ctx := context.Background()
	g.observeWatched(ctx, w, ds(7, 7))
	g.observeWatched(ctx, w, ds(6, 7))
	evs := l.events("ds:monitoring/node-exporter")
	if len(evs) != 2 || evs[0].body["ready"] != "ready" || evs[1].body["ready"] != "notready" || evs[1].body["workload"] != "monitoring/node-exporter" {
		t.Fatalf("events %+v, want ready then notready", evs)
	}
	if len(l.errs) != 0 {
		t.Fatalf("bodies refused by the register: %v", l.errs)
	}
}

// Origin: 2026-11-21, the communication line's list of what the system cannot
// see: a backup's success or failure was only pulled (get_postgres_backup_status),
// never said. The CNPG cluster's own status says it: each successful backup
// moves last_backup, a failed one flips backup to failed.
func TestABackupIsSaidFromTheClustersStatus(t *testing.T) {
	w := watchRow(t, "postgres")
	pg := func(lastOK, backup string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"namespace": "data", "name": "bifrost-postgres"},
			"spec":     map[string]any{"instances": int64(2)},
			"status": map[string]any{"readyInstances": int64(2), "currentPrimary": "bifrost-postgres-1", "lastSuccessfulBackup": lastOK,
				"conditions": []any{
					map[string]any{"type": "ContinuousArchiving", "status": "True"},
					map[string]any{"type": "LastBackupSucceeded", "status": backup},
				}},
		}}
	}
	l := &memLedger{}
	g := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g.init()
	ctx := context.Background()
	g.observeWatched(ctx, w, pg("2026-11-21T04:42:40Z", "True"))
	g.observeWatched(ctx, w, pg("2026-11-21T04:42:40Z", "True"))  // nothing new
	g.observeWatched(ctx, w, pg("2026-11-22T04:40:00Z", "True"))  // the next night's backup
	g.observeWatched(ctx, w, pg("2026-11-22T04:40:00Z", "False")) // the one after failed
	evs := l.events("pg:data/bifrost-postgres")
	if len(evs) != 3 {
		t.Fatalf("%d events, want 3: %+v", len(evs), evs)
	}
	if evs[0].body["backup"] != "ok" || evs[0].body["archiving"] != "ok" || evs[1].body["last_backup"] != "2026-11-22T04:40:00Z" || evs[2].body["backup"] != "failed" {
		t.Fatalf("events %+v", evs)
	}
	if len(l.errs) != 0 {
		t.Fatalf("bodies refused by the register: %v", l.errs)
	}
}

// Origin: 2026-11-21, the communication line's list of what the system cannot
// see: a workload that changed version said nothing (only ready and notready).
// The version is the first container's image: a rollout is one change; a
// field the object leaves out is said as none, never empty (the ledger refuses
// an empty string, which would lose the whole state).
func TestARolloutIsAVersionChange(t *testing.T) {
	w := watchRow(t, "deployments")
	dep := func(image string) *unstructured.Unstructured {
		containers := []any{map[string]any{"name": "api", "image": image}, map[string]any{"name": "sidecar", "image": "envoy:1"}}
		if image == "" {
			containers = []any{}
		}
		return &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"namespace": "bifrost-platform-prod", "name": "platform-api"},
			"spec":     map[string]any{"replicas": int64(1), "template": map[string]any{"spec": map[string]any{"containers": containers}}},
			"status":   map[string]any{"readyReplicas": int64(1)},
		}}
	}
	l := &memLedger{}
	g := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g.init()
	ctx := context.Background()
	g.observeWatched(ctx, w, dep("registry/platform-api@sha256:aaa"))
	g.observeWatched(ctx, w, dep("registry/platform-api@sha256:aaa"))
	g.observeWatched(ctx, w, dep("registry/platform-api@sha256:bbb"))
	g.observeWatched(ctx, w, dep(""))
	evs := l.events("deploy:bifrost-platform-prod/platform-api")
	if len(evs) != 3 || evs[0].body["version"] != "registry/platform-api@sha256:aaa" || evs[1].body["version"] != "registry/platform-api@sha256:bbb" || evs[2].body["version"] != None {
		t.Fatalf("events %+v, want aaa, bbb, none", evs)
	}
	if len(l.errs) != 0 {
		t.Fatalf("bodies refused by the register: %v", l.errs)
	}
}
