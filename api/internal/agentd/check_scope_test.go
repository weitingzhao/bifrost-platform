package agentd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestVerificationChangeScope(t *testing.T) {
	t.Parallel()
	defaults := store.Repos["bifrost-platform"].Verify
	custom := []string{"go", "-C", "api", "test", "./internal/example"}
	commands := append(append([][]string{}, defaults...), custom)
	for _, tc := range []struct {
		name    string
		paths   []string
		indexes []int
	}{
		{"Go", []string{"api/internal/example/example.go"}, []int{0, 1, 2, 3, 7}},
		{"Console", []string{"console/src/page.tsx"}, []int{2, 4, 5, 6, 7}},
		{"mixed", []string{"api/example.go", "console/src/page.tsx"}, nil},
		{"module", []string{"api/go.mod"}, nil},
		{"build", []string{"api/Makefile"}, nil},
		{"shared", []string{"config/shared.yaml"}, nil},
		{"empty", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := commands
			if tc.indexes != nil {
				want = nil
				for _, i := range tc.indexes {
					want = append(want, commands[i])
				}
			}
			if got := selectVerification(commands, tc.paths); !reflect.DeepEqual(got, want) {
				t.Fatalf("commands = %v; want %v", got, want)
			}
		})
	}
}

func TestVerificationKeepsRepository(t *testing.T) {
	t.Parallel()
	a := &Attempt{Spec: TaskSpec{Check: "verify", Repo: "bifrost-platform"}, Subject: &TaskSpec{Text: "work"}, Dir: t.TempDir(), Out: t.TempDir()}
	if _, err := deterministicExecutor(os.Args[0], os.Environ())(a); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(a.Out, "verify-spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec TaskSpec
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Repo != "bifrost-platform" {
		t.Fatalf("repository = %q", spec.Repo)
	}
}
