package landingci

import (
	"strings"
	"testing"
)

// TD-467: a node pulls the checker with containerd, outside cluster DNS, so
// the trust root names the registry by its NodePort, as every platform image
// does, never by its cluster Service name (ErrImagePull on the first ci-infra
// run). The embedded contract and each Task step pull from that repository.
func TestS5ImageRepositoryIsNodeReachable(t *testing.T) {
	host, _, ok := strings.Cut(s5ImageRepository, "/")
	if !ok || strings.HasSuffix(host, ".svc") || strings.Contains(host, ".svc.cluster.local") {
		t.Fatalf("S5 checker image host is not node-reachable: %q", host)
	}
	c, err := loadS5Contract()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(c.Image, s5ImageRepository) {
		t.Fatalf("embedded contract image %q is not under %q", c.Image, s5ImageRepository)
	}
	for name, task := range c.Tasks {
		steps, _ := task["steps"].([]any)
		if len(steps) != 1 {
			t.Fatalf("task %s: %d steps", name, len(steps))
		}
		if step, _ := steps[0].(map[string]any); step["image"] != c.Image {
			t.Fatalf("task %s step image %v is not the contract image %s", name, step["image"], c.Image)
		}
	}
}
