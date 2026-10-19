//go:build linux

package agentd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TD-371 on a Linux host: the wall holds, every check inside it as it must
// be, and a process the walled one starts is held as well.
func TestWallHolds(t *testing.T) {
	if abi, err := landlockABI(); err != nil || abi < wallABI {
		t.Skipf("no Landlock ABI %d here (%d, %v): the agent host's probe proves it", wallABI, abi, err)
	}
	w, lines := ProveWall(t.Context(), os.Args[0], t.TempDir(), launcherEnv(), true)
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
