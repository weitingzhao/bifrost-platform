package devsession

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSessionsCatalog(t *testing.T) {
	dir := t.TempDir()
	raw := `
version: "1"
envs:
  stg:
    - name: platform
      label: Platform API
      group: platform
      namespace: bifrost-platform-stg
      deployment: platform-api
    - name: api-monitor
      label: Monitor API
      group: api
      namespace: bifrost-stg
`
	if err := os.WriteFile(filepath.Join(dir, sessionsCatalogFile), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadSessionsCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	entries := cat.EntriesForEnv("stg")
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Deployment != "platform-api" {
		t.Fatalf("platform deployment=%q", entries[0].Deployment)
	}
	if entries[1].Deployment != "api-monitor" {
		t.Fatalf("default deployment=%q want api-monitor", entries[1].Deployment)
	}
	got := cat.Lookup("stg", "platform")
	if got == nil || got.Namespace != "bifrost-platform-stg" {
		t.Fatalf("lookup platform: %+v", got)
	}
}

func TestLoadSessionsCatalog_MissingFile(t *testing.T) {
	cat, err := LoadSessionsCatalog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.EntriesForEnv("stg")) != 0 {
		t.Fatalf("expected empty catalog")
	}
}

func TestDiscoveryNamespacesForEnv(t *testing.T) {
	dir := t.TempDir()
	raw := `
version: "1"
discovery:
  enabled: true
  namespaces:
    stg:
      - bifrost-stg
      - data
envs:
  stg:
    - name: api-monitor
      namespace: bifrost-stg
`
	if err := os.WriteFile(filepath.Join(dir, sessionsCatalogFile), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadSessionsCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	nss := cat.DiscoveryNamespacesForEnv("stg")
	if len(nss) != 2 || nss[0] != "bifrost-stg" || nss[1] != "data" {
		t.Fatalf("nss=%v", nss)
	}
	cat.Discovery.Enabled = false
	if len(cat.DiscoveryNamespacesForEnv("stg")) != 0 {
		t.Fatal("disabled discovery should return nil")
	}
}

func TestEntryFromAnnotatedDeployment(t *testing.T) {
	e := entryFromAnnotatedDeployment("ns", "my-deploy", map[string]string{
		AnnotationSession:      "true",
		AnnotationSessionName:  "custom",
		AnnotationSessionLabel: "Custom Label",
		AnnotationSessionGroup: "plugins",
	}, []int{8080})
	if e.Name != "custom" || e.Label != "Custom Label" || e.Group != "plugins" || e.Deployment != "my-deploy" {
		t.Fatalf("%+v", e)
	}
}
