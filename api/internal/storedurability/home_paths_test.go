// Package storedurability holds repo-wide checks on where platform-api keeps
// its state (ratchet platform-store-durability, TD-229).
package storedurability

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var homeDerived = regexp.MustCompile(`os\.Getenv\("HOME"\)|os\.UserHomeDir\(\)`)

// homePathFiles are the non-test files that still derive a path from $HOME,
// each with why. In the cluster $HOME is outside every mounted volume, so a
// store resolved there loses what it holds on every restart and nobody sees it
// go (the Owner's 09-07 trust grant). This list may only shrink: a new entry
// needs a reason that is not a store, and a file that stops matching must be
// removed from it.
var homePathFiles = map[string]string{
	// stores still resolving under $HOME when no env is set — TD-196 backlog
	"agentdeploy/store.go":    "store; TD-196 moves it to a ConfigMap",
	"cluster/data_clone.go":   "store; TD-196 moves it to a ConfigMap",
	"codehealth/store.go":     "store; TD-196 moves it to a ConfigMap",
	"patrol/store.go":             "store; TD-196 moves it to a ConfigMap",
	"agentgovernance/outcome.go":  "skill-outcome archive; same $HOME fallback the retired runner job store used",
	// operator credentials and workstation files, not platform state
	"cluster/metrics_server.go":        "default kubeconfig path",
	"cluster/observability_install.go": "default kubeconfig path",
	"cluster/pod_exec.go":              "default kubeconfig path",
	"cluster/script_runner.go":         "default kubeconfig path",
	"cluster/sync.go":                  "kubeconfig the sync script writes on the workstation",
	"config/clusters.go":               "default kubeconfig path",
	"console/ssh_ws.go":                "ssh keys and known_hosts for the operator console",
	"devsession/provider_bdev.go":      "~/.bifrost-dev of the bdev workstation",
	"ibgateway/config.go":              "default kubeconfig path",
	"launchd/list.go":                  "reads the operator-plane host LaunchAgents directory; not platform state",
	"threadtitles/threadtitles.go":     "Claude transcripts on the workstation",
}

func TestNoNewStoreUnderHome(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if homeDerived.Match(src) {
			rel, _ := filepath.Rel(root, path)
			found[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("found no file at all — the walk is not seeing api/internal")
	}
	var added, gone []string
	for f := range found {
		if _, ok := homePathFiles[f]; !ok {
			added = append(added, f)
		}
	}
	for f := range homePathFiles {
		if !found[f] {
			gone = append(gone, f)
		}
	}
	sort.Strings(added)
	sort.Strings(gone)
	for _, f := range added {
		t.Errorf("%s derives a path from $HOME: keep platform state in a ConfigMap (internal/threadtitles pattern), or list it with a reason that is not a store", f)
	}
	for _, f := range gone {
		t.Errorf("%s no longer derives a path from $HOME: remove it from homePathFiles", f)
	}
}
