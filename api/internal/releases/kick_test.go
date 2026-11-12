package releases

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A kicked recorder writes the record of a run that completed since its last
// pass without waiting for its interval.
func TestKickRecordsWithoutWaitingForTheInterval(t *testing.T) {
	s, core := newFakeService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, time.Hour)
	until := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal(what)
			}
		}
	}
	until("the recorder made no first pass", func() bool { return s.Status().LastTick != nil })
	first := s.Status().LastTick

	_, dyn, err := s.clients()
	if err != nil {
		t.Fatal(err)
	}
	done := time.Date(2026, 11, 12, 21, 41, 54, 0, time.UTC)
	if _, err = dyn.Resource(pipelineRunGVR).Namespace("cicd").Create(ctx, pipelineRun("deliver-stg-9", "deliver-stg", true, done, nil, nil), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = dyn.Resource(taskRunGVR).Namespace("cicd").Create(ctx, cloneTaskRun("deliver-stg-9", "clone-core", "core", shaA), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if s.Status().LastTick != first {
		t.Fatal("the recorder passed again before its interval or a kick")
	}
	s.Kick()
	s.Kick() // asks made while one waits are one pass
	until("a kick did not record the run", func() bool {
		_, err := core.CoreV1().ConfigMaps("cicd").Get(ctx, "release-deliver-stg-9", metav1.GetOptions{})
		return err == nil
	})
}

// Kick never blocks: not without a started recorder, and not on a Service a
// test built by hand.
func TestKickNeverBlocks(t *testing.T) {
	s, _ := newFakeService(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 3; i++ {
			s.Kick()
			(&Service{}).Kick()
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Kick blocked")
	}
}
