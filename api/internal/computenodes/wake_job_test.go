package computenodes

import (
	"errors"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// TD-287, 2026-11-22: #686 proved that a host-network WOL job can boot
// the 4090. Queueing a job without a confirmed send does not replace that proof.
func TestWakeNodeRequiresCompletedSender(t *testing.T) {
	for _, test := range []struct {
		name      string
		createErr error
		condition batchv1.JobConditionType
		wantError string
		target    string
		startsJob bool
	}{
		{name: "completed", target: wakeGPUNode, condition: batchv1.JobComplete, startsJob: true},
		{name: "failed", target: wakeGPUNode, condition: batchv1.JobFailed, wantError: "WOL send not confirmed", startsJob: true},
		{name: "admission denied", target: wakeGPUNode, createErr: errors.New("admission denied"), wantError: "admission denied", startsJob: true},
		{name: "other registered node", target: "another-gpu", wantError: "not configured"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			client.PrependReactor("create", "jobs", func(a ktesting.Action) (bool, runtime.Object, error) {
				if test.createErr != nil {
					return true, nil, test.createErr
				}
				job := a.(ktesting.CreateAction).GetObject().(*batchv1.Job)
				job.Name = "gpu4090-wol-test"
				job.Status.Conditions = []batchv1.JobCondition{{Type: test.condition, Status: corev1.ConditionTrue}}
				return false, job, nil
			})
			svc := cluster.NewService(&config.ClusterEntry{
				ComputeNodes: []config.ComputeNodeSpec{{Name: wakeGPUNode, WolMAC: wakeGPUMAC}, {Name: "another-gpu", WolMAC: wakeGPUMAC}},
			})
			svc.SetClientFactoryForTest(func() (kubernetes.Interface, string, error) { return client, "fake", nil })
			out, err := WakeNode(t.Context(), svc, test.target)
			if test.wantError == "" {
				if err != nil || !out.OK || !strings.Contains(out.Message, "WOL packet sent") {
					t.Fatalf("wake = %+v, %v", out, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantError) || out.OK {
				t.Fatalf("wake = %+v, %v; want failure %q", out, err, test.wantError)
			}
			if got := len(client.Actions()) > 0; got != test.startsJob {
				t.Fatalf("API called = %t, want %t", got, test.startsJob)
			}
		})
	}
}
