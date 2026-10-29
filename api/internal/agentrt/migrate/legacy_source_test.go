package migrate_test

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Tiny local history exercises the source contract without needing T50's real
// historical object, network credentials, a compiler subprocess or a database.
func TestLegacyAPISourceContract(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	must0010(t, os.MkdirAll(filepath.Join(repo, "api/internal"), 0o700))
	git := func(at string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = at
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(repo, "init", "-q")
	message := filepath.Join(dir, "message")
	must0010(t, os.WriteFile(message, []byte("test: fixture commit\n"), 0o600))
	commit := func(contents string) string {
		t.Helper()
		must0010(t, os.WriteFile(filepath.Join(repo, "api/internal/old.go"), []byte(contents), 0o600))
		git(repo, "add", "api/internal/old.go")
		git(repo, "-c", "user.name=T50 Test", "-c", "user.email=t50@example.invalid", "commit", "-q", "-F", message)
		return git(repo, "rev-parse", "HEAD")
	}
	baseline := commit("baseline\n")
	git(repo, "branch", "legacy")
	commit("current\n")
	git(repo, "branch", "candidate")
	clone := func(branch string) string {
		t.Helper()
		path := filepath.Join(dir, branch)
		git(dir, "clone", "-q", "--depth=1", "--no-local", "--branch", branch, "file://"+repo, path)
		if git(path, "rev-parse", "--is-shallow-repository") != "true" {
			t.Fatal("fixture must be shallow")
		}
		return path
	}
	legacy, candidate := clone("legacy"), clone("candidate")
	assertArchive := func(t *testing.T, source string) {
		t.Helper()
		archive := filepath.Join(t.TempDir(), "baseline.tar")
		must0010(t, archiveLegacyAPI(context.Background(), source, archive, baseline))
		file, err := os.Open(archive)
		must0010(t, err)
		defer file.Close()
		reader := tar.NewReader(file)
		files := map[string]string{}
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			must0010(t, err)
			if header.Typeflag == tar.TypeReg {
				content, err := io.ReadAll(reader)
				must0010(t, err)
				files[header.Name] = string(content)
			}
		}
		if len(files) != 1 || files["api/internal/old.go"] != "baseline\n" {
			t.Fatalf("archive is not the committed baseline: %v", files)
		}
	}
	t.Run("pinned shallow source ignores working files", func(t *testing.T) {
		t.Chdir(filepath.Join(candidate, "api"))
		must0010(t, os.WriteFile(filepath.Join(legacy, "api/internal/old.go"), []byte("dirty\n"), 0o600))
		must0010(t, os.WriteFile(filepath.Join(legacy, "api/injected.go"), []byte("untracked\n"), 0o600))
		assertArchive(t, filepath.Join(legacy, "api"))
	})
	t.Run("local archive fallback", func(t *testing.T) {
		t.Chdir(filepath.Join(repo, "api"))
		assertArchive(t, "")
	})
	for _, tc := range []struct{ name, source, want string }{
		{"wrong HEAD with baseline present", filepath.Join(repo, "api"), "HEAD is"},
		{"repository root is not api", legacy, "must name the checkout's api directory"},
		{"nested directory is not api", filepath.Join(legacy, "api/internal"), "must name the checkout's api directory"},
		{"missing directory", filepath.Join(dir, "absent"), "git rev-parse"},
		{"no Git metadata", dir, "not a git repository"},
		{"shallow fallback has no baseline", "", "not a tree object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENTRT_TEST_REQUIRED", "1")
			t.Chdir(filepath.Join(candidate, "api"))
			err := archiveLegacyAPI(context.Background(), tc.source, filepath.Join(t.TempDir(), "baseline.tar"), baseline)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}
