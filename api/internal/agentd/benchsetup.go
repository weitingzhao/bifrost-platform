package agentd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// What a question's checks need, prepared before the executor runs: the
// trees' dependencies and the siblings (benchrun.go runs and judges).

// setup installs what the checks need in tree (dependencies from the
// tree's own manifests, which the judgement never takes from the executor)
// and returns the checks as they are run.
func (b *Bench) setup(ctx context.Context, tree, venv string, q BenchQuestion) ([]BenchCheck, error) {
	checks := append([]BenchCheck(nil), q.Checks...)
	for _, d := range []string{"tmp", "home"} {
		if err := os.MkdirAll(filepath.Join(tree, ".bench", d), 0o700); err != nil {
			return nil, err
		}
	}
	env := b.setupEnv(tree)
	seen := map[string]bool{}
	switch q.Language {
	case "ts":
		for _, c := range checks {
			if seen[c.Cwd] {
				continue
			}
			seen[c.Cwd] = true
			if _, err := os.Stat(filepath.Join(tree, c.Cwd, "package-lock.json")); err != nil {
				return nil, fmt.Errorf("no package-lock.json in %s", c.Cwd)
			}
			if err := b.npmInstall(ctx, filepath.Join(tree, c.Cwd), env); err != nil {
				return nil, err
			}
		}
	case "go":
		for _, c := range checks {
			dir := moduleRoot(tree, c.Cwd)
			if dir == "" || seen[dir] {
				continue
			}
			seen[dir] = true
			if why := b.run(ctx, dir, env, "go", "mod", "download"); why != "" {
				return nil, errors.New("go mod download: " + why)
			}
		}
	case "python", "shell":
		if _, err := os.Stat(filepath.Join(venv, "bin", "python3")); err != nil {
			if why := b.run(ctx, tree, env, "python3", "-m", "venv", venv); why != "" {
				return nil, errors.New("python3 -m venv: " + why)
			}
			if why := b.run(ctx, tree, env, filepath.Join(venv, "bin", "pip"), "install", "--quiet", "--disable-pip-version-check", "pytest"); why != "" {
				return nil, errors.New("pip install pytest: " + why)
			}
		}
		for i, c := range checks {
			if c.Argv[0] == "python3" || c.Argv[0] == "python" {
				// -I: the tree is not on the module path before pytest puts it
				// there, so a pytest.py of the change is not what runs (review
				// 288 B2).
				checks[i] = BenchCheck{Cwd: c.Cwd, Argv: append([]string{filepath.Join(venv, "bin", "python3"), "-I"}, c.Argv[1:]...)}
			}
		}
	}
	return checks, nil
}

// moduleRoot is the directory of the go.mod nearest cwd inside tree.
func moduleRoot(tree, cwd string) string {
	for d := filepath.Join(tree, cwd); strings.HasPrefix(d, tree); d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		if d == tree {
			break
		}
	}
	return ""
}

func (b *Bench) setupEnv(tree string) []string {
	env := append([]string{}, b.CheckEnv...)
	if b.GoModCache != "" {
		env = withEnv(env, "GOMODCACHE", b.GoModCache)
	}
	env = withEnv(env, "GOCACHE", filepath.Join(tree, ".bench", "go-build"))
	env = withEnv(env, "GOTOOLCHAIN", "local")
	return withEnv(env, "TMPDIR", filepath.Join(tree, ".bench", "tmp"))
}

// checkEnv is the setup's, offline: everything the checks need is in place.
func (b *Bench) checkEnv(tree string) []string {
	env := b.setupEnv(tree)
	env = withEnv(env, "GOFLAGS", "-mod=mod")
	env = withEnv(env, "GOPROXY", "off")
	return withEnv(env, "GONOSUMDB", "*")
}

// npmInstall installs a tree's locked dependencies without install scripts.
// A lockfile npm ci refuses as out of step with its package.json (some Trade
// frontend commits miss an entry) is resolved as Trade's CI installs then,
// once per question and manifest: every tree of the question with the same
// package.json and lockfile installs that one resolution, so the control
// before the fix and the judgement read the same dependencies (review 292
// B6). The resolution stays in agentd's memory, where no executor reaches
// it; any other npm ci failure is the setup's.
func (b *Bench) npmInstall(ctx context.Context, dir string, env []string) error {
	why, out := b.runTail(ctx, dir, env, "npm", "ci", "--ignore-scripts", "--no-audit", "--no-fund")
	if why == "" {
		return nil
	}
	if !bytes.Contains(out, []byte("EUSAGE")) {
		return errors.New("npm ci: " + why)
	}
	lockPath := filepath.Join(dir, "package-lock.json")
	manifest, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return err
	}
	lock, err := os.ReadFile(lockPath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(append(append(manifest, 0), lock...))
	key := hex.EncodeToString(sum[:])
	if err = forceRemove(filepath.Join(dir, "node_modules")); err != nil {
		return err
	}
	resolved, ok := b.locks[key]
	if !ok {
		if why2 := b.run(ctx, dir, env, "npm", "install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund"); why2 != "" {
			return errors.New("npm ci: " + why + "; resolving the lockfile: " + why2)
		}
		if resolved, err = os.ReadFile(lockPath); err != nil {
			return err
		}
		if b.locks == nil {
			b.locks = map[string][]byte{}
		}
		b.locks[key] = resolved
	}
	if err = os.Remove(lockPath); err != nil {
		return err
	}
	if err = os.WriteFile(lockPath, resolved, 0o600); err != nil {
		return err
	}
	if why2 := b.run(ctx, dir, env, "npm", "ci", "--ignore-scripts", "--no-audit", "--no-fund"); why2 != "" {
		return errors.New("npm ci with the lockfile resolved for this question: " + why2)
	}
	return nil
}

// sibling makes qdir/<repo> the sibling's tree at its commit, beside the
// question's trees, so a file:../<repo> dependency of a tree resolves to it,
// and builds it: its locked dependencies without install scripts (online, as
// a tree's), then its own build script in the sandbox, offline. Its clone is
// gone before anything of it runs. It stays, unchanged by the executor (whose
// writable roots are its own checkout and temporary directory), for the
// judgement's checks, which read it only.
func (b *Bench) sibling(ctx context.Context, qdir string, s BenchSibling) error {
	clone := filepath.Join(qdir, ".clone-"+s.Repo)
	src, err := Prepare(ctx, clone, b.Remote(s.Repo), s.Commit, b.GitEnv)
	if err != nil {
		return fmt.Errorf("cloning: %w", err)
	}
	dir := filepath.Join(qdir, s.Repo)
	if err = b.snapshot(ctx, src, s.Commit, dir); err != nil {
		return err
	}
	if err = forceRemove(clone); err != nil {
		return err
	}
	for _, d := range []string{"tmp", "home"} {
		if err = os.MkdirAll(filepath.Join(dir, ".bench", d), 0o700); err != nil {
			return err
		}
	}
	if _, err = os.Stat(filepath.Join(dir, "package-lock.json")); err != nil {
		return nil // nothing to install or build: read as it is
	}
	if why := b.run(ctx, dir, b.setupEnv(dir), "npm", "ci", "--ignore-scripts", "--no-audit", "--no-fund"); why != "" {
		return errors.New("npm ci: " + why)
	}
	build := []string{"/bin/sh", "-c", `cd "$1" && exec npm run --if-present build`, "sh", dir}
	if b.Sandbox != nil {
		build = b.Sandbox(dir, b.readOnly("", nil), build)
	}
	if why := b.run(ctx, dir, b.checkEnv(dir), build...); why != "" {
		return errors.New("npm run build: " + why)
	}
	return nil
}
