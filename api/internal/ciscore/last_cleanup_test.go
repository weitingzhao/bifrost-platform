package ciscore

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Origin: 2026-11-18, decoupling item 5: this moved out of api/internal/server,
// where nothing tested it; when the cleanup last succeeded is read from its CronJob, a job that
// has not succeeded yet is no time and no error, and a missing one an error.
func TestLastCleanupReadsTheCronJob(t *testing.T) {
	at := time.Date(2026, 11, 18, 3, 0, 0, 0, time.UTC)
	done := metav1.NewTime(at)
	job := func(last *metav1.Time) *batchv1.CronJob {
		return &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: cleanupJob, Namespace: "cicd"}, Status: batchv1.CronJobStatus{LastSuccessfulTime: last}}
	}
	ctx := context.Background()
	if got, err := LastCleanup(ctx, fake.NewSimpleClientset(job(&done)), "cicd"); err != nil || !got.Equal(at) {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, err := LastCleanup(ctx, fake.NewSimpleClientset(job(nil)), "cicd"); err != nil || !got.IsZero() {
		t.Fatalf("never succeeded: %v, %v", got, err)
	}
	if _, err := LastCleanup(ctx, fake.NewSimpleClientset(), "cicd"); err == nil {
		t.Fatal("a missing CronJob read as no error")
	}
}
