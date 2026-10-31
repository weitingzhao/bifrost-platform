package landingci

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// W-31 B5 first open: every existing platform CI run read Unknown(ci_identity)
// because none carried repo-url. The reader accepts exactly one entry with the
// platform repository; it never falls back to the Pipeline default.
func TestReleaseCIRepoURLIdentity(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Add(-time.Hour)
	withRepoURL := func(t *testing.T, o *unstructured.Unstructured, values ...string) {
		t.Helper()
		params := []any{map[string]any{"name": "revision", "value": factSHA1}}
		for _, v := range values {
			params = append(params, map[string]any{"name": "repo-url", "value": v})
		}
		setField(t, o, params, "spec", "params")
	}
	for _, tc := range []struct {
		name, status, reason string
		values               []string
	}{
		{"exact", "Succeeded", "", []string{PlatformRepoURL}},
		{"missing", "Unknown", "ci_identity", nil},
		{"duplicate same value", "Unknown", "ci_identity", []string{PlatformRepoURL, PlatformRepoURL}},
		{"duplicate other value", "Unknown", "ci_identity", []string{PlatformRepoURL, "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-ui.git"}},
		// One wrong value is another repository's run: not this P's evidence at all.
		{"wrong value", "Missing", "", []string{"http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-ui.git"}},
		{"empty value", "Missing", "", []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run := greenRun(t, "run", at)
			withRepoURL(t, run, tc.values...)
			c, _ := drillService(t, run)
			got, err := c.ReadRelease(context.Background(), factSHA1)
			if err != nil || got.Status != tc.status || got.Reason != tc.reason {
				t.Fatalf("got %s/%s %v, want %s/%s", got.Status, got.Reason, err, tc.status, tc.reason)
			}
			if _, err := got.Record(); (err == nil) != (tc.status == "Succeeded") {
				t.Fatal("record", err)
			}
		})
	}
}

// Ensure and EnsureLandingCI write repo-url, so the run they create is release
// evidence once it succeeds (the fake client sets no UID or creation time).
func TestEnsuredPlatformRunsCarryRepoURL(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"pinned", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			c, d := drillService(t)
			ctx, name := context.Background(), landing.CIName("repo-url", 1)
			ensure := func() error {
				if mode == "legacy" {
					return c.EnsureLandingCI(ctx, name, factSHA1)
				}
				return c.Ensure(ctx, name, pinnedKey())
			}
			if err := ensure(); err != nil {
				t.Fatal(err)
			}
			res := d.Resource(pipelineRunGVR).Namespace("cicd")
			obj, err := res.Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			values, counts := namedValues(*obj, "spec", "params")
			if counts["repo-url"] != 1 || values["repo-url"] != PlatformRepoURL {
				t.Fatal("spec", values, counts)
			}
			obj.SetUID("uid-ensured")
			obj.SetCreationTimestamp(metav1.NewTime(time.Now().UTC().Add(-time.Minute)))
			setField(t, obj, []any{map[string]any{"type": "Succeeded", "status": "True"}}, "status", "conditions")
			setField(t, obj, []any{
				map[string]any{"name": "platform-commit", "value": factSHA1},
				map[string]any{"name": "ui-commit", "value": factSHA2},
				map[string]any{"name": "infra-commit", "value": factSHA2},
			}, "status", "results")
			if _, err = res.Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			got, err := c.ReadRelease(ctx, factSHA1)
			if err != nil || got.Status != "Succeeded" || got.Run != name {
				t.Fatal("release", got, err)
			}
			if err = ensure(); err != nil {
				t.Fatal("idempotent", err)
			}
		})
	}
}

// A run created before this change (same name, no repo-url) is still the
// operation's run, and stays Unknown to B5. A wrong or duplicate repo-url is a
// collision, never adopted.
func TestEnsureRepoURLCollisionAndPreB5Run(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"absent", "wrong", "duplicate"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			c, d := drillService(t)
			ctx, name := context.Background(), landing.CIName("pre-b5", 1)
			if err := c.EnsureLandingCI(ctx, name, factSHA1); err != nil {
				t.Fatal(err)
			}
			res := d.Resource(pipelineRunGVR).Namespace("cicd")
			obj, err := res.Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			params, _, _ := unstructured.NestedSlice(obj.Object, "spec", "params")
			var kept []any
			for _, p := range params {
				m := p.(map[string]any)
				if m["name"] != "repo-url" {
					kept = append(kept, m)
					continue
				}
				switch change {
				case "wrong":
					kept = append(kept, map[string]any{"name": "repo-url", "value": "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-ui.git"})
				case "duplicate":
					kept = append(kept, m, m)
				}
			}
			setField(t, obj, kept, "spec", "params")
			if _, err = res.Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			err = c.EnsureLandingCI(ctx, name, factSHA1)
			if change == "absent" {
				if err != nil {
					t.Fatal("pre-B5 run not adopted", err)
				}
				obj, err = res.Get(ctx, name, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				obj.SetUID("uid-pre")
				obj.SetCreationTimestamp(metav1.NewTime(time.Now().UTC().Add(-time.Minute)))
				setField(t, obj, []any{map[string]any{"type": "Succeeded", "status": "True"}}, "status", "conditions")
				if _, err = res.Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
				got, readErr := c.ReadRelease(ctx, factSHA1)
				if readErr != nil || got.Status != "Unknown" || got.Reason != "ci_identity" {
					t.Fatal("pre-B5 run read as release evidence", got, readErr)
				}
				return
			}
			if err == nil || err.Error() != "landing CI name collision" {
				t.Fatal("adopted collision", err)
			}
		})
	}
}
