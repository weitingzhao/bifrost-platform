package agentd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// TestDaemon is how a test runs agentd in a process of its own, to kill it
// at a point of its choosing (T34 case 4: a restart in the middle of a
// hand-over): the test binary, started with AGENTD_TEST_DAEMON naming a file
// of this, claims once with exec:cursor running Script and runs the attempt.
type TestDaemon struct {
	Base, Host, Token string
	Root, Registry    string
	Remote            string
	GitEnv            []string
	Script            string
}

// TestDaemonEnv names the configuration file; TestMain runs the daemon when
// it is set.
const TestDaemonEnv = "AGENTD_TEST_DAEMON"

func runTestDaemon(path string) int {
	_ = os.Unsetenv(TestDaemonEnv) // not for the launcher or the executor
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var c TestDaemon
	if err = json.Unmarshal(raw, &c); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	reg, err := OpenRegistry(c.Registry)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	script := func(a *Attempt) (Spawn, error) {
		return Spawn{Argv: []string{"sh", "-c", c.Script}, Dir: a.Dir, Env: append(os.Environ(), "OUT="+a.Out, "TESTMAIN_AGENTD_LAUNCHER=1")}, nil
	}
	d := &Daemon{
		Client:    &Client{Base: c.Base, Host: c.Host, Token: c.Token},
		Registry:  reg,
		Root:      c.Root,
		Self:      os.Args[0],
		Executors: map[string]Executor{"exec:cursor": script},
		Remote:    func(string) string { return c.Remote },
		GitEnv:    c.GitEnv,
		Beat:      100 * time.Millisecond, Retry: 50 * time.Millisecond, SalvageEvery: 50 * time.Millisecond, KillGrace: time.Second,
		Logf: func(f string, args ...any) { fmt.Fprintf(os.Stderr, f+"\n", args...) },
	}
	ctx := context.Background()
	cl, err := d.Client.Claim(ctx, 1, []Tool{{Executor: "exec:cursor", Provider: "cursor-mix"}})
	if err != nil || cl == nil {
		fmt.Fprintf(os.Stderr, "claim = %+v %v\n", cl, err)
		return 1
	}
	out, err := d.Run(ctx, cl)
	fmt.Fprintf(os.Stderr, "run = %+v %v\n", out, err)
	if err != nil {
		return 1
	}
	return 0
}
