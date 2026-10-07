package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAppEnvsKeepFileOrderAndSkipUndeclared(t *testing.T) {
	cfg := &Config{Environments: []Environment{
		{ID: "local"},
		{ID: "qa", Namespace: "shop-qa", Database: "shop_qa"},
		{ID: "qa-again", Namespace: "shop-qa"},
		{ID: "live", Namespace: " shop-live ", Database: "shop_live"},
	}}
	got := cfg.AppEnvs()
	want := []AppEnv{
		{ID: "qa", Namespace: "shop-qa", Database: "shop_qa"},
		{ID: "live", Namespace: "shop-live", Database: "shop_live"},
	}
	if len(got) != len(want) {
		t.Fatalf("AppEnvs = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("AppEnvs[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// The cluster probes, the cutover check and the database list read namespaces
// only from environments.yaml (TD-231); an environment that runs in the cluster
// but declares no namespace silently drops out of all of them.
func TestShippedEnvironmentsDeclareNamespaces(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "config", "environments.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := yaml.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	for _, e := range f.Environments {
		if e.ID == "dev-local" {
			continue
		}
		if e.Namespace == "" || e.Database == "" {
			t.Errorf("environment %s has namespace %q database %q; both are required", e.ID, e.Namespace, e.Database)
		}
	}
}
