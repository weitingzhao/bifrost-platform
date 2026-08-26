package research

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCronJobTriggerCatalog(t *testing.T) {
	cat := CronJobTriggerCatalog()
	if len(cat) != 7 {
		t.Fatalf("expected 7 triggers, got %d", len(cat))
	}
}

func TestTriggerCronJobUnknown(t *testing.T) {
	clusterSvc := cluster.NewService(nil)
	svc := NewService(clusterSvc)
	_, err := svc.TriggerCronJob(context.Background(), "not-in-whitelist")
	if err == nil {
		t.Fatal("expected error for unknown trigger")
	}
}

func TestTriggerCronJobCreatesJob(t *testing.T) {
	clientset := fake.NewSimpleClientset(&batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "research-terrain-intraday",
			Namespace: pluginNamespace,
		},
		Spec: batchv1.CronJobSpec{
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "c", Image: "test"}},
						},
					},
				},
			},
		},
	})
	clusterSvc := cluster.NewService(nil)
	clusterSvc.SetClientFactoryForTest(func() (kubernetes.Interface, string, error) {
		return clientset, "/fake", nil
	})
	svc := NewService(clusterSvc)
	resp, err := svc.TriggerCronJob(context.Background(), "terrain-intraday")
	if err != nil {
		t.Fatalf("TriggerCronJob: %v", err)
	}
	if !resp.OK || resp.JobName == "" {
		t.Fatalf("unexpected resp: %+v", resp)
	}
}
