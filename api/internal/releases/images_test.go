package releases

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRunningImagesReadsConfigAndMarksMissingSTG(t *testing.T) {
	plane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","version":"abc1234"}`))
	}))
	t.Cleanup(plane.Close)
	dir := t.TempDir()
	raw := []byte("lanes:\n" +
		"  - lane: research\n" +
		"    envs:\n" +
		"      - env: prod\n" +
		"        deployments:\n" +
		"          - namespace: research\n" +
		"            name: research-api\n" +
		"  - lane: agent\n" +
		"    envs:\n" +
		"      - env: prod\n" +
		"        planes:\n" +
		"          - name: primary\n" +
		"            url: " + plane.URL + "\n")
	if err := os.WriteFile(filepath.Join(dir, "running-images.yaml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "research-api", Namespace: "research"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "api", Image: "registry.example/bifrost-research:0.205.0"}},
		}}},
	}
	svc := NewService(dir, func() (kubernetes.Interface, dynamic.Interface, error) {
		return fake.NewSimpleClientset(deploy), nil, nil
	})
	cells, err := svc.RunningImages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]ImageCell{}
	for _, c := range cells {
		by[c.Lane+"/"+c.Env] = c
	}
	stg := by["research/stg"]
	if !stg.Absent || stg.Text != "No STG" {
		t.Fatalf("research stg = %+v", stg)
	}
	prod := by["research/prod"]
	if prod.Text != "research-api 0.205.0" {
		t.Fatalf("research prod = %+v", prod)
	}
	agent := by["agent/prod"]
	if agent.Text != "primary abc1234" {
		t.Fatalf("agent prod = %+v", agent)
	}
}

func TestImageTag(t *testing.T) {
	if got := imageTag("registry.example/bifrost-research:0.205.0"); got != "0.205.0" {
		t.Fatalf("tag = %s", got)
	}
	if got := imageTag("registry.example/app@sha256:abcdef"); got != "sha256:abcdef" {
		t.Fatalf("digest = %s", got)
	}
}
