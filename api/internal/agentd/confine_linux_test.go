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
