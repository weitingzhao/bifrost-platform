// Package landinggit owns the isolated Git subprocesses for the STG drill.
// It never executes candidate code. CUT §2.1, §2.4, §2.5 and §2.11.
package landinggit

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const remoteURL = "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-platform.git"
const UsernameFileEnv = "AGENTRT_LANDING_USERNAME_FILE"
const PasswordFileEnv = "AGENTRT_LANDING_PASSWORD_FILE"

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var drillRef = regexp.MustCompile(`^refs/heads/land/stg/t_[0-9A-HJKMNP-TV-Z]{26}/[1-9][0-9]*$`)

// liveRef is the production landing branch (S3, CUT §2.5 step 4): only the
// lander identity pushes it.
var liveRef = regexp.MustCompile(`^refs/heads/land/prod/t_[0-9A-HJKMNP-TV-Z]{26}/[1-9][0-9]*$`)

// Git has no configurable production remote and no general-purpose push API.
// The credentials are file paths, never token values in the parent's env.
type Git struct {
	repo, remote, usernameFile, passwordFile string
	// scope is the only branch shape this identity reads, pushes and
	// deletes: land/stg for the drill, land/prod for the lander.
	scope *regexp.Regexp
	// lander: the production identity, the only one that may push main
	// (PushMain). The drill's Git never can.
	lander bool
}

func FromEnv(environment string, repos ...string) (*Git, error) {
	if environment != "stg" {
		return nil, errors.New("landing drill requires stg")
	}
	u, p := os.Getenv(UsernameFileEnv), os.Getenv(PasswordFileEnv)
	if !filepath.IsAbs(u) || !filepath.IsAbs(p) {
		return nil, errors.New("landing credential paths must be absolute")
	}
	repo, err := selectedRepo(repos)
	if err != nil {
		return nil, err
	}
	return &Git{repo: repo.Name, remote: repo.Remote, usernameFile: u, passwordFile: p, scope: drillRef}, nil
}

// Lander credentials: the agentrt-lander identity's basic-auth files (S3).
const LanderUsernameFileEnv = "AGENTRT_LANDER_USERNAME_FILE"
const LanderPasswordFileEnv = "AGENTRT_LANDER_PASSWORD_FILE"

// LanderFromEnv is the production landing identity: land/prod branches and
// a fast-forward push of platform main, nothing else (CUT §2.5, §3 S3 ⑫).
func LanderFromEnv(environment string, repos ...string) (*Git, error) {
	if environment != "prod" {
		return nil, errors.New("the lander runs on prod")
	}
	u, p := os.Getenv(LanderUsernameFileEnv), os.Getenv(LanderPasswordFileEnv)
	if !filepath.IsAbs(u) || !filepath.IsAbs(p) {
		return nil, errors.New("lander credential paths must be absolute")
	}
	repo, err := selectedRepo(repos)
	if err != nil {
		return nil, err
	}
	return &Git{repo: repo.Name, remote: repo.Remote, usernameFile: u, passwordFile: p, scope: liveRef, lander: true}, nil
}

// ReaderDirEnv names the platform's Gitea reader mount (TD-432): a
// kubernetes.io/basic-auth Secret (files username and password) of an
// identity that reads the organization's repositories and writes nothing.
const ReaderDirEnv = "GITEA_READER_DIR"

// noRef matches no ref at all: a reader pushes, deletes and reads no
// land/** branch; it reads main and fetches commits by SHA.
var noRef = regexp.MustCompile(`$^`)

// ReaderFromEnv is a read-only Git for the landing grant's material, its
// create check and its executor, which run in platform-api (Owner 2026-10-29
// chose this over mounting the lander, the one identity that can push
// platform main, in the process that serves HTTP). It reads main and fetches
// the A, M0 and R commits; every push, main push and delete refuses.
func ReaderFromEnv(repos ...string) (*Git, error) {
	dir := strings.TrimSpace(os.Getenv(ReaderDirEnv))
	if !filepath.IsAbs(dir) {
		return nil, errors.New("the Gitea reader directory must be an absolute path")
	}
	repo, err := selectedRepo(repos)
	if err != nil {
		return nil, err
	}
	return &Git{repo: repo.Name, remote: repo.Remote, usernameFile: filepath.Join(dir, "username"), passwordFile: filepath.Join(dir, "password"), scope: noRef}, nil
}

func (g *Git) inScope(ref string) bool {
	scope := g.scope
	if scope == nil {
		scope = drillRef
	}
	return scope.MatchString(ref)
}

// A strict allowlist prevents inherited Git config, tracing, helpers, proxies,
// askpass, attributes and alternate object paths from entering a subprocess.
func isolatedEnv() []string {
	return []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C", "HOME=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=http", "GIT_LITERAL_PATHSPECS=1"}
}

var isolation = []string{"-c", "core.hooksPath=/dev/null", "-c", "core.attributesFile=/dev/null",
	"-c", "credential.helper=", "-c", "http.followRedirects=false", "-c", "protocol.allow=never",
	"-c", "protocol.http.allow=always", "-c", "submodule.recurse=false", "-c", "commit.gpgSign=false",
	"-c", "tag.gpgSign=false", "-c", "core.autocrlf=false"}

func (g *Git) header() (string, error) {
	u, e := os.ReadFile(g.usernameFile)
	if e != nil {
		return "", errors.New("cannot read landing username file")
	}
	p, e := os.ReadFile(g.passwordFile)
	if e != nil {
		return "", errors.New("cannot read landing password file")
	}
	// Basic-auth Secret values are exact bytes; trailing newlines are not tokens.
	if len(u) == 0 || len(p) == 0 || bytes.ContainsAny(u, ":\r\n") || bytes.ContainsAny(p, "\r\n") {
		return "", errors.New("invalid landing basic-auth files")
	}
	return "Authorization: Basic " + base64.StdEncoding.EncodeToString(append(append(u, ':'), p...)), nil
}

func (g *Git) run(ctx context.Context, dir string, network bool, input []byte, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.Command("git", append(append([]string{}, isolation...), args...)...)
	cmd.Dir = dir
	cmd.Env = append(isolatedEnv(), env...)
	cmd.Stdin = bytes.NewReader(input)
	if network {
		h, e := g.header()
		if e != nil {
			return "", e
		}
		cmd.Env = append(cmd.Env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0="+h)
	}
	// Output is bounded (64 MiB: a history walk, never a remote's say);
	// stderr is never kept: servers and Git can echo credentials or source.
	out := &limitedBuffer{max: 64 << 20}
	cmd.Stdout = out
	if e := runGroup(ctx, cmd); e != nil || len(out.b) == out.max {
		return "", fmt.Errorf("landing git %s failed", args[0])
	}
	return out.String(), nil
}

type Workspace struct {
	git *Git
	dir string
}

func (w *Workspace) Close() error { return os.RemoveAll(w.dir) }

func (g *Git) Open(ctx context.Context, commit string) (w *Workspace, err error) {
	if !fullSHA.MatchString(commit) {
		return nil, errors.New("invalid result SHA")
	}
	dir, err := os.MkdirTemp("", "agentrt-landing-")
	if err != nil {
		return nil, err
	}
	w = &Workspace{git: g, dir: dir}
	owned := w
	defer func() {
		if err != nil {
			_ = owned.Close()
		}
	}()
	if _, err = g.run(ctx, dir, false, nil, nil, "init", "--template=", "."); err != nil {
		return nil, err
	}
	if _, err = g.run(ctx, dir, false, nil, nil, "remote", "add", "origin", g.remote); err != nil {
		return nil, err
	}
	if _, err = g.run(ctx, dir, true, nil, nil, "fetch", "--no-tags", "--no-recurse-submodules", "origin", "+refs/heads/main:refs/remotes/origin/main", commit); err != nil {
		return nil, err
	}
	return w, nil
}
func (w *Workspace) local(ctx context.Context, args ...string) (string, error) {
	return w.git.run(ctx, w.dir, false, nil, nil, args...)
}
func (w *Workspace) Main(ctx context.Context) (string, error) {
	s, e := w.local(ctx, "rev-parse", "refs/remotes/origin/main")
	return strings.TrimSpace(s), e
}
func (g *Git) Ref(ctx context.Context, ref string) (string, error) {
	if ref != "refs/heads/main" && !g.inScope(ref) {
		return "", errors.New("ref outside landing scope")
	}
	s, e := g.run(ctx, "", true, nil, nil, "ls-remote", "--refs", g.remote, ref)
	if e != nil {
		return "", e
	}
	f := strings.Fields(s)
	if len(f) == 0 {
		return "", nil
	}
	if len(f) != 2 || !fullSHA.MatchString(f[0]) || f[1] != ref {
		return "", errors.New("invalid remote ref response")
	}
	return f[0], nil
}
func (w *Workspace) Push(ctx context.Context, ref, sha string) error {
	if !w.git.inScope(ref) || !fullSHA.MatchString(sha) {
		return errors.New("push outside landing scope")
	}
	_, e := w.git.run(ctx, w.dir, true, nil, nil, "push", "--porcelain", "origin", sha+":"+ref)
	return e
}
func (g *Git) Delete(ctx context.Context, ref, sha string) error {
	// The lander never deletes: a land/prod branch is the record of what
	// was pushed to main (B-05).
	if g.lander || !g.inScope(ref) || !fullSHA.MatchString(sha) {
		return errors.New("delete outside landing scope")
	}
	// A crash after deletion is already complete. A race after this read is
	// still protected by the atomic lease below.
	tip, err := g.Ref(ctx, ref)
	if err != nil {
		return err
	}
	if tip == "" {
		return nil
	}
	if tip != sha {
		return errors.New("branch no longer belongs to operation")
	}
	// Empty temporary repo: no candidate config; lease is the atomic SHA fence.
	dir, e := os.MkdirTemp("", "agentrt-landing-prune-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	if _, e = g.run(ctx, dir, false, nil, nil, "init", "--template=", "."); e != nil {
		return e
	}
	_, e = g.run(ctx, dir, true, nil, nil, "push", "--porcelain", "--force-with-lease="+ref+":"+sha, g.remote, ":"+ref)
	return e
}

// Repository is bound at construction, never supplied by a task or URL.
func (g *Git) Repository() string {
	if g.repo == "" {
		return "bifrost-platform"
	}
	return g.repo
}
func selectedRepo(repos []string) (landing.LandingRepo, error) {
	name := "bifrost-platform"
	if len(repos) == 1 {
		name = repos[0]
	}
	r, ok := landing.LandingRepos[name]
	if !ok || len(repos) > 1 {
		return r, errors.New("unsupported landing repo")
	}
	return r, nil
}
