package unitplan

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// ManifestPath is where a repository says what its units are; a repository
// without one is one unit, tested as a whole (the first wave's rows).
const ManifestPath = "bifrost.units.yaml"

// Manifest is bifrost.units.yaml: the repository's modules, each a kind of
// unit with its tasks, and the other repositories it uses, pinned to a commit.
type Manifest struct {
	Version int            `json:"version"`
	Pins    map[string]Pin `json:"pins,omitempty"`
	Modules []Module       `json:"modules"`
}

// Pin is another repository at the commit this one is tested with. Advance
// says who moves it (manual: whoever changes the file).
type Pin struct {
	Commit  string `json:"commit"`
	Advance string `json:"advance,omitempty"`
}

// Module is one part of the repository. Kind go is a Go module: its vet task
// is one unit for the module and its test task one unit a package with
// tests. Any other kind (node, or none) is one unit: the directory.
type Module struct {
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`
	Root     string `json:"root"`
	Image    string `json:"image,omitempty"`
	Template string `json:"template,omitempty"`
	DBRoles  string `json:"db-roles,omitempty"`
	// Needs are inputs of every task of the module, beside its own tree.
	Needs []string        `json:"needs,omitempty"`
	Tasks map[string]Task `json:"tasks"`
	// Shards is how many parts a package's test task runs in, by package
	// directory (kind go).
	Shards map[string]int `json:"shards,omitempty"`
	// Inputs are what a package's tests read outside the package, by package
	// directory (kind go): the inputs the import graph cannot see.
	Inputs map[string][]string `json:"inputs,omitempty"`
}

// Task is one task of a module's units.
type Task struct {
	Per    string   `json:"per,omitempty"` // kind go: module or package
	Shards int      `json:"shards,omitempty"`
	Needs  []string `json:"needs,omitempty"`
}

var (
	taskName = regexp.MustCompile(`^(vet|test|typecheck|lint|build|check)$`)
	unitName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`) // 0017's CHECK on evidence.unit
	fullID   = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// ParseManifest reads and checks a manifest: a field it does not know, a pin
// that is not a commit, or an input that names a repository with no pin is an
// error, not a default.
func ParseManifest(doc []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.UnmarshalStrict(doc, &m); err != nil {
		return nil, fmt.Errorf("unitplan: %s: %w", ManifestPath, err)
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("unitplan: %s: version %d, want 1", ManifestPath, m.Version)
	}
	for repo, pin := range m.Pins {
		if !fullID.MatchString(pin.Commit) {
			return nil, fmt.Errorf("unitplan: %s: pin %s is not a 40-character commit", ManifestPath, repo)
		}
	}
	names := map[string]bool{}
	for _, mod := range m.Modules {
		if mod.Name == "" || names[mod.Name] || mod.Root == "" || len(mod.Tasks) == 0 {
			return nil, fmt.Errorf("unitplan: %s: module %q needs a name of its own, a root and a task", ManifestPath, mod.Name)
		}
		names[mod.Name] = true
		refs := append([]string{}, mod.Needs...)
		for name, task := range mod.Tasks {
			if !taskName.MatchString(name) {
				return nil, fmt.Errorf("unitplan: %s: module %s: %q is not a task name", ManifestPath, mod.Name, name)
			}
			if mod.Kind == "go" && task.Per != "module" && task.Per != "package" {
				return nil, fmt.Errorf("unitplan: %s: module %s task %s: per is module or package", ManifestPath, mod.Name, name)
			}
			refs = append(refs, task.Needs...)
		}
		for _, list := range mod.Inputs {
			refs = append(refs, list...)
		}
		for _, ref := range refs {
			if repo, _, other := strings.Cut(ref, ":"); other {
				if _, pinned := m.Pins[repo]; !pinned {
					return nil, fmt.Errorf("unitplan: %s: module %s uses %q, and %s has no pin", ManifestPath, mod.Name, ref, repo)
				}
			}
		}
	}
	return &m, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
