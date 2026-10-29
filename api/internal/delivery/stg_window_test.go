package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
)

// The stock tracker does not enforce CAS. This fake does, including server
// versions on create and preserving the actual object on rejected updates.
func stgWindowCAS(t *testing.T) (*STGReleaseAdapter, *fake.Clientset) {
	t.Helper()
	cs := fake.NewSimpleClientset()
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	cs.PrependReactor("create", "configmaps", func(a kt.Action) (bool, runtime.Object, error) {
		cm := a.(kt.CreateAction).GetObject().(*corev1.ConfigMap).DeepCopy()
		cm.ResourceVersion, cm.UID = "1", "window-uid"
		err := cs.Tracker().Create(gvr, cm, a.GetNamespace())
		return true, cm, err
	})
	cs.PrependReactor("update", "configmaps", func(a kt.Action) (bool, runtime.Object, error) {
		cm := a.(kt.UpdateAction).GetObject().(*corev1.ConfigMap).DeepCopy()
		obj, err := cs.Tracker().Get(gvr, a.GetNamespace(), cm.Name)
		if err != nil {
			return true, nil, err
		}
		old := obj.(*corev1.ConfigMap)
		if cm.ResourceVersion == "" || cm.ResourceVersion != old.ResourceVersion {
			return true, nil, apierrors.NewConflict(gvr.GroupResource(), cm.Name, errors.New("stale version"))
		}
		n, _ := strconv.Atoi(cm.ResourceVersion)
		cm.ResourceVersion = strconv.Itoa(n + 1)
		err = cs.Tracker().Update(gvr, cm, a.GetNamespace())
		return true, cm, err
	})
	return &STGReleaseAdapter{Core: func() (kubernetes.Interface, error) { return cs, nil }, Now: func() time.Time { return stgTestNow }}, cs
}

func TestSTGWindowCASFencesOldGenerationAndRelease(t *testing.T) {
	t.Parallel()
	a, cs := stgWindowCAS(t)
	ctx := context.Background()
	who := "stgrel-bifrost-platform-1-1"
	first, err := a.TakeSTGWindow(ctx, STGWindow{}, who, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	// A second fresh writer lost the create race; a non-recovery holder
	// cannot adopt the same live who either.
	for _, old := range []STGWindow{{}, first} {
		if _, err := a.TakeSTGWindow(ctx, old, who, 2, false); err == nil {
			t.Fatal("competing acquire succeeded")
		}
	}
	a.Now = func() time.Time { return stgTestNow.Add(time.Minute) }
	renewed, err := a.RenewSTGWindow(ctx, first)
	if err != nil || !renewed.Expires.Equal(stgTestNow.Add(6*time.Minute)) || renewed.Generation != 1 {
		t.Fatal("renew success control", renewed, err)
	}
	next, err := a.TakeSTGWindow(ctx, renewed, who, 2, true)
	if err != nil || next.Generation != 2 {
		t.Fatal("restart adoption", next, err)
	}
	// Late old requests must each make one update, never GET/retry/adopt.
	cs.ClearActions()
	if _, err := a.RenewSTGWindow(ctx, renewed); err == nil {
		t.Fatal("old renewer revived")
	}
	if err := a.EndSTGWindow(ctx, renewed); err == nil {
		t.Fatal("old release ended the new lease")
	}
	if actions := cs.Actions(); len(actions) != 2 || actions[0].GetVerb() != "update" || actions[1].GetVerb() != "update" {
		t.Fatal("stale writes re-read/retried", actions)
	}
	got, err := a.ReadSTGWindow(ctx)
	if err != nil || got.ResourceVersion != next.ResourceVersion || got.Generation != 2 || !got.Live(a.now()) {
		t.Fatal("new lease was changed", got, err)
	}
	if err := a.EndSTGWindow(ctx, next); err != nil {
		t.Fatal("release success control", err)
	}
	cs.ClearActions()
	expired, err := a.ReadSTGWindow(ctx)
	if err != nil || expired.Live(a.now()) || expired.Generation != 2 || len(cs.Actions()) != 1 || cs.Actions()[0].GetVerb() != "get" {
		t.Fatal("expired read lost generation or wrote", expired, err, cs.Actions())
	}
	if _, err := a.RenewSTGWindow(ctx, expired); err == nil {
		t.Fatal("expired lease renewed")
	}
	if _, err := a.TakeSTGWindow(ctx, expired, who, 3, true); err != nil {
		t.Fatal("expired same-try recovery control", err)
	}
}

func TestSTGWindowCoexistsWithManualHolders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a, cs := stgWindowCAS(t)
	manual := windowInput{What: "bifrost-platform", Who: "operator@host", Env: "stg", TTLMinutes: 5}
	if _, err := putReleaseWindow(ctx, cs, "cicd", manual, a.now()); err != nil {
		t.Fatal(err)
	}
	old, err := a.ReadSTGWindow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cs.ClearActions()
	if _, err := a.TakeSTGWindow(ctx, old, "stgrel-bifrost-platform-1-1", 1, true); err == nil || len(cs.Actions()) != 0 {
		t.Fatal("automatic adoption changed a human window", err)
	}
	// release.sh still renews/releases through the original entry points.
	if _, err := putReleaseWindow(ctx, cs, "cicd", manual, a.now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := deleteReleaseWindow(ctx, cs, "cicd", manual.Who, false, a.now()); err != nil {
		t.Fatal(err)
	}
	old, err = a.ReadSTGWindow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	held, err := a.TakeSTGWindow(ctx, old, "stgrel-bifrost-platform-1-1", 1, false)
	if err != nil {
		t.Fatal("acquire after manual release", err)
	}
	if _, err := putReleaseWindow(ctx, cs, "cicd", manual, a.now()); err == nil {
		t.Fatal("human acquire overwrote a live B5 window")
	}
	// Forced manual replacement makes B5's captured release conflict.
	if err := deleteReleaseWindow(ctx, cs, "cicd", manual.Who, true, a.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := putReleaseWindow(ctx, cs, "cicd", manual, a.now()); err != nil {
		t.Fatal(err)
	}
	if err := a.EndSTGWindow(ctx, held); err == nil {
		t.Fatal("release overwrote the human replacement")
	}
	got, err := a.ReadSTGWindow(ctx)
	if err != nil || got.Who != manual.Who || !got.Live(a.now()) {
		t.Fatal(got, err)
	}
}

func TestSTGWindowReadAndTakeRefusals(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"valid", "json", "expiry", "generation", "wrong env", "wrong what", "manual same who", "same generation", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			a, cs := stgWindowCAS(t)
			r := windowRecord{Who: "stgrel-bifrost-platform-1-1", What: "bifrost-platform", Env: "stg", ExpiresAt: a.now().Add(time.Minute).Format(time.RFC3339), Reason: "stgrel-gen:1"}
			switch mode {
			case "expiry":
				r.ExpiresAt = "bad"
			case "generation":
				r.Reason = "stgrel-gen:01"
			case "overflow":
				r.Reason = "stgrel-gen:999999999999999999999"
			case "wrong env":
				r.Env = "prod"
			case "wrong what":
				r.What = "bifrost-trade-core"
			case "manual same who":
				r.Reason = "hold"
			}
			body, _ := json.Marshal(r)
			if mode == "json" {
				body = []byte("{")
			}
			_, err := cs.CoreV1().ConfigMaps("cicd").Create(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap}, Data: map[string]string{releaseWindowDataKey: string(body)}}, metav1.CreateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			cs.ClearActions()
			old, err := a.ReadSTGWindow(context.Background())
			if mode == "json" || mode == "expiry" || mode == "generation" || mode == "overflow" {
				if err == nil || len(cs.Actions()) != 1 {
					t.Fatal("unreadable record treated as free", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			gen := int64(2)
			if mode == "same generation" {
				gen = 1
			}
			_, err = a.TakeSTGWindow(context.Background(), old, r.Who, gen, true)
			if mode == "valid" {
				if err != nil {
					t.Fatal("same-entry success", err)
				}
			} else if err == nil || len(cs.Actions()) != 1 {
				t.Fatal("invalid takeover wrote", mode, err, cs.Actions())
			}
		})
	}
}

func TestSTGWindowGenerationCheckedAtNamedCreate(t *testing.T) {
	t.Parallel()
	for _, takeover := range []bool{false, true} {
		t.Run(strconv.FormatBool(takeover), func(t *testing.T) {
			t.Parallel()
			a, dyn, _ := stgAdapter(t)
			windows, core := stgWindowCAS(t)
			a.Core = func() (kubernetes.Interface, error) { return core, nil }
			id, err := stgInput().Identity()
			if err != nil {
				t.Fatal(err)
			}
			one, err := windows.TakeSTGWindow(context.Background(), STGWindow{}, id.Who, 1, false)
			if err != nil {
				t.Fatal(err)
			}
			if takeover {
				// Same who/what/env still passes the generic named-run check.
				// The generation check must fence the stale sender separately.
				if _, err := windows.TakeSTGWindow(context.Background(), one, id.Who, 2, true); err != nil {
					t.Fatal(err)
				}
			}
			creates := trackSTGCreates(t, dyn, "")
			checks := 0
			_, _, err = a.CreatePermittedNamedDeliverRun(context.Background(), stgInput(), func(ctx context.Context) error {
				checks++
				w, err := windows.ReadSTGWindow(ctx)
				if err != nil {
					return err
				}
				if w.Generation != one.Generation {
					return &WindowChanged{}
				}
				return nil
			})
			if checks != 1 {
				t.Fatal("did not reach the create boundary", checks)
			}
			if takeover {
				if err == nil || *creates != 0 {
					t.Fatal("stale sender created", err, *creates)
				}
			} else if err != nil || *creates != 1 {
				t.Fatal("same-entry success control", err, *creates)
			}
		})
	}
}
