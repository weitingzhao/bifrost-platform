//go:build linux

package agentd

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TD-371 on a Linux host: the wall holds, every check inside it as it must
// be, and a process the walled one starts is held as well.
func TestWallHolds(t *testing.T) {
	if abi, err := landlockABI(); err != nil || abi < wallABI {
		t.Skipf("no Landlock ABI %d here (%d, %v): the agent host's probe proves it", wallABI, abi, err)
	}
	w, lines := ProveWall(t.Context(), os.Args[0], t.TempDir(), launcherEnv(), "/no/such/codex")
	t.Log(strings.Join(lines, "\n"))
	for _, name := range []string{"exec:deterministic", "exec:cursor", "exec:claude"} {
		if !w.holds(name) {
			t.Fatalf("the wall does not hold for %s", name)
		}
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "FAIL") {
			t.Errorf("%s", l)
		}
	}
	// A grandchild, started by a shell inside the wall, writes nowhere else.
	in, outside := t.TempDir(), t.TempDir()
	argv := append(wallArgv(os.Args[0], []string{in}), "/bin/sh", "-c", `sh -c "echo x > $1/inside && echo x > $2/outside"`, "sh", in, outside)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = launcherEnv()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the grandchild wrote outside:\n%s", out)
	}
	if _, serr := os.Stat(filepath.Join(in, "inside")); serr != nil {
		t.Errorf("the grandchild could not write inside: %v\n%s", serr, out)
	}
	if _, serr := os.Stat(filepath.Join(outside, "outside")); serr == nil {
		t.Errorf("the grandchild wrote outside")
	}
}

// Review 300 A3 on a Linux host: the setup's commands run in a narrower wall,
// and cannot write the executor's home or the attempt's output.
func TestSetupWallKeepsTheExecutorsHome(t *testing.T) {
	if abi, err := landlockABI(); err != nil || abi < wallABI {
		t.Skipf("no Landlock ABI %d here (%d, %v)", wallABI, abi, err)
	}
	root := t.TempDir()
	a := &Attempt{Dir: filepath.Join(root, "a1"), Out: filepath.Join(root, "a1.out")}
	for _, d := range []string{a.Dir, attemptHome(a.Out), attemptTmp(a.Out)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(a.Out, "executor-ran")
	sp, err := withSetup(os.Args[0], a.Out, [][]string{
		{"sh", "-c", "echo x > in-checkout && echo x > " + filepath.Join(attemptTmp(a.Out), "in-tmp")},
		{"sh", "-c", "echo x > " + filepath.Join(attemptHome(a.Out), "config.toml") + " || echo x > refused-home"},
		{"sh", "-c", "echo x > " + filepath.Join(a.Out, "verify-spec.json") + " || echo x > refused-out"},
	}, Spawn{Argv: []string{"sh", "-c", "touch " + marker}}, setupOpts{Home: setupHome(a.Out), Writes: innerWrites(a, nil)})
	if err != nil {
		t.Fatal(err)
	}
	argv := append(wallArgv(os.Args[0], []string{a.Dir, a.Out}), sp.Argv...)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = a.Dir, launcherEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, p := range []string{filepath.Join(a.Dir, "in-checkout"), filepath.Join(attemptTmp(a.Out), "in-tmp"), filepath.Join(a.Dir, "refused-home"),
		filepath.Join(a.Dir, "refused-out"), marker} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	for _, p := range []string{filepath.Join(attemptHome(a.Out), "config.toml"), filepath.Join(a.Out, "verify-spec.json")} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("the setup wrote %s", p)
		}
	}
}

// TD-404 on a Linux host (review td404-1 B2): inside the walls, the setup's
// narrower one and the executor's around both, what the setup fetches into
// Go's module cache the executor reads, for each executor agentd walls.
// codex has its own sandbox; TestSetupAndExecutorShareGoCaches holds its
// writable root to the caches' directory.
func TestWalledSetupFillsTheExecutorsGoCache(t *testing.T) {
	if abi, err := landlockABI(); err != nil || abi < wallABI {
		t.Skipf("no Landlock ABI %d here (%d, %v)", wallABI, abi, err)
	}
	env := append(slices.Clone(hostEnv[1:]), "PATH=/usr/bin:/bin")
	for _, name := range []string{"exec:deterministic", "exec:cursor", "exec:claude"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			a := &Attempt{Claim: &Claimed{}, Spec: TaskSpec{Text: "# do it"}, Dir: filepath.Join(root, "a1"), Out: filepath.Join(root, "a1.out"),
				Walled: true}
			for _, d := range []string{a.Dir, a.Out} {
				must(t, os.MkdirAll(d, 0o700))
			}
			sp, err := Executors(os.Args[0], env, Secrets{})[name](a)
			must(t, err)
			sp.Argv = []string{"sh", "-c", `cat "$GOMODCACHE/cache/download/marker" > seen.txt`}
			sp, err = withSetup(os.Args[0], a.Out, [][]string{{"sh", "-c", `mkdir -p "$GOMODCACHE/cache/download" && ` +
				`echo fetched > "$GOMODCACHE/cache/download/marker"`}}, sp, setupOpts{Home: setupHome(a.Out), Writes: innerWrites(a, nil)})
			must(t, err)
			argv := append(wallArgv(os.Args[0], executorWrites(name, a, nil)), sp.Argv...)
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.Dir, cmd.Env = a.Dir, append(slices.Clone(sp.Env), "TESTMAIN_AGENTD_LAUNCHER=1")
			if out, rerr := cmd.CombinedOutput(); rerr != nil {
				t.Fatalf("%v\n%s", rerr, out)
			}
			if got, _ := os.ReadFile(filepath.Join(a.Dir, "seen.txt")); string(got) != "fetched\n" {
				t.Fatalf("the executor did not find what the walled setup fetched: %q", got)
			}
		})
	}
}

// Review td404-3 A1 on a Linux host: the stage runs inside the executor's
// wall as Run puts it (the wall over the checkout, the output and the
// sockets' directory made before it), its programs inside walls of their
// own; the executor reaches the database as the tests do, and afterwards the
// sockets' directory is there and empty and the database's is gone.
func TestWalledTestDBStage(t *testing.T) {
	if abi, err := landlockABI(); err != nil || abi < wallABI {
		t.Skipf("no Landlock ABI %d here (%d, %v)", wallABI, abi, err)
	}
	tools := testDBTools(t)
	root := shortDir(t)
	a := &Attempt{Claim: &Claimed{}, Spec: TaskSpec{Text: "# do it"}, Dir: filepath.Join(root, "a1"), Out: filepath.Join(root, "a1.out"),
		Walled: true, root: root}
	for _, d := range []string{a.Dir, a.Out} {
		must(t, os.MkdirAll(d, 0o700))
	}
	env := append(slices.Clone(hostEnv[1:]), "PATH=/usr/bin:/bin")
	sp, err := Executors(os.Args[0], env, Secrets{})["exec:deterministic"](a)
	must(t, err)
	sp.Argv = []string{os.Args[0]} // the probe (TestMain)
	sp, err = withTestDB(os.Args[0], tools, a, sp, true)
	must(t, err)
	a.testDB = tools
	argv := append(wallArgv(os.Args[0], executorWrites("exec:deterministic", a, nil)), sp.Argv...)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = a.Dir, append(slices.Clone(sp.Env), "TESTMAIN_AGENTD_LAUNCHER=1", "TESTMAIN_TESTDB_PROBE=1")
	if out, rerr := cmd.CombinedOutput(); rerr != nil {
		t.Fatalf("%v\n%s", rerr, out)
	}
	sock := testDBSockDir(root, a.Out)
	if left, rerr := os.ReadDir(sock); rerr != nil || len(left) != 0 {
		t.Fatalf("the sockets' directory after the attempt: %v %v", left, rerr)
	}
	if _, serr := os.Stat(testDBDir(a.Out)); !os.IsNotExist(serr) {
		t.Fatalf("the database's directory is still there: %v", serr)
	}
}
