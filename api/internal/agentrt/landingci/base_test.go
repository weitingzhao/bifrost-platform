package landingci

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const baseM0 = "4444444444444444444444444444444444444444"

func withBase(c *Client) *Client {
	return &Client{Dynamic: c.Dynamic, BaseParam: true}
}

// Off, no run names a base: the landing, pinned and infra params are as
// before.
func TestLandingCIBaseOffParamsUnchanged(t *testing.T) {
	t.Parallel()
	c, _ := drillService(t)
	ctx := context.Background()
	name := landing.CIName("base-off", 1)
	if err := c.EnsureLandingCI(ctx, name, factSHA1, baseM0); err != nil {
		t.Fatal(err)
	}
	if params, _ := fullParams(t, c, name); !reflect.DeepEqual(params, map[string]string{"revision": factSHA1, "uiRevision": "main", "repo-url": PlatformRepoURL}) {
		t.Fatal(params)
	}
	pinned := landing.CIName("base-off-pinned", 1)
	if err := c.Ensure(ctx, pinned, pinnedKey()); err != nil {
		t.Fatal(err)
	}
	if params, _ := fullParams(t, c, pinned); !reflect.DeepEqual(params, map[string]string{"revision": factSHA1,
		"uiRevision": factSHA2, "infraRevision": factSHA2, "repo-url": PlatformRepoURL}) {
		t.Fatal(params)
	}
}

// On, a platform landing CI names its M0: EnsureLandingCI's base, a pinned
// key's Base. Infra's params and every label stay as they are.
func TestLandingCIBaseOnNamesM0(t *testing.T) {
	t.Parallel()
	off, _ := drillService(t)
	c := withBase(off)
	ctx := context.Background()
	name := landing.CIName("base-on", 1)
	if err := c.EnsureLandingCI(ctx, name, factSHA1, baseM0); err != nil {
		t.Fatal(err)
	}
	params, labels := fullParams(t, c, name)
	if !reflect.DeepEqual(params, map[string]string{"revision": factSHA1, "uiRevision": "main", "repo-url": PlatformRepoURL, BaseParamName: baseM0}) {
		t.Fatal(params)
	}
	if _, ok := labels["bifrost.io/base"]; ok {
		t.Fatal("a platform run labelled its base", labels)
	}
	// A base that is not a full SHA is not sent.
	short := landing.CIName("base-short", 1)
	if err := c.EnsureLandingCI(ctx, short, factSHA1, "main"); err != nil {
		t.Fatal(err)
	}
	if params, _ := fullParams(t, c, short); params[BaseParamName] != "" {
		t.Fatal(params)
	}
	key := pinnedKey()
	pinned := landing.CIName("base-on-pinned", 1)
	if err := c.Ensure(ctx, pinned, key); err != nil {
		t.Fatal(err)
	}
	if params, _ := fullParams(t, c, pinned); params[BaseParamName] != key.Base {
		t.Fatal("pinned key's base", params)
	}
	infra := infraKey()
	infraName := landing.InfraCIName(infra.Revision, infra.Base, infra.Siblings["bifrost-platform"], infra.Generation)
	if err := c.Ensure(ctx, infraName, infra); err != nil {
		t.Fatal(err)
	}
	if params, _ := fullParams(t, c, infraName); !reflect.DeepEqual(params, infraParams(infra)) {
		t.Fatal("infra params", params)
	}
}

// The daily full run never names a base, so it stays full.
func TestLandingCIBaseFullRunHasNone(t *testing.T) {
	t.Parallel()
	off, _ := drillService(t)
	day := time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)
	for i, c := range []*Client{off, withBase(off), {Dynamic: off.Dynamic, BaseParam: true, FullScopeParam: true}} {
		name, err := c.EnsureFullRun(context.Background(), factSHA1, day.AddDate(0, 0, i))
		if err != nil {
			t.Fatal(err)
		}
		if params, _ := fullParams(t, c, name); params[BaseParamName] != "" {
			t.Fatal("a full run named a base", i, params)
		}
	}
}

// A run made before the switch was on (no base) is resumed as this
// operation's run; one naming another base, or base twice, collides.
func TestLandingCIBaseResume(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"absent", "other", "duplicate"} {
		for _, pinned := range []bool{false, true} {
			label := change
			if pinned {
				label += "-pinned"
			}
			t.Run(label, func(t *testing.T) {
				t.Parallel()
				off, d := drillService(t)
				on := withBase(off)
				ctx, name := context.Background(), landing.CIName("base-resume", 1)
				ensure := func(c *Client) error {
					if pinned {
						return c.Ensure(ctx, name, pinnedKey())
					}
					return c.EnsureLandingCI(ctx, name, factSHA1, baseM0)
				}
				if err := ensure(on); err != nil {
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
					if m["name"] != BaseParamName {
						kept = append(kept, m)
						continue
					}
					switch change {
					case "other":
						kept = append(kept, map[string]any{"name": BaseParamName, "value": factSHA1})
					case "duplicate":
						kept = append(kept, m, m)
					}
				}
				setField(t, obj, kept, "spec", "params")
				if _, err = res.Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
				err = ensure(on)
				if change == "absent" {
					if err != nil {
						t.Fatal("a run made before the switch was not resumed", err)
					}
					return
				}
				if err == nil || err.Error() != "landing CI name collision" {
					t.Fatal("adopted a run of another base", err)
				}
			})
		}
	}
}
