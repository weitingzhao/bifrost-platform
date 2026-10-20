package agentd

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

// Review 419 B2: a process's identity is the same each time it is read,
// another process's is another, and a process that is not there has none.
func TestProcIdentity(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("no process identity on " + runtime.GOOS)
	}
	self, err := procIdentity(os.Getpid())
	if err != nil || self == "" {
		t.Fatalf("this process: %q, %v", self, err)
	}
	if again, _ := procIdentity(os.Getpid()); again != self {
		t.Fatalf("read again: %q, then %q", self, again)
	}
	cmd := exec.Command("sleep", "60")
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	other, err := procIdentity(pid)
	if err != nil || other == "" || other == self {
		t.Fatalf("another process: %q (this one %q), %v", other, self, err)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if id, err := procIdentity(pid); err == nil {
		t.Fatalf("a process that is gone: %q", id)
	}
}
