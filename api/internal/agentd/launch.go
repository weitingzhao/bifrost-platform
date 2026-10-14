package agentd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// The launcher (v5 §5.2 本地登记, rule R1). agentd never starts an executor
// directly: it starts itself as the launcher (LaunchArg) in a new process
// group, and the launcher waits on file descriptor 3 for one release byte
// before it replaces itself with the executor. agentd records the group (its
// id and the leader's start time) in the registry, synced, and only then
// writes the byte. If agentd dies in between, the pipe closes, the launcher
// reads end of file and exits: no process group runs that the registry does
// not know.

// LaunchArg is the first argument that makes agentd the launcher.
const LaunchArg = "launch"

const releaseByte = 'g'

// RunLauncher is the launcher's whole life: wait for the release byte, then
// exec argv (argv[0] looked up on PATH). It returns only on failure, with the
// exit code to use.
func RunLauncher(argv []string) int {
	gate := os.NewFile(3, "release")
	if gate == nil {
		return 125
	}
	b := make([]byte, 1)
	n, _ := io.ReadFull(gate, b)
	_ = gate.Close()
	if n != 1 || b[0] != releaseByte || len(argv) == 0 {
		return 125
	}
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentd launcher:", err)
		return 127
	}
	err = syscall.Exec(bin, argv, os.Environ())
	fmt.Fprintln(os.Stderr, "agentd launcher:", err)
	return 126
}

// Spawn is an executor to run: an argument array, never a shell line.
type Spawn struct {
	Argv           []string
	Env            []string // the whole environment; nothing is inherited
	Dir            string
	Stdout, Stderr io.Writer
}

// Proc is a launched process group.
type Proc struct {
	PGID  int
	Start string
	cmd   *exec.Cmd
}

// Launch starts sp through the launcher at self (agentd's own executable) in
// a new process group, calls record with the group before releasing it, and
// releases it only if record succeeded; otherwise the group is killed and
// never runs sp.
func Launch(self string, sp Spawn, record func(pgid int, start string) error) (*Proc, error) {
	if len(sp.Argv) == 0 {
		return nil, errors.New("agentd: nothing to launch")
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self, append([]string{LaunchArg, "--"}, sp.Argv...)...)
	cmd.Env, cmd.Dir, cmd.Stdout, cmd.Stderr = sp.Env, sp.Dir, sp.Stdout, sp.Stderr
	cmd.ExtraFiles = []*os.File{r}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = cmd.Start(); err != nil {
		_ = r.Close()
		_ = w.Close()
		return nil, err
	}
	_ = r.Close()
	p := &Proc{PGID: cmd.Process.Pid, cmd: cmd}
	if p.Start, err = StartTime(p.PGID); err == nil {
		err = record(p.PGID, p.Start)
	}
	if err != nil {
		_ = w.Close()
		KillGroup(p.PGID)
		_ = cmd.Wait()
		return nil, err
	}
	_, err = w.Write([]byte{releaseByte})
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		KillGroup(p.PGID)
		_ = cmd.Wait()
		return nil, err
	}
	return p, nil
}

// Wait waits for the group's leader and returns its exit code (-1 when a
// signal ended it).
func (p *Proc) Wait() int {
	_ = p.cmd.Wait()
	if p.cmd.ProcessState == nil {
		return -1
	}
	return p.cmd.ProcessState.ExitCode()
}

// Terminate ends the whole group: TERM, and KILL after grace if any of it is
// still there (v5 §5.2 心跳与死人开关: 10 seconds).
func Terminate(pgid int, grace time.Duration) {
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if syscall.Kill(-pgid, 0) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	KillGroup(pgid)
}

// KillGroup sends KILL to every process of the group.
func KillGroup(pgid int) { _ = syscall.Kill(-pgid, syscall.SIGKILL) }

// Alive reports whether the group still has a process and its leader is the
// one recorded (same start time): a group id the system gave to another
// process since is not ours.
func Alive(pgid int, start string) bool {
	if pgid <= 1 || syscall.Kill(-pgid, 0) != nil {
		return false
	}
	now, err := StartTime(pgid)
	return err == nil && now == start
}

// StartTime is when process pid started, as ps prints it.
func StartTime(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "lstart=", "-p", fmt.Sprint(pid)).Output()
	if err != nil {
		return "", fmt.Errorf("agentd: start time of %d: %w", pid, err)
	}
	s := strings.Join(strings.Fields(string(out)), " ")
	if s == "" {
		return "", fmt.Errorf("agentd: no process %d", pid)
	}
	return s, nil
}
