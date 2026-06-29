package cluster

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const kubeconfigSecretName = "bifrost-platform-kubeconfig"
const kubeconfigSecretKey = "bifrost-k3s.yaml"

// EnsureKubeconfigSecretRequest specifies which namespaces to provision.
// Empty Namespaces defaults to the platform STG+PROD namespaces.
type EnsureKubeconfigSecretRequest struct {
	Namespaces []string `json:"namespaces,omitempty"`
	SyncFirst  bool     `json:"sync_first,omitempty"`
}

// EnsureKubeconfigSecretResponse extends ActuationResponse with per-namespace detail.
type EnsureKubeconfigSecretResponse struct {
	ActuationResponse
	SyncResult   *SyncResponse                    `json:"sync_result,omitempty"`
	SecretPath   string                            `json:"secret_path"`
	PerNamespace []KubeconfigSecretNamespaceResult `json:"per_namespace"`
}

type KubeconfigSecretNamespaceResult struct {
	Namespace string `json:"namespace"`
	Action    string `json:"action"`
	OK        bool   `json:"ok"`
	Detail    string `json:"detail"`
}

func defaultPlatformNamespaces() []string {
	return []string{"bifrost-platform-stg", "bifrost-platform-prod"}
}

// EnsureKubeconfigSecret syncs the local kubeconfig (optionally), reads the
// file, then creates or updates the bifrost-platform-kubeconfig Secret in
// each requested namespace.
func (s *Service) EnsureKubeconfigSecret(ctx context.Context, req EnsureKubeconfigSecretRequest) (EnsureKubeconfigSecretResponse, error) {
	now := time.Now().UTC()
	namespaces := req.Namespaces
	if len(namespaces) == 0 {
		namespaces = defaultPlatformNamespaces()
	}
	target := strings.Join(namespaces, ",")

	resp := EnsureKubeconfigSecretResponse{
		ActuationResponse: ActuationResponse{
			Action:      "ensure-kubeconfig-secret",
			Target:      target,
			GeneratedAt: now,
		},
	}

	if req.SyncFirst {
		syncResp := s.SyncKubeconfig()
		resp.SyncResult = &syncResp
		if !syncResp.OK {
			resp.Message = fmt.Sprintf("kubeconfig sync failed: %s", syncResp.Message)
			return resp, fmt.Errorf("%s", resp.Message)
		}
	}

	kubeconfigPath := s.kubeconfigPath()
	if kubeconfigPath == "" {
		resp.Message = "kubeconfig path not configured (set PLATFORM_KUBECONFIG)"
		return resp, fmt.Errorf("%s", resp.Message)
	}
	resp.SecretPath = kubeconfigPath

	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		resp.Message = fmt.Sprintf("cannot read kubeconfig %s: %v", kubeconfigPath, err)
		return resp, err
	}
	if len(data) == 0 {
		resp.Message = fmt.Sprintf("kubeconfig file is empty: %s", kubeconfigPath)
		return resp, fmt.Errorf("%s", resp.Message)
	}

	clientset, _, err := s.buildClient()
	if err != nil {
		resp.Message = fmt.Sprintf("cluster client: %v", err)
		return resp, err
	}

	var results []KubeconfigSecretNamespaceResult
	createdCount := 0
	updatedCount := 0
	for _, ns := range namespaces {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			continue
		}
		result := ensureSecretInNamespace(ctx, clientset, ns, data)
		results = append(results, result)
		if !result.OK {
			resp.PerNamespace = results
			resp.Message = fmt.Sprintf("failed in namespace %s: %s", ns, result.Detail)
			return resp, fmt.Errorf("%s", resp.Message)
		}
		switch result.Action {
		case "created":
			createdCount++
		case "updated":
			updatedCount++
		}
	}

	resp.PerNamespace = results
	resp.OK = true
	resp.Changed = createdCount > 0 || updatedCount > 0

	parts := make([]string, 0, 3)
	if createdCount > 0 {
		parts = append(parts, fmt.Sprintf("%d created", createdCount))
	}
	if updatedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d updated", updatedCount))
	}
	unchanged := len(results) - createdCount - updatedCount
	if unchanged > 0 {
		parts = append(parts, fmt.Sprintf("%d unchanged", unchanged))
	}
	resp.Message = fmt.Sprintf("kubeconfig secret ensured: %s", strings.Join(parts, ", "))
	return resp, nil
}

func ensureSecretInNamespace(ctx context.Context, clientset kubernetes.Interface, ns string, kubeconfigData []byte) KubeconfigSecretNamespaceResult {
	secretsClient := clientset.CoreV1().Secrets(ns)

	existing, err := secretsClient.Get(ctx, kubeconfigSecretName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return KubeconfigSecretNamespaceResult{Namespace: ns, Action: "error", Detail: err.Error()}
	}

	secretLabels := map[string]string{
		"app.kubernetes.io/part-of":  "bifrost-platform",
		"bifrost.dev/managed-by":     "platform-api",
		"bifrost.dev/secret-purpose": "kubeconfig",
	}

	if apierrors.IsNotFound(err) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kubeconfigSecretName,
				Namespace: ns,
				Labels:    secretLabels,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				kubeconfigSecretKey: kubeconfigData,
			},
		}
		_, createErr := secretsClient.Create(ctx, secret, metav1.CreateOptions{})
		if createErr != nil {
			return KubeconfigSecretNamespaceResult{Namespace: ns, Action: "create-failed", Detail: createErr.Error()}
		}
		return KubeconfigSecretNamespaceResult{Namespace: ns, Action: "created", OK: true, Detail: "secret created"}
	}

	existing.Data = map[string][]byte{kubeconfigSecretKey: kubeconfigData}
	existing.Labels = secretLabels
	_, updateErr := secretsClient.Update(ctx, existing, metav1.UpdateOptions{})
	if updateErr != nil {
		return KubeconfigSecretNamespaceResult{Namespace: ns, Action: "update-failed", Detail: updateErr.Error()}
	}
	return KubeconfigSecretNamespaceResult{Namespace: ns, Action: "updated", OK: true, Detail: "secret updated"}
}
