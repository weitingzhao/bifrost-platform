package registry

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Watches are the cluster objects gate-cluster watches and says on the bus
// when their state changes (ISSUE-34). One row is one kind of object: which
// resource, which namespaces, which fields make its state, and the event it
// is said as. Adding a module's state to the bus is a row here and its
// topic's entry (with its body) in <tenant>.yaml; no Go.

//go:embed watches/*.yaml
var watchFiles embed.FS

// Watch is one row.
// Gone is the value of every state field of an object that left the cluster:
// the watch writes it when the object is deleted, or when a full list no
// longer holds an object the ledger still says is there, and the state
// readout (GET /api/v1/bus/states) leaves such subjects out. Origin:
// 2026-11-21, the metrics line counted the readout's workloads against the
// cluster: 392 objects for 82 real Deployments and StatefulSets. The watch
// dropped deletions, so every Tekton affinity-assistant StatefulSet (one per
// run, deleted when the run ends) stayed "there" for the 30 days the readout
// looks back.
const Gone = "gone"

type Watch struct {
	ID       string `yaml:"id"`
	Group    string `yaml:"group"`
	Version  string `yaml:"version"`
	Resource string `yaml:"resource"`
	// Namespaces to watch; empty watches every namespace (or a cluster-wide
	// resource such as nodes).
	Namespaces []string `yaml:"namespaces"`
	// Type is the registered topic; Stream the ledger stream's prefix
	// (stream = <stream>:<subject>).
	Type   string `yaml:"type"`
	Stream string `yaml:"stream"`
	// Subject is made of the object: a template of field paths in braces,
	// e.g. "{metadata.namespace}/{metadata.name}". SubjectField is the body
	// field it is also written to (the topic's subject_from).
	Subject      string `yaml:"subject"`
	SubjectField string `yaml:"subject_field"`
	// State is the fields that make the object's state, in the order of the
	// topic's change_from.
	State []WatchState `yaml:"state"`
}

// WatchState is one state field: where it is read (a dot path; a list is
// picked by [key=value]) and how a raw value becomes the said value (map;
// "*" is the default, also for a missing field; without a map the raw value
// is said as it is, "" when missing). With compare_to, the raw value is
// "true" when the field at path equals the field at compare_to, else
// "false"; a missing field at path counts as 0 (a count the API leaves out),
// a missing compare_to is never equal. A workload is ready when its ready
// replicas equal its replicas, which no single field says.
type WatchState struct {
	Name      string            `yaml:"name"`
	Path      string            `yaml:"path"`
	CompareTo string            `yaml:"compare_to"`
	Map       map[string]string `yaml:"map"`
}

type watchFile struct {
	Tenant  string  `yaml:"tenant"`
	Watches []Watch `yaml:"watches"`
}

var (
	watchIDRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	// A segment may pick a list item by [key=value] or by position, [0]
	// (2026-11-21: a workload's version is its first container's image).
	pathRE  = regexp.MustCompile(`^[a-zA-Z0-9_]+(\[([a-zA-Z0-9_]+=[^\]]+|[0-9]+)\])?(\.[a-zA-Z0-9_]+(\[([a-zA-Z0-9_]+=[^\]]+|[0-9]+)\])?)*$`)
	braceRE = regexp.MustCompile(`\{([^}]*)\}`)
)

// Watches reads every tenant's watch rows and checks each against that
// tenant's register: the topic exists, its body takes the subject field and
// every state field, and its change_from is the state in order.
func Watches() (map[string][]Watch, error) {
	tenants, err := Load()
	if err != nil {
		return nil, err
	}
	wSrc := source(watchFiles, "watches")
	entries, err := fs.ReadDir(wSrc, "watches")
	if err != nil {
		return nil, err
	}
	out := map[string][]Watch{}
	var errs []string
	for _, e := range entries {
		data, err := fs.ReadFile(wSrc, "watches/"+e.Name())
		if err != nil {
			return nil, err
		}
		ws, err := ParseWatches(e.Name(), data, tenants)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		out[strings.TrimSuffix(e.Name(), ".yaml")] = ws
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("bus watches: %s", strings.Join(errs, "\n"))
	}
	return out, nil
}

// ParseWatches reads one tenant's watch file.
func ParseWatches(name string, data []byte, tenants map[string]Tenant) ([]Watch, error) {
	var f watchFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	var errs []string
	bad := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }
	if want := f.Tenant + ".yaml"; path.Base(name) != want {
		bad("file %s holds tenant %q; name it %s", name, f.Tenant, want)
	}
	reg := tenants[f.Tenant]
	ids := map[string]bool{}
	for _, w := range f.Watches {
		where := func(format string, args ...any) { bad("%s: "+format, append([]any{w.ID}, args...)...) }
		if !watchIDRE.MatchString(w.ID) || ids[w.ID] {
			where("an id is a unique lower-case name")
		}
		ids[w.ID] = true
		if w.Version == "" || w.Resource == "" {
			where("version and resource are needed")
		}
		if !watchIDRE.MatchString(w.Stream) {
			where("stream %q is not a lower-case name", w.Stream)
		}
		c, ok := reg.Channels[w.Type]
		if !ok {
			where("type %s is not registered for tenant %s", w.Type, f.Tenant)
			continue
		}
		body := map[string]bool{}
		for _, b := range c.Body {
			body[b.Name] = true
		}
		if len(body) == 0 {
			where("%s has no body in the register: a watched topic's body is written there", w.Type)
		}
		if w.SubjectField == "" || c.SubjectFrom != w.SubjectField {
			where("subject_field %q is not %s's subject_from %q", w.SubjectField, w.Type, c.SubjectFrom)
		}
		if !strings.Contains(w.Subject, "{") {
			where("subject %q names no field", w.Subject)
		}
		for _, m := range braceRE.FindAllStringSubmatch(w.Subject, -1) {
			if !pathRE.MatchString(m[1]) {
				where("subject path %q is not a field path", m[1])
			}
		}
		names := make([]string, 0, len(w.State))
		for _, s := range w.State {
			names = append(names, s.Name)
			if !body[s.Name] {
				where("state %q is not a field of %s's body", s.Name, w.Type)
			}
			if !pathRE.MatchString(s.Path) {
				where("state %q: path %q is not a field path", s.Name, s.Path)
			}
			if s.CompareTo != "" && !pathRE.MatchString(s.CompareTo) {
				where("state %q: compare_to %q is not a field path", s.Name, s.CompareTo)
			}
		}
		if len(names) == 0 {
			where("no state: a watch says a change of state")
		}
		if !slices.Equal(names, c.ChangeFrom) {
			where("state %v is not %s's change_from %v", names, w.Type, c.ChangeFrom)
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return nil, fmt.Errorf("%s:\n  %s", name, strings.Join(errs, "\n  "))
	}
	return f.Watches, nil
}
