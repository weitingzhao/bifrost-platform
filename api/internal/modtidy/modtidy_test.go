package modtidy_test

import (
	"os/exec"
	"testing"
)

// Two branches can each be tidy and still merge into a module that is not:
// W-43b dropped golang.org/x/crypto while W-42 added an import of it, and the
// merge failed with a missing go.sum entry. CI runs go test ./..., so this
// keeps go.mod and go.sum equal to what go mod tidy would write.
func TestGoModIsTidy(t *testing.T) {
	cmd := exec.Command("go", "mod", "tidy", "-diff")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go.mod / go.sum are not tidy (fix: cd api && go mod tidy):\n%s", out)
	}
}
