package delivery

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kt "k8s.io/client-go/testing"
)

func TestRunWindowFollowsAfterTransientRead(t *testing.T) {
	t.Parallel()
	cs, dyn := recoveryFixture(t)
	reads := 0
	dyn.PrependReactor("get", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) {
		reads++
		if reads == 1 {
			return true, nil, errors.New("temporary read failure")
		}
		return false, nil, nil
	})
	w := &runWindow{cs: cs, ns: "cicd", who: "run", version: "1"}
	w.follow(dyn.Resource(pipelineRunGVR).Namespace("cicd"), "run", true)
	time.Sleep(42 * time.Second)
	cm, err := cs.CoreV1().ConfigMaps("cicd").Get(context.Background(), releaseWindowConfigMap, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := parseWindowCM(cm)
	expires, _ := time.Parse(time.RFC3339, r.ExpiresAt)
	if expires.Before(time.Now().Add(290 * time.Second)) {
		t.Fatalf("follower stopped after temporary read failure: expires_at=%s", r.ExpiresAt)
	}
}

func TestRunWindowRetryBudget(t *testing.T) {
	for _, operation := range []string{"run-read", "window-read", "window-update"} {
		for _, failures := range []int{1, 3, 100} {
			t.Run(fmt.Sprintf("%s/%d", operation, failures), func(t *testing.T) {
				cs, dyn := recoveryFixture(t)
				attempts, writes := 0, 0
				fault := func(kt.Action) (bool, runtime.Object, error) {
					attempts++
					if attempts <= failures {
						return true, nil, errors.New("temporary failure")
					}
					return false, nil, nil
				}
				cs.PrependReactor("update", "configmaps", func(kt.Action) (bool, runtime.Object, error) { writes++; return false, nil, nil })
				switch operation {
				case "run-read":
					dyn.PrependReactor("get", "pipelineruns", fault)
				case "window-read":
					cs.PrependReactor("get", "configmaps", fault)
				case "window-update":
					cs.PrependReactor("update", "configmaps", fault)
				}
				var events []WindowEvent
				w := &runWindow{cs: cs, ns: "cicd", who: "run", version: "1", recorder: func(_ context.Context, e WindowEvent) { events = append(events, e) }}
				ticks := make(chan time.Time, 8)
				for i := 0; i < 8; i++ {
					ticks <- time.Now()
				}
				close(ticks)
				w.followTicks(context.Background(), dyn.Resource(pipelineRunGVR).Namespace("cicd"), "run", true, ticks)
				if failures < 5 {
					if writes == 0 || len(events) != 0 {
						t.Fatalf("did not recover: writes=%d events=%v", writes, events)
					}
				} else if attempts != 5 || writes != 0 || len(events) != 1 || events[0].Action != "follow_failed" {
					t.Fatalf("unbounded or silent failure: attempts=%d writes=%d events=%v", attempts, writes, events)
				}
			})
		}
	}
}
