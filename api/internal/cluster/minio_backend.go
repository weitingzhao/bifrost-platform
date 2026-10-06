package cluster

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

const (
	minioServiceName   = "minio"
	minioHealthPath    = "/minio/health/cluster"
	minioHealthTimeout = 5 * time.Second
)

// minioBackend says where the backup MinIO behind Service data/minio runs.
//
// In-cluster: the Service selects the minio Deployment's pods (the original
// layout). External: the Service has no selector and a hand-written
// EndpointSlice pointing outside the cluster — since 2026-10-06 the MinIO on the
// NAS, run with docker compose. An external MinIO cannot be restarted or exec'd
// into from here, and the minio Deployment is kept at 0 replicas on purpose:
// starting it would put a second server on the same data directory.
type minioBackend struct {
	External bool
	Endpoint string // http://host:port, external only
	Reach    probe.Reachability
	Detail   string
}

// minioEndpointFromService returns the external endpoint of a selectorless
// Service, or "" when the Service selects pods (in-cluster MinIO).
func minioEndpointFromService(svc *corev1.Service, slices []discoveryv1.EndpointSlice) (external bool, endpoint string) {
	if svc == nil || len(svc.Spec.Selector) > 0 {
		return false, ""
	}
	for i := range slices {
		port := int32(0)
		for _, p := range slices[i].Ports {
			if p.Port == nil {
				continue
			}
			if p.Name != nil && *p.Name == "api" {
				port = *p.Port
				break
			}
			if port == 0 {
				port = *p.Port
			}
		}
		if port == 0 {
			continue
		}
		for _, ep := range slices[i].Endpoints {
			if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
				continue
			}
			if len(ep.Addresses) == 0 {
				continue
			}
			return true, "http://" + net.JoinHostPort(ep.Addresses[0], strconv.Itoa(int(port)))
		}
	}
	return true, ""
}

// classifyMinioHealth maps a /minio/health/cluster answer. That endpoint is
// the one that turns 503 when the drive is offline or write quorum is lost;
// /live and /ready keep answering 200 in that state (2026-10-05).
func classifyMinioHealth(endpoint string, status int, err error) (probe.Reachability, string) {
	where := "MinIO @ " + strings.TrimPrefix(endpoint, "http://")
	switch {
	case err != nil:
		return probe.ReachFail, where + " unreachable: " + err.Error()
	case status == http.StatusOK:
		return probe.ReachOK, where + " healthy (outside the cluster)"
	case status == http.StatusServiceUnavailable:
		return probe.ReachFail, where + " answers 503 on " + minioHealthPath + ": drive offline or write quorum lost — restart that MinIO server"
	default:
		return probe.ReachDegraded, fmt.Sprintf("%s answered HTTP %d on %s", where, status, minioHealthPath)
	}
}

func probeMinioHealth(ctx context.Context, endpoint string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, minioHealthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+minioHealthPath, nil)
	if err != nil {
		return 0, err
	}
	resp, err := (&http.Client{Timeout: minioHealthTimeout}).Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// resolveMinioBackend reads Service data/minio and, when it points outside the
// cluster, checks the MinIO there. Errors reading the Service fall back to the
// in-cluster layout, whose checks report the Deployment as before.
func (s *Service) resolveMinioBackend(ctx context.Context, clientset kubernetes.Interface) minioBackend {
	svc, err := clientset.CoreV1().Services(cnpgNamespace).Get(ctx, minioServiceName, metav1.GetOptions{})
	if err != nil {
		return minioBackend{}
	}
	if len(svc.Spec.Selector) > 0 {
		return minioBackend{}
	}
	list, err := clientset.DiscoveryV1().EndpointSlices(cnpgNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: discoveryv1.LabelServiceName + "=" + minioServiceName,
	})
	if err != nil {
		return minioBackend{External: true, Reach: probe.ReachDegraded, Detail: "MinIO outside the cluster: list EndpointSlices: " + err.Error()}
	}
	_, endpoint := minioEndpointFromService(svc, list.Items)
	if endpoint == "" {
		return minioBackend{External: true, Reach: probe.ReachFail, Detail: "Service data/minio has no selector and no ready EndpointSlice address"}
	}
	probeFn := s.minioHealthProbe
	if probeFn == nil {
		probeFn = probeMinioHealth
	}
	status, perr := probeFn(ctx, endpoint)
	reach, detail := classifyMinioHealth(endpoint, status, perr)
	return minioBackend{External: true, Endpoint: endpoint, Reach: reach, Detail: detail}
}
