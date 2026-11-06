package actions

import (
	"slices"
	"testing"
)

func TestCoveredBy(t *testing.T) {
	old := currentPolicy()
	t.Cleanup(func() { SetActuationPolicy(old) })
	SetActuationPolicy(loadPolicy(t))
	tests := []struct{ command, path string }{
		{"kubectl apply -f k8s/cicd/tekton/x.yaml", "k8s/cicd/tekton/x.yaml"},
		{"kubectl --context dev --kubeconfig /tmp/config apply --filename='/work/bifrost-trade-infra/k8s/cicd/tekton/x.yaml'", "k8s/cicd/tekton/x.yaml"},
		{"kubectl -n cicd apply -k k8s/monitoring", "k8s/monitoring"},
		{"kustomize build k8s/cicd/tekton | kubectl apply -f -", "k8s/cicd/tekton"},
		{"kubectl apply --server-side --force-conflicts -f k8s/cicd/tekton/x.yaml", ""},
		{"kubectl apply -f k8s/platform-rbac/40-admission.yaml", ""},
		{"kubectl get -f k8s/cicd/tekton/x.yaml", ""},
		{"echo 'kubectl apply -f k8s/monitoring'", ""},
		{"kubectl create --filename=./k8s/monitoring/x.yaml", "k8s/monitoring/x.yaml"},
		{"kubectl replace --kustomize=k8s/monitoring", "k8s/monitoring"},
		{"kubectl apply -f k8s/monitoring-other/x.yaml", ""},
		{"kubectl get pods && kubectl apply -f elsewhere -f k8s/monitoring/x.yaml", "k8s/monitoring/x.yaml"},
		{"echo 'a|b; c && d' || kubectl apply -k=k8s/monitoring", "k8s/monitoring"},
		{"kubectl apply --server-side=false --force-conflicts -f k8s/monitoring", "k8s/monitoring"},
		{"kustomize build k8s/monitoring; kubectl apply -f -", ""},
		{"kubectl apply -f /work/bifrost-research/k8s/monitoring", ""},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			repo, path, ok := CoveredBy(tt.command)
			if ok != (tt.path != "") || path != tt.path || ok && repo != "bifrost-trade-infra" {
				t.Fatalf("CoveredBy = %q, %q, %v", repo, path, ok)
			}
		})
	}
	SetActuationPolicy(nil)
	for _, tt := range tests {
		if repo, path, ok := CoveredBy(tt.command); ok || repo != "" || path != "" {
			t.Fatalf("no policy: %q, %q, %v", repo, path, ok)
		}
	}
}

func TestCommandTokens(t *testing.T) {
	tests := []struct {
		command string
		want    []string
	}{
		{`kubectl apply -f "k8s/a b/x.yaml"`, []string{"kubectl", "apply", "-f", "k8s/a b/x.yaml"}},
		{`echo '|' "&&" ';' || kubectl get pods`, []string{"echo", "|", "&&", ";", "\x00||", "kubectl", "get", "pods"}},
		{"echo a\\ b\nkubectl get pods", []string{"echo", "a b", "\x00;", "kubectl", "get", "pods"}},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := commandTokens(tt.command)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("tokens = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestCoveredByNamedCheckout(t *testing.T) {
	old := currentPolicy()
	SetActuationPolicy(loadPolicy(t))
	t.Cleanup(func() { SetActuationPolicy(old) })
	repo, path, ok := CoveredBy("kubectl apply -f /work/bifrost-research/k8s/orchestration/x.yaml")
	if !ok || repo != "bifrost-research" || path != "k8s/orchestration/x.yaml" {
		t.Fatalf("CoveredBy = %q, %q, %v", repo, path, ok)
	}
}
