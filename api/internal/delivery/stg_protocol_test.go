package delivery

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"k8s.io/client-go/kubernetes"
	kfake "k8s.io/client-go/kubernetes/fake"
)

// Contract with the already integrated B5-a, without a database or any store
// mutation. The adapters' nested evidence must pass its closed parser.
func TestSTGEvidenceMatchesB5AProtocol(t *testing.T) {
	t.Parallel()
	in := stgInput()
	id, err := in.Identity()
	if err != nil {
		t.Fatal(err)
	}
	parse := func(kind string, fields map[string]any) {
		t.Helper()
		fields["v"] = "1"
		fields["kind"] = kind
		fields["scope"] = "batch"
		fields["batch"] = "stg:bifrost-platform:1"
		if _, err := store.ParseStgRelease(fields); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	ci := landingci.ReleaseEvidence{Evidence: landingci.Evidence{CIRun: landing.CIRun{Run: "ci-p", UID: "ci-uid", R: in.P, Status: "Succeeded", CreatedAt: stgTestNow.Add(-time.Hour), PlatformCommit: in.P, UICommit: in.U}, InfraCommit: in.Infra}, PipelineRef: "bifrost-ci-platform", RepoURL: landingci.PlatformRepoURL, ReadAt: stgTestNow}
	accepted, err := ci.Record()
	if err != nil {
		t.Fatal(err)
	}
	parse("ci_accepted", map[string]any{"P": in.P, "ci": accepted})
	ci.Status = "Failed"
	if _, err := ci.Record(); err == nil {
		t.Fatal("failed CI made accepted record")
	}
	parse("started", map[string]any{"try": in.Try, "intent_seq": in.IntentSeq, "run": id.Name, "labels": stringObject(id.Labels), "annotations": stringObject(id.Annotations), "fingerprint": id.Fingerprint, "spend": []any{}, "lease": map[string]any{"who": id.Who, "gen": int64(1)}, "deadline": stgTestNow.Add(time.Minute).Format(time.RFC3339Nano)})
	run, children := stgRunWorld(t)
	obs, err := readSTGWorld(t, run, children)
	if err != nil {
		t.Fatal(err)
	}
	parse("run_evidence", map[string]any{"try": in.Try, "uid": string(run.GetUID()), "status": obs.Status, "raw": obs.Raw, "at": stgTestNow.Format(time.RFC3339Nano)})
	// The parent definition survives finalizer release alongside every child.
	archived := obs.Raw["child_refs"].([]any)[0].(map[string]any)["pipeline_run"].(map[string]any)
	if archived["status"].(map[string]any)["pipelineSpec"] == nil {
		t.Fatal("executed definition lost")
	}
	core := kfake.NewSimpleClientset(stgWorkloadWorld()...)
	a := &STGReleaseAdapter{Core: func() (kubernetes.Interface, error) { return core, nil }, HTTP: &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"status":"ok","service":"bifrost-platform-api"}`)), Request: r}, nil
	})}}
	health := a.ReadHealth(context.Background())
	if health.Outcome != "passed" {
		t.Fatal(health)
	}
	workloads, err := a.ReadWorkloads(context.Background(), stgAPI, stgConsole)
	if err != nil || len(workloads) != 3 {
		t.Fatal("read workloads", workloads, err)
	}
	proof := map[string]any{"v": "1", "kind": "evidence", "scope": "ext", "evidence": "test/cicd/run-uid", "source": "external", "run": id.Name, "P": in.P, "U": in.U, "infra": in.Infra, "content": map[string]any{"api_digest": stgAPI, "console_digest": stgConsole}, "deployed": map[string]any{"pin_sync_ok": true, "workloads": workloads, "at": stgTestNow.Format(time.RFC3339Nano)}, "health": health.Evidence}
	if _, err := store.ParseStgRelease(proof); err != nil {
		t.Fatal(err)
	}
}

// Review 702 B-01: use the adapter's exact return value at every workloads
// protocol position. A hand-built valid record is only the parser control.
func TestSTGWorkloadOutputPassesEveryProtocolPosition(t *testing.T) {
	t.Parallel()
	core := kfake.NewSimpleClientset(stgWorkloadWorld()...)
	a := &STGReleaseAdapter{Core: func() (kubernetes.Interface, error) { return core, nil }}
	workloads, err := a.ReadWorkloads(context.Background(), stgAPI, stgConsole)
	if err != nil || len(workloads) != 3 {
		t.Fatal("read control", workloads, err)
	}
	expected := map[string]any{}
	for _, name := range []string{"platform-api", "platform-workers", "platform-console"} {
		digest := stgAPI
		if name == "platform-console" {
			digest = stgConsole
		}
		expected[name] = map[string]any{"uid": name + "-uid", "generation": int64(2), "digest": digest}
	}
	for _, kind := range []string{"evidence", "deployment_checked", "effect"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			at := stgTestNow.Format(time.RFC3339Nano)
			fields := map[string]any{"v": "1", "kind": kind, "scope": "ext", "evidence": "test/cicd/run-uid",
				"content":  map[string]any{"api_digest": stgAPI, "console_digest": stgConsole},
				"deployed": map[string]any{"pin_sync_ok": true, "workloads": expected, "at": at},
				"health":   map[string]any{"status": "ok", "service": "bifrost-platform-api"}}
			if kind == "evidence" {
				fields["source"], fields["run"] = "external", "stgrel-1-1-aaaaaaaa"
				fields["P"], fields["U"], fields["infra"] = stgInput().P, stgInput().U, stgInput().Infra
			}
			if kind == "effect" {
				fields = map[string]any{"v": "1", "kind": kind, "scope": "batch", "batch": "stg:bifrost-platform:1",
					"try": int64(1), "class": "this_try", "workloads": expected, "at": at}
			}
			if _, err := store.ParseStgRelease(fields); err != nil {
				t.Fatal("parser control", err)
			}
			if kind == "effect" {
				fields["workloads"] = workloads
			} else {
				fields["deployed"].(map[string]any)["workloads"] = workloads
			}
			record, err := store.ParseStgRelease(fields)
			if err != nil {
				t.Fatal("actual ReadWorkloads output rejected", err)
			}
			stored := record["workloads"]
			if kind != "effect" {
				stored = record["deployed"].(map[string]any)["workloads"]
			}
			got, err := canon.Encode(stored)
			want, wantErr := canon.Encode(expected)
			if err != nil || wantErr != nil || string(got) != string(want) {
				t.Fatal("workload evidence changed", string(got), err, wantErr)
			}
		})
	}
}
