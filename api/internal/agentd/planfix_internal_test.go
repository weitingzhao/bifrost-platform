package agentd

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A confined plan attempt's follow-up is built as its first run was: with a
// test database, the wall, then the stage, then the executor's follow-up
// command, which checkPins takes (11-06: without the stage it lacked "the
// test database's stage" and never ran); without one, the wall and the
// follow-up command alone.
func TestPlanFollowUpSpawnHasTheTestDBStage(t *testing.T) {
	const self, session = "/opt/agentd", "5f0c2a8e-1b7d-4c3e-9a51-0d2f6b7e8c91"
	tools := &TestDBTools{Initdb: "/usr/lib/postgresql/16/bin/initdb", Postgres: "/usr/lib/postgresql/16/bin/postgres", Redis: "/usr/bin/redis-server"}
	for _, withDB := range []bool{true, false} {
		t.Run(map[bool]string{true: "with a test database", false: "without one"}[withDB], func(t *testing.T) {
			root := shortDir(t)
			a := &Attempt{Claim: &Claimed{Role: "plan", EffectClass: "none", Tool: Tool{Executor: "exec:claude", Model: "opus"}},
				Spec: TaskSpec{Text: "# plan"}, Dir: filepath.Join(root, "a1"), Out: filepath.Join(root, "a1.out"), Walled: true, root: root}
			for _, d := range []string{a.Dir, a.Out} {
				must(t, os.MkdirAll(d, 0o700))
			}
			a.inner = innerWrites(a, nil)
			d := &Daemon{Self: self, Wall: &Wall{ABI: 4}}
			if withDB {
				d.TestDB = tools
			}
			sp, err := Executors(self, hostEnv, Secrets{ClaudeOAuthToken: "fixture-token"})["exec:claude"](a)
			must(t, err)
			repo := "bifrost-trade-infra"
			if withDB {
				repo = "bifrost-platform"
			}
			// The first run, as Run builds it.
			first, _, err := d.testDBFor(a, repo, "exec:claude", sp)
			must(t, err)
			if (a.testDB != nil) != withDB {
				t.Fatalf("the attempt's test database: %v", a.testDB)
			}
			first.Argv = append(wallArgv(self, executorWrites("exec:claude", a, nil)), first.Argv...)
			must(t, checkPins(d.Wall, "exec:claude", self, a, nil, first))
			log := `{"type":"system","subtype":"init","session_id":"` + session + `"}` + "\n" +
				`{"type":"result","is_error":false,"result":"# plan","session_id":"` + session + `"}` + "\n"
			must(t, os.WriteFile(filepath.Join(a.Out, "log.txt"), []byte(log), 0o600))

			why := errors.New("the report has no ```json block")
			fu, err := d.followUpSpawn(planRun{a: a, sp: sp, confined: true}, why)
			if err != nil {
				t.Fatalf("the follow-up's spawn: %v", err)
			}
			bare, err := sp.FollowUp(planFixPrompt(why))
			must(t, err)
			want := wallArgv(self, executorWrites("exec:claude", a, nil))
			if withDB {
				want = append(want, testDBArgv(self, tools, a.root, a.Out, true)...)
			}
			want = append(want, bare.Argv...)
			if !slices.Equal(fu.Argv, want) {
				t.Fatalf("follow-up = %q\nwant %q", fu.Argv, want)
			}
			if i := slices.Index(fu.Argv, "--resume"); i < 0 || fu.Argv[i+1] != session {
				t.Fatalf("the follow-up does not resume the session: %q", fu.Argv)
			}
			if !withDB {
				return
			}
			// What the follow-up was before: the wall alone around it.
			bare.Argv = append(wallArgv(self, executorWrites("exec:claude", a, nil)), bare.Argv...)
			if err = checkPins(d.Wall, "exec:claude", self, a, nil, bare); err == nil || !strings.Contains(err.Error(), "the test database's stage") {
				t.Fatalf("a follow-up without the stage: %v", err)
			}
		})
	}
}
