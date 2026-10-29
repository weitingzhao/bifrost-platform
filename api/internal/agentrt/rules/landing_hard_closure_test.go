package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const modulePath = "github.com/weitingzhao/bifrost-platform/api/"

type listedPackage struct {
	ImportPath string
	Imports    []string
	DepOnly    bool
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
}
func listDependencies(t *testing.T, dir string, args ...string) []listedPackage {
	t.Helper()
	cmd := exec.Command("go", append([]string{"list", "-deps", "-json"}, args...)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v: %s", err, stderr.String())
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var out []listedPackage
	for {
		var p listedPackage
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// §4.2 excludes mounted business features, not injected trust providers.
// Each retained edge was checked at its call sites (686 B-02). Reasons name
// the consumer so this list cannot silently stand for every server import.
// These exclusions do not prove isolation from arbitrary same-process code.
var assemblyEdges = map[string]map[string]string{
	"internal/server": {
		"agentgovernance": "server.go: trust matrix/override display and skill-run recording; not AuthService or action tier input",
		"agentprogress":   "agent_progress_wire.go: progress recorder and reporting routes",
		"agentthreads":    "agent_threads_wire.go: thread liveness, metrics and operator notices",
		"checklist":       "server.go: checklist signals and background prober; no landing/approval predicate",
		"codehealth":      "server.go: code-health reporting/rescan routes; not the landing CI reader",
		"datahusbandry":   "server.go: data status projection and checklist source",
		"devsession":      "server.go: local dev-session list/log/control routes",
		"flexquery":       "server.go, metrics.go: plugin proxy and health metric",
		"gitops":          "actions_wire.go: guarded Argo action handler; not an approval or landing evidence provider",
		"ibgateway":       "actions_wire.go, metrics.go: guarded plugin handlers and health metric",
		"marketdata":      "actions_wire.go, metrics.go: plugin proxy/action handlers and health metric",
		"mcp":             "server.go: tool catalog/status routes; not the credential-bearing mcp/platform bridge",
		"network":         "actions_wire.go: guarded network action handlers and read routes",
		"operatorplane":   "server.go: mount/proxy L-1 business routes using the supplied AuthService",
		"opsagent":        "server.go: Alertmanager intake route",
		"opsfacts":        "ops_facts.go: read-only System projection, not an authorization input",
		"patrol":          "server.go: patrol business routes/background loop; not the agentrt controller",
		"promote":         "server.go: context display overlay; not release-policy or landing input",
		"research":        "server.go, metrics.go: research proxy/status and health metric",
		"satellite":       "server.go: satellite bus/matrix read routes",
		"selfhealth":      "server.go: platform self-health read route",
		"telemetry":       "server.go, ops_facts.go: metrics/alerts projection and attention-mute route",
		"threadtitles":    "server.go, agent_threads_wire.go: display titles and transcript sync",
		"topology":        "server.go: topology read projection",
		"tradeagent":      "server.go: Trade domain/catalog read routes",
	},
	"cmd/operator-plane": {
		"alertrelay":    "main.go: out-of-band alert delivery and heartbeat service",
		"operatorplane": "main.go: L-1 business route assembly using protected config/actuation",
	},
}

func assemblyEdge(from, to string) bool {
	return assemblyEdges[strings.TrimPrefix(from, modulePath)][strings.TrimPrefix(to, modulePath+"internal/")] != ""
}
func uncoveredDependencies(pkgs []listedPackage, prefix string, protected func(string) bool, exempt func(string, string) bool) []string {
	graph := map[string]listedPackage{}
	var queue []string
	for _, p := range pkgs {
		graph[p.ImportPath] = p
		if !p.DepOnly {
			queue = append(queue, p.ImportPath)
		}
	}
	seen := map[string]bool{}
	var failures []string
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		if !protected(name) {
			failures = append(failures, "root: "+name)
		}
		for _, dep := range graph[name].Imports {
			if !strings.HasPrefix(dep, prefix) {
				continue
			}
			if exempt(name, dep) {
				continue
			}
			if !protected(dep) {
				failures = append(failures, name+" -> "+dep)
				continue
			}
			queue = append(queue, dep)
		}
	}
	return failures
}
func TestLandingHardDependencyClosure(t *testing.T) {
	t.Parallel()
	root := repositoryRoot(t)
	var args []string
	for _, dir := range landingHardRoots {
		args = append(args, "./"+strings.TrimPrefix(dir, "api/")+"...")
	}
	pkgs := listDependencies(t, filepath.Join(root, "api"), args...)
	for from, deps := range assemblyEdges {
		for dep, reason := range deps {
			found := false
			for _, p := range pkgs {
				if p.ImportPath == modulePath+from && slices.Contains(p.Imports, modulePath+"internal/"+dep) {
					found = true
				}
			}
			if !found || strings.TrimSpace(reason) == "" || inLandingEntries("api/internal/"+dep+"/", landingHardGo[:]) {
				t.Errorf("stale, unexplained or protected assembly exemption: %s -> %s", from, dep)
			}
		}
	}
	missing := uncoveredDependencies(pkgs, modulePath, func(pkg string) bool {
		return inLandingEntries("api/"+strings.TrimPrefix(pkg, modulePath)+"/", landingHardGo[:])
	}, assemblyEdge)
	if len(missing) != 0 {
		t.Fatalf("hard dependency closure missing:\n%s", strings.Join(missing, "\n"))
	}
}
func TestLandingHardCatalogEntriesExist(t *testing.T) {
	t.Parallel()
	root := repositoryRoot(t)
	for _, entries := range [][]string{landingHardGo[:], landingHardConsole[:], landingHardOtherDirs[:], landingHardFiles[:]} {
		for _, p := range entries {
			if slices.Contains(landingHardFuture[:], p) {
				continue
			}
			info, err := os.Stat(filepath.Join(root, p))
			if err != nil {
				t.Errorf("stale hard entry %s: %v", p, err)
				continue
			}
			if strings.HasSuffix(p, "/") != info.IsDir() {
				t.Errorf("hard entry kind changed: %s", p)
			}
		}
	}
}

// T19 uses a real temporary Go module. Adding a non-test dependency to the
// protected root makes the same go-list closure checker reject it.
func TestLandingHardClosureDetectsNewDependency(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(p, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/closure\n\ngo 1.25.0\n")
	write("root/root.go", "package root\n")
	check := func() []string {
		return uncoveredDependencies(listDependencies(t, dir, "./root"), "example.test/closure/", func(p string) bool { return p == "example.test/closure/root" }, func(string, string) bool { return false })
	}
	if got := check(); len(got) != 0 {
		t.Fatal("control", got)
	}
	write("dep/dep.go", "package dep\n")
	write("root/root.go", "package root\nimport _ \"example.test/closure/dep\"\n")
	if got := check(); len(got) != 1 || !strings.Contains(fmt.Sprint(got), "/dep") {
		t.Fatal("new dependency not caught", got)
	}
}
