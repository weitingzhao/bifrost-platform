package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/yaml"
)

// probeJobPlaceholders are the values CANARY.md tells the Owner to replace in
// probe-job.yaml before applying it; nothing else in the file varies.
var probeJobPlaceholders = []string{"WORKERS_IMAGE_DIGEST", "B5_PROBE_P", "B5_HELD_RUN", "B5_HELD_UID"}

// The one-off probe Job must run the probe, not a workers replica, under the
// deployed workers' identity, and stop by itself (W-31 B5 first open).
func TestProbeJobManifest(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("probe-job.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var job batchv1.Job
	if err = yaml.UnmarshalStrict(raw, &job); err != nil {
		t.Fatal(err)
	}
	pod := job.Spec.Template.Spec
	if job.Kind != "Job" || job.Namespace != "bifrost-platform-prod" || pod.ServiceAccountName != "bifrost-platform" ||
		pod.AutomountServiceAccountToken == nil || !*pod.AutomountServiceAccountToken || pod.RestartPolicy != "Never" ||
		job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 ||
		job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 300 ||
		pod.NodeSelector["kubernetes.io/arch"] != "amd64" || len(pod.Containers) != 1 || len(pod.InitContainers) != 0 || len(pod.Volumes) != 0 {
		t.Fatalf("probe Job shape: %+v", job)
	}
	// Never selected by the workers' Service, PDB or rollout checks.
	for _, labels := range []map[string]string{job.Labels, job.Spec.Template.Labels} {
		if labels["app.kubernetes.io/name"] == "platform-workers" || labels["app.kubernetes.io/component"] == "platform-workers" {
			t.Fatal("probe Job carries the workers' labels")
		}
	}
	c := pod.Containers[0]
	if c.Image != "WORKERS_IMAGE_DIGEST" || !slices.Equal(c.Command, []string{"/usr/local/bin/stg-release-probe"}) ||
		len(c.Env) != 0 || len(c.EnvFrom) != 0 || len(c.VolumeMounts) != 0 {
		t.Fatalf("probe container: %+v", c)
	}
	want := []string{"--revision=B5_PROBE_P", "--sample=B5_HELD_RUN", "--sample-uid=B5_HELD_UID", "--workers-namespace=bifrost-platform-prod"}
	if !slices.Equal(c.Args, want) {
		t.Fatalf("probe args %v, want %v", c.Args, want)
	}
	canary, err := os.ReadFile("CANARY.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probeJobPlaceholders {
		if !strings.Contains(string(canary), "`"+p+"`") {
			t.Errorf("CANARY.md does not say how to fill %s", p)
		}
	}
	if !strings.Contains(string(canary), "probe-job.yaml") {
		t.Error("CANARY.md does not name probe-job.yaml")
	}
}
