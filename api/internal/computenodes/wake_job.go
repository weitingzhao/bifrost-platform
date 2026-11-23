package computenodes

import (
	"context"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
)

const (
	wakeGPUNode      = "gpu-server"
	wakeGPUMAC       = "c8:7f:54:5b:b8:33"
	wakeJobNamespace = "bifrost-platform-stg"
	wakeSenderNode   = "ubt-k3s-07"
	wakeSenderImage  = "192.168.10.73:30500/bifrost-research@sha256:8cd0b34bbff05e1c6bc21b555e9d196813c484f6d49514583a78def32b4b83c5"
	wakePacketScript = `import socket
packet = bytes([255]) * 6 + bytes.fromhex('c87f545bb833') * 16
with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_BROADCAST, 1)
    sent = sock.sendto(packet, ('192.168.10.255', 9))
if sent != len(packet):
    raise SystemExit('WOL packet was not fully sent')
print(f'gpu-server: sent {sent} WOL bytes via host network')`
)

// WakeNode sends a WOL packet using the host network of the cluster node
// verified in approval #686. Admission allows only the fixed sender job.
func WakeNode(ctx context.Context, c *cluster.Service, nodeName string) (cluster.ActuationResponse, error) {
	resp := cluster.ActuationResponse{
		Action: "cluster.node.wake",
		Target: nodeName,
	}
	fail := func(err error) (cluster.ActuationResponse, error) {
		resp.Message = err.Error()
		resp.GeneratedAt = time.Now().UTC()
		return resp, err
	}
	spec := computeNodeSpec(c, nodeName)
	if spec == nil || nodeName != wakeGPUNode || !strings.EqualFold(strings.TrimSpace(spec.WolMAC), wakeGPUMAC) {
		return fail(fmt.Errorf("node %q is not configured for the fixed GPU WOL sender", nodeName))
	}

	client, _, err := c.KubernetesClient()
	if err != nil {
		return fail(err)
	}
	jobs := client.BatchV1().Jobs(wakeJobNamespace)
	job, err := jobs.Create(ctx, gpuWakeJob(), metav1.CreateOptions{})
	if err != nil {
		return fail(fmt.Errorf("create GPU WOL job: %w", err))
	}
	err = wait.PollUntilContextTimeout(ctx, time.Second, 24*time.Second, true, func(ctx context.Context) (bool, error) {
		state, err := jobs.Get(ctx, job.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, condition := range state.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				continue
			}
			switch condition.Type {
			case batchv1.JobComplete:
				return true, nil
			case batchv1.JobFailed:
				return false, fmt.Errorf("%s: %s", condition.Reason, condition.Message)
			}
		}
		return false, nil
	})
	if err != nil {
		return fail(fmt.Errorf("WOL send not confirmed for job %s/%s: %w", job.Namespace, job.Name, err))
	}
	resp.OK = true
	resp.Changed = true
	resp.Message = fmt.Sprintf("WOL packet sent to %s via %s (job %s); node readiness is checked separately", wakeGPUMAC, wakeSenderNode, job.Name)
	resp.GeneratedAt = time.Now().UTC()
	return resp, nil
}

func gpuWakeJob() *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "gpu4090-wol-", Namespace: wakeJobNamespace},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To[int32](0),
			ActiveDeadlineSeconds:   ptr.To[int64](20),
			TTLSecondsAfterFinished: ptr.To[int32](120),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				NodeName:                     wakeSenderNode,
				HostNetwork:                  true,
				AutomountServiceAccountToken: ptr.To(false),
				EnableServiceLinks:           ptr.To(false),
				RestartPolicy:                corev1.RestartPolicyNever,
				Containers: []corev1.Container{{
					Name:    "wake",
					Image:   wakeSenderImage,
					Command: []string{"python", "-I", "-B", "-c"},
					Args:    []string{wakePacketScript},
					SecurityContext: &corev1.SecurityContext{
						RunAsNonRoot:             ptr.To(true),
						RunAsUser:                ptr.To[int64](65534),
						AllowPrivilegeEscalation: ptr.To(false),
						ReadOnlyRootFilesystem:   ptr.To(true),
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
					},
				}},
			}},
		},
	}
}
