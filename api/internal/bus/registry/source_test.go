package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A module that joins or leaves must not mean a code change (ISSUE-34).
// This adds a kind of cluster object to the bus with configuration
// only, in a directory that overrides the compiled register (as a mounted
// ConfigMap would): one topic with its body in rocket.yaml, one watch row.
func TestANewWatchedTopicIsConfigurationOnly(t *testing.T) {
	dir := t.TempDir()
	src, err := files.ReadFile("rocket.yaml")
	if err != nil {
		t.Fatal(err)
	}
	topic := `  cluster.sample.changed:
    address: ev.rocket.{env}.cluster.sample.changed
    x-bifrost:
      version: 1
      lane: event
      guarantee: best-effort
      subject: "<namespace>/<name>"
      publisher: gate-cluster
      domain: compute
      ref: "/api/v1/cluster/workloads"
      reconcile: "gate-cluster relists every 30 min"
      on_unread: stale
      retention: 30d
      consumers: [console]
      exports: []
      replaces: []
      body:
        - {name: workload, kind: str}
        - {name: ready, kind: str, pattern: '^(ready|notready)$'}
        - {name: subject, kind: str}
        - {name: ref, kind: str}
        - {name: change, kind: str}
      subject_from: workload
      change_from: [ready]
      console_keys: [[cluster, samples]]
`
	text := strings.Replace(string(src), "\nchannels:\n", "\nchannels:\n"+topic, 1)
	if err := os.WriteFile(filepath.Join(dir, "rocket.yaml"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "watches"), 0o755); err != nil {
		t.Fatal(err)
	}
	row := `tenant: rocket
watches:
  - id: samples
    group: apps
    version: v1
    resource: statefulsets
    namespaces: [data]
    type: cluster.sample.changed
    stream: workload
    subject: "{metadata.namespace}/{metadata.name}"
    subject_field: workload
    state:
      - name: ready
        path: status.readyReplicas
        map: {"0": notready, "*": ready}
`
	if err := os.WriteFile(filepath.Join(dir, "watches", "rocket.yaml"), []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RegistryDirEnv, dir)
	tenants, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c, ok := tenants["rocket"].Channels["cluster.sample.changed"]
	if !ok || len(c.Body) != 5 || c.ConsoleKeys[0][1] != "samples" {
		t.Fatalf("the new topic was not read from the override: %+v", c)
	}
	if _, ok := Vocabulary(c); !ok {
		t.Fatal("the new topic's body gives no ledger vocabulary")
	}
	rows, err := Watches()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows["rocket"]) != 1 || rows["rocket"][0].ID != "samples" {
		t.Fatalf("the watch rows were not read from the override: %+v", rows["rocket"])
	}
	// Subscriptions were not overridden: the compiled ones still answer.
	if subs, err := LoadSubscriptions(); err != nil || len(subs["rocket"].Rows) == 0 {
		t.Fatalf("the compiled subscriptions were lost: %v", err)
	}
	if Source() != dir {
		t.Fatalf("Source says %q", Source())
	}
}
