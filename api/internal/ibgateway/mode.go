package ibgateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
)

func patchGatewayYamlMode(yamlContent, mode string) string {
	lines := strings.Split(yamlContent, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "mode:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "mode: " + mode
			break
		}
	}
	return strings.Join(lines, "\n")
}

func (s *Service) SetMode(ctx context.Context, mode string) (ControlResponse, error) {
	now := time.Now().UTC()
	target := dataNamespace + "/ConfigMap/" + configMapName
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "mock" && mode != "live" {
		return ControlResponse{
			OK: false, Action: "ib-gateway.mode", Target: target,
			Autonomy: "L1", Message: "mode must be mock or live", GeneratedAt: now,
		}, fmt.Errorf("invalid mode %q", mode)
	}
	if s.cluster == nil {
		return ControlResponse{
			OK: false, Action: "ib-gateway.mode", Target: target,
			Autonomy: "L1", Message: "cluster service unavailable", GeneratedAt: now,
		}, fmt.Errorf("cluster service unavailable")
	}
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return ControlResponse{
			OK: false, Action: "ib-gateway.mode", Target: target,
			Autonomy: "L1", Message: err.Error(), GeneratedAt: now,
		}, err
	}
	cm, err := clientset.CoreV1().ConfigMaps(dataNamespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		return ControlResponse{
			OK: false, Action: "ib-gateway.mode", Target: target,
			Autonomy: "L1", Message: err.Error(), GeneratedAt: now,
		}, err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data["mode"] = mode
	if gy := cm.Data["gateway.yaml"]; gy != "" {
		cm.Data["gateway.yaml"] = patchGatewayYamlMode(gy, mode)
	}
	if _, updateErr := clientset.CoreV1().ConfigMaps(dataNamespace).Update(ctx, cm, metav1.UpdateOptions{}); updateErr != nil {
		return ControlResponse{
			OK: false, Action: "ib-gateway.mode", Target: target,
			Autonomy: "L1", Message: updateErr.Error(), GeneratedAt: now,
		}, updateErr
	}
	restart, err := s.cluster.RolloutRestart(ctx, cluster.RolloutRestartRequest{
		Namespace: dataNamespace,
		Kind:      "Deployment",
		Name:      gatewayDeployName,
	})
	if err != nil || !restart.OK {
		msg := restart.Message
		if err != nil {
			msg = err.Error()
		}
		return ControlResponse{
			OK: false, Action: "ib-gateway.mode", Target: target,
			Autonomy: "L1", Message: "configmap updated but rollout failed: " + msg, GeneratedAt: now,
		}, fmt.Errorf("rollout after mode patch: %s", msg)
	}
	return ControlResponse{
		OK: true, Action: "ib-gateway.mode", Target: target,
		Autonomy: "L1",
		Message:  fmt.Sprintf("ib-gateway mode set to %s; deployment rollout requested", mode),
		GeneratedAt: now,
	}, nil
}
