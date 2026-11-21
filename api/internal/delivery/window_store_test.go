package delivery

import (
	"context"
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clienttesting "k8s.io/client-go/testing"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

func TestReleaseWindowOneHolderWins(t *testing.T) {
	cs := fake.NewSimpleClientset()
	now := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	held := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		who := "holder-" + string(rune('a'+i))
		go func() {
			defer wg.Done()
			_, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{
				What: "bifrost-trade-core", Who: who, Reason: "hold", TTLMinutes: 5,
			}, now)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
				return
			}
			if _, ok := err.(*WindowHeld); ok {
				held++
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins=%d held=%d, want one winner", wins, held)
	}
	if held != 7 {
		t.Fatalf("held=%d, want the other seven refused", held)
	}
}

func TestReleaseWindowRenewAndForeignDelete(t *testing.T) {
	cs := fake.NewSimpleClientset()
	now := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	first, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{
		What: "bifrost-trade-core", Who: "ada@host", Reason: "hold", TTLMinutes: 5,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{
		What: "bifrost-trade-core", Who: "ada@host", Reason: "hold", TTLMinutes: 5,
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if renewed.StartedAt != first.StartedAt {
		t.Fatalf("renewal reset started_at %s -> %s", first.StartedAt, renewed.StartedAt)
	}
	if renewed.ExpiresAt == first.ExpiresAt {
		t.Fatal("renewal did not move expires_at")
	}
	if err := deleteReleaseWindow(context.Background(), cs, "cicd", "bob@host", false, now.Add(time.Minute)); err == nil {
		t.Fatal("a non-holder deleted the window")
	}
	got, open, err := getReleaseWindow(context.Background(), cs, "cicd", now.Add(time.Minute))
	if err != nil || !open || got.Who != "ada@host" {
		t.Fatalf("window after refused delete: open=%v who=%s err=%v", open, got.Who, err)
	}
	if err := deleteReleaseWindow(context.Background(), cs, "cicd", "ada@host", false, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, open, err := getReleaseWindow(context.Background(), cs, "cicd", now.Add(time.Minute)); err != nil || open {
		t.Fatalf("window still open after holder delete: open=%v err=%v", open, err)
	}
}

func TestExpiredWindowIsEmptyAndReplaceable(t *testing.T) {
	cs := fake.NewSimpleClientset()
	start := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	if _, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{
		What: "bifrost-trade-core", Who: "ada@host", Reason: "stg", TTLMinutes: 5,
	}, start); err != nil {
		t.Fatal(err)
	}
	later := start.Add(6 * time.Minute)
	if _, open, err := getReleaseWindow(context.Background(), cs, "cicd", later); err != nil || open {
		t.Fatalf("expired window still open: open=%v err=%v", open, err)
	}
	if _, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{
		What: "bifrost-research", Who: "bob@host", Reason: "hold", TTLMinutes: 5,
	}, later); err != nil {
		t.Fatal(err)
	}
	got, open, err := getReleaseWindow(context.Background(), cs, "cicd", later)
	if err != nil || !open || got.Who != "bob@host" {
		t.Fatalf("replacement: open=%v who=%s err=%v", open, got.Who, err)
	}
	if err := deleteReleaseWindow(context.Background(), cs, "cicd", "ada@host", true, later); err != nil {
		t.Fatal(err)
	}
	if _, open, err := getReleaseWindow(context.Background(), cs, "cicd", later); err != nil || open {
		t.Fatalf("force delete left the window open: open=%v err=%v", open, err)
	}
}

// replaceWindowBeforeUpdate simulates another caller taking the window
// between a release's read and its update. The fake tracker does not check
// resourceVersion on Update by itself.
func replaceWindowBeforeUpdate(t *testing.T, cs *fake.Clientset, replacement windowRecord) {
	t.Helper()
	first := true
	resource := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	cs.PrependReactor("update", "configmaps", func(action clienttesting.Action) (bool, runtime.Object, error) {
		update := action.(clienttesting.UpdateAction)
		sent := update.GetObject().(*corev1.ConfigMap)
		if sent.Name != releaseWindowConfigMap {
			return false, nil, nil
		}
		obj, err := cs.Tracker().Get(resource, action.GetNamespace(), sent.Name)
		if err != nil {
			return true, nil, err
		}
		cm := obj.(*corev1.ConfigMap).DeepCopy()
		if first {
			first = false
			body, err := json.Marshal(replacement)
			if err != nil {
				t.Fatal(err)
			}
			cm.Data = map[string]string{releaseWindowDataKey: string(body)}
			cm.ResourceVersion = "2"
			if err := cs.Tracker().Update(resource, cm, action.GetNamespace()); err != nil {
				t.Fatal(err)
			}
		}
		if sent.ResourceVersion != cm.ResourceVersion {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, cm.Name, nil)
		}
		return false, nil, nil
	})
}

// CUT §2.6 (S3): a read never deletes or writes; an expired window reads as
// free and stays. A release ends the window now with an Update on the
// version it read, never a delete: a window taken meanwhile is left alone
// (WindowChanged). A record that does not parse is not free.
func TestReleaseWindowReadsAndReleasesWithoutDeleting(t *testing.T) {
	now := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	ctx := context.Background()
	seed := func(rec windowRecord) *fake.Clientset {
		body, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		return fake.NewSimpleClientset(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: "cicd", ResourceVersion: "1", UID: "window-uid"},
			Data:       map[string]string{releaseWindowDataKey: string(body)},
		})
	}
	expired := windowRecord{Who: "a@host", What: "repo", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339)}
	cs := seed(expired)
	_, live, err := getReleaseWindow(ctx, cs, "cicd", now)
	if err != nil || live {
		t.Fatalf("expired read: live=%v err=%v", live, err)
	}
	for _, a := range cs.Actions() {
		if a.GetVerb() != "get" {
			t.Fatalf("a read wrote: %s", a.GetVerb())
		}
	}
	if _, err := cs.CoreV1().ConfigMaps("cicd").Get(ctx, releaseWindowConfigMap, metav1.GetOptions{}); err != nil {
		t.Fatal("the expired window was removed", err)
	}

	held := windowRecord{Who: "a@host", What: "repo", ExpiresAt: now.Add(time.Minute).Format(time.RFC3339)}
	cs = seed(held)
	if err := deleteReleaseWindow(ctx, cs, "cicd", "a@host", false, now); err != nil {
		t.Fatal(err)
	}
	cm, err := cs.CoreV1().ConfigMaps("cicd").Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if err != nil {
		t.Fatal("a release deleted the window", err)
	}
	if rec, ok := parseWindowCM(cm); !ok || rec.live(now) || rec.Who != "a@host" {
		t.Fatalf("released window: %+v", rec)
	}

	cs = seed(held)
	taken := windowRecord{Who: "b@host", What: "repo", ExpiresAt: now.Add(time.Minute).Format(time.RFC3339)}
	replaceWindowBeforeUpdate(t, cs, taken)
	err = deleteReleaseWindow(ctx, cs, "cicd", "a@host", false, now)
	if _, ok := err.(*WindowChanged); !ok {
		t.Fatalf("expected window changed, got %v", err)
	}
	cm, err = cs.CoreV1().ConfigMaps("cicd").Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rec, ok := parseWindowCM(cm); !ok || rec.Who != "b@host" || !rec.live(now) {
		t.Fatalf("the window taken meanwhile was ended: %+v", rec)
	}

	cs = fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: "cicd", ResourceVersion: "1"},
		Data:       map[string]string{releaseWindowDataKey: "{not json"},
	})
	if _, _, err := getReleaseWindow(ctx, cs, "cicd", now); err == nil {
		t.Fatal("an unreadable window read as free")
	}
}
