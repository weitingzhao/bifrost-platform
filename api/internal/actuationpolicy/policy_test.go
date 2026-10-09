package actuationpolicy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func loadReal(t *testing.T) *Policy {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "config", "actuation-policy.yaml")
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPolicyTiersFollowTheFile(t *testing.T) {
	p := loadReal(t)
	var sawB, sawC bool
	for ns, want := range p.Apply.Namespaces {
		got, err := p.ManifestTier([]string{ns}, false)
		if err != nil {
			t.Fatalf("namespace tier: %v", err)
		}
		if got != want {
			t.Fatalf("apply tier %s, file says %s", got, want)
		}
		if want == "B" {
			sawB = true
		}
		if want == "C" {
			sawC = true
		}
	}
	if !sawB || !sawC {
		t.Fatal("apply allow-list must contain both B and C namespaces")
	}
	got, err := p.ManifestTier([]string{firstKey(p.Apply.Namespaces)}, true)
	if err != nil || got != "X" {
		t.Fatalf("daemon deployment tier = %s %v, want X", got, err)
	}
}

func TestProbeAndJobDenies(t *testing.T) {
	p := loadReal(t)
	image := "registry.example/" + p.Probe.Images[0] + "1"
	if len(p.Jobs.Deny) == 0 || len(p.Probe.Deny) == 0 {
		t.Fatal("job and probe deny lists are empty")
	}
	for _, ns := range p.Jobs.Deny {
		if _, err := p.JobTier(ns); err == nil {
			t.Fatal("denied namespace was allowed to start a job")
		}
		if _, err := p.ProbeTier(ns, image, false); err == nil {
			t.Fatal("job-denied namespace was allowed to start a probe")
		}
	}
	var sawProbeC bool
	for ns, want := range p.Probe.Namespaces {
		got, err := p.ProbeTier(ns, image, false)
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		if got != want {
			t.Fatalf("probe tier %s, file says %s", got, want)
		}
		if want == "C" {
			sawProbeC = true
		}
	}
	if !sawProbeC {
		t.Fatal("probe allow-list has no C namespace")
	}
	if _, err := p.ProbeTier(firstKey(p.Probe.Namespaces), "library/not-listed:1", false); err == nil {
		t.Fatal("unlisted image was allowed")
	}
	outside := firstKey(p.Probe.Namespaces)
	for _, ns := range p.Probe.EnvFrom {
		if ns == outside {
			outside = ""
		}
	}
	if outside == "" {
		for ns := range p.Probe.Namespaces {
			if !strIn(p.Probe.EnvFrom, ns) {
				outside = ns
				break
			}
		}
	}
	if outside == "" {
		t.Fatal("every probe namespace allows env_from; the file should not")
	}
	if _, err := p.ProbeTier(outside, image, true); err == nil {
		t.Fatal("env_from was allowed outside its list")
	}
	for _, ns := range p.Probe.EnvFrom {
		if _, err := p.ProbeTier(ns, image, true); err != nil {
			t.Fatalf("env_from namespace rejected: %v", err)
		}
	}
}

func TestUnknownNamespaceRejected(t *testing.T) {
	p := loadReal(t)
	if _, err := p.JobTier("not-a-listed-namespace"); err == nil {
		t.Fatal("unknown namespace started a job")
	}
	if err := p.CleanupAllowed("not-a-listed-namespace"); err == nil {
		t.Fatal("unknown namespace was cleaned")
	}
	if err := p.RepoAllowed("not-a-repo", "k8s"); err == nil {
		t.Fatal("unknown repo was accepted")
	}
}

func TestGoSourcesDoNotNameAllowList(t *testing.T) {
	p := loadReal(t)
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Dir(file)
	sources := []string{
		filepath.Join(root, "policy.go"),
		filepath.Join(root, "..", "workactions", "service.go"),
		filepath.Join(root, "..", "workactions", "handler.go"),
		filepath.Join(root, "..", "actions", "actuation.go"),
	}
	var names []string
	for ns := range p.Apply.Namespaces {
		names = append(names, ns)
	}
	for _, repo := range p.Apply.Repos {
		names = append(names, repo.Name)
	}
	for _, image := range p.Probe.Images {
		names = append(names, image)
	}
	for _, path := range sources {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if name == "" {
				continue
			}
			if strings.Contains(string(body), `"`+name+`"`) {
				t.Errorf("%s quotes allow-list name %q", path, name)
			}
		}
	}
}

func firstKey(m map[string]string) string {
	for k := range m {
		return k
	}
	return ""
}

func strIn(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
