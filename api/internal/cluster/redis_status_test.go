package cluster

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestSummarizeRedisStatusEmbeddedOnly(t *testing.T) {
	resp := RedisStatusResponse{
		TargetsTotal:   5,
		TargetsReady:   0,
		EmbeddedActive: 3,
	}
	reach, summary := summarizeRedisStatus(resp)
	if reach != probe.ReachDegraded {
		t.Fatalf("reach=%s", reach)
	}
	if summary == "" {
		t.Fatal("expected summary")
	}
}

func TestSummarizeRedisStatusAllTargets(t *testing.T) {
	resp := RedisStatusResponse{
		TargetsTotal:   5,
		TargetsReady:   5,
		EmbeddedActive: 0,
	}
	reach, _ := summarizeRedisStatus(resp)
	if reach != probe.ReachOK {
		t.Fatalf("reach=%s", reach)
	}
}

func TestEvalRedisDomainPartial(t *testing.T) {
	replicas := int32(1)
	snap := readinessSnapshot{
		clusterCaps: map[string]ClusterCapabilityView{
			"storage-class-local-path": {Reachability: probe.ReachOK},
			"storage-class-nfs-hot":    {Reachability: probe.ReachOK},
		},
		nodes: []NodeView{
			{Name: "n1", Architecture: "amd64", Status: "Ready", Reachability: probe.ReachOK},
		},
		deployments: map[string]appsv1.Deployment{
			"bifrost-stg/redis": {
				ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "bifrost-stg"},
				Status:     appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
				Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
			},
		},
	}
	d := evalRedisDomain(snap)
	if d.Status != "partial" && d.Reachability != probe.ReachDegraded {
		t.Fatalf("status=%s reach=%s summary=%s", d.Status, d.Reachability, d.Summary)
	}
}
