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
	"strings"
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

// replaceWindowBeforeDelete simulates an update between the read and delete.
// The fake tracker needs explicit enforcement of delete preconditions.
func replaceWindowBeforeDelete(t *testing.T, cs *fake.Clientset, replacement windowRecord) {
	t.Helper()
	first := true
	resource := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	cs.PrependReactor("delete", "configmaps", func(action clienttesting.Action) (bool, runtime.Object, error) {
		deletion := action.(clienttesting.DeleteAction)
		if deletion.GetName() != releaseWindowConfigMap {
			return false, nil, nil
		}
		obj, err := cs.Tracker().Get(resource, action.GetNamespace(), deletion.GetName())
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
		pre := deletion.GetDeleteOptions().Preconditions
		if pre != nil && ((pre.ResourceVersion != nil && *pre.ResourceVersion != cm.ResourceVersion) || (pre.UID != nil && *pre.UID != cm.UID)) {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, cm.Name, nil)
		}
		return false, nil, nil
	})
}

func TestReleaseWindowConditionalDelete(t *testing.T) {
	now := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	for _, operation := range []string{"read", "release"} {
		for _, race := range []bool{false, true} {
			name := operation + "/no-race"
			if race {
				name = operation + "/race"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				original := windowRecord{Who: "a@host", What: "repo", ExpiresAt: now.Add(time.Minute).Format(time.RFC3339)}
				if operation == "read" {
					original.ExpiresAt = now.Add(-time.Minute).Format(time.RFC3339)
				}
				body, err := json.Marshal(original)
				if err != nil {
					t.Fatal(err)
				}
				cs := fake.NewSimpleClientset(&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: "cicd", ResourceVersion: "1", UID: "window-uid"},
					Data:       map[string]string{releaseWindowDataKey: string(body)},
				})
				replacement := windowRecord{Who: "b@host", What: "repo", ExpiresAt: now.Add(time.Minute).Format(time.RFC3339)}
				if operation == "read" {
					replacement.Who = "other@host"
				}
				if race {
					replaceWindowBeforeDelete(t, cs, replacement)
				}
				if operation == "read" {
					rec, live, err := getReleaseWindow(ctx, cs, "cicd", now)
					if err != nil {
						t.Fatal(err)
					}
					if live != race || (race && rec.Who != replacement.Who) {
						t.Fatalf("read: live=%v who=%s", live, rec.Who)
					}
				} else {
					err := deleteReleaseWindow(ctx, cs, "cicd", "a@host", false, now)
					if race {
						if _, ok := err.(*WindowChanged); !ok || !strings.Contains(err.Error(), "changed") {
							t.Fatalf("expected window changed error, got %v", err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
				}
				cm, err := cs.CoreV1().ConfigMaps("cicd").Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
				if !race {
					if !apierrors.IsNotFound(err) {
						t.Fatalf("expected removed ConfigMap, got %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				rec, ok := parseWindowCM(cm)
				if !ok || !rec.live(now) || rec.Who != replacement.Who || cm.ResourceVersion != "2" {
					t.Fatalf("replacement was lost: %+v", cm)
				}
			})
		}
	}
}
