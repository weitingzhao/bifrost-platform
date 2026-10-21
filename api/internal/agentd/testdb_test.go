package agentd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// testDBTools are this machine's programs for a test database, or the test
// skips: CI's image has none; the agent host and a developer's Mac do.
func testDBTools(t *testing.T) *TestDBTools {
	t.Helper()
	tools, err := FindTestDBTools(os.Getenv("AGENTD_PG_BIN"))
	if err != nil {
		t.Skipf("no test database programs here: %v", err)
	}
	return tools
}

// shortDir is a directory whose path leaves room for a socket under it: a
// test's own temporary directory, named after the test, is too long for one
// on macOS (103 bytes). A temporary directory itself too long for one (an
// attempt's on the agent host) skips the test.
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "a")
	must(t, err)
	t.Cleanup(func() { _ = forceRemove(d) })
	if p := pgSocket(testDBSockDir(d, filepath.Join(d, "tasks", "t_01M5E53AASSQBNFFHH29B3S79P", "a1.out"))); len(p) > sockMax() {
		t.Skipf("this machine's temporary directory %s is too long for a socket under it (%d bytes)", d, len(p))
	}
	return d
}

// postgresOf says whether a process still runs Postgres over dir.
func postgresOf(t *testing.T, dir string) bool {
	t.Helper()
	out, err := exec.Command("pgrep", "-f", dir).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// TD-403: the stage starts the attempt's own Postgres and Redis, each on a
// socket in the attempt's directory, with the roles the tests use; the
// executor sees AGENTRT_TEST_PG, AGENTRT_TEST_REDIS and AGENTRT_TEST_REQUIRED=1
// and reaches both; once it ends, both are stopped, the directory is gone,
// and the stage exits as the executor did.
func TestTestDBStage(t *testing.T) {
	tools := testDBTools(t)
	out := filepath.Join(shortDir(t), "a1.out")
	must(t, os.MkdirAll(attemptTmp(out), 0o700))
	dir := testDBDir(out)
	probe := filepath.Join(out, "probe.txt")
	script := `printf '%s\n%s\n%s\n' "$AGENTRT_TEST_PG" "$AGENTRT_TEST_REDIS" "$AGENTRT_TEST_REQUIRED" > ` + probe +
		` && test -S "` + pgSocket(dir) + `" && test -S "` + filepath.Join(dir, "redis.sock") + `" && "$PROBE" && exit 7`
	argv := append(testDBArgv(os.Args[0], tools, "", out, false), "sh", "-c", script)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = out
	cmd.Env = append(launcherEnv(), "PROBE="+os.Args[0], "AGENTD_TEST_DB_PROBE=1")
	b, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 7 {
		t.Fatalf("the stage ended %v, want the executor's exit 7:\n%s", err, b)
	}
	got, _ := os.ReadFile(probe)
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "host="+dir+" ") || lines[1] != "unix://"+filepath.Join(dir, "redis.sock") || lines[2] != "1" {
		t.Fatalf("the executor saw %q", lines)
	}
	if _, serr := os.Stat(dir); !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("the database's directory is still there: %v", serr)
	}
	if postgresOf(t, dir) {
		t.Fatal("Postgres still runs over the attempt's directory")
	}
	if v, failed := setupFailure(out); failed {
		t.Fatalf("testdb.json says it failed: %+v", v)
	}
}

// The executor's probe (TestMain runs it): it connects to the test
// database as the tests do, as the superuser and as each role they use, and
// to Redis; it exits 0 when every one answers.
func probeTestDB() int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base := os.Getenv("AGENTRT_TEST_PG")
	for _, role := range append([]string{""}, testRoles...) {
		dsn := base + " dbname=postgres"
		if role != "" {
			dsn += " user=" + role
		}
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			os.Stderr.WriteString("probe: " + role + ": " + err.Error() + "\n")
			return 1
		}
		_ = c.Close(ctx)
	}
	if !redisUp(strings.TrimPrefix(os.Getenv("AGENTRT_TEST_REDIS"), "unix://")) {
		os.Stderr.WriteString("probe: redis did not answer\n")
		return 1
	}
	return 0
}

// A stop ends the whole group, as agentd ends an attempt's: the executor,
// Postgres and Redis go, and the stage removes the directory on its way out.
func TestTestDBStageEndsWithItsGroup(t *testing.T) {
	tools := testDBTools(t)
	out := filepath.Join(shortDir(t), "a1.out")
	must(t, os.MkdirAll(attemptTmp(out), 0o700))
	dir := testDBDir(out)
	argv := append(testDBArgv(os.Args[0], tools, "", out, false), "sh", "-c", "sleep 60")
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = out, launcherEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	must(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for deadline := time.Now().Add(time.Minute); !redisUp(filepath.Join(dir, "redis.sock")) || !postgresOf(t, dir); time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the database did not come up")
		}
	}
	Terminate(cmd.Process.Pid, 10*time.Second)
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the stage did not end")
	}
	if gone, _ := groupGone(cmd.Process.Pid); !gone {
		t.Fatal("the group is not gone")
	}
	if postgresOf(t, dir) {
		t.Fatal("Postgres outlived the stop")
	}
	if _, serr := os.Stat(dir); !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("the database's directory is still there: %v", serr)
	}
}

// A database that does not start fails the attempt before its executor, as
// a setup does: exit setupFailed, testdb.json says what failed, the
// executor never runs.
func TestTestDBStageThatFails(t *testing.T) {
	out := filepath.Join(shortDir(t), "a1.out")
	must(t, os.MkdirAll(attemptTmp(out), 0o700))
	marker := filepath.Join(out, "executor-ran")
	broken := &TestDBTools{Initdb: "/bin/false", Postgres: "/bin/false", Redis: "/bin/false"}
	argv := append(testDBArgv(os.Args[0], broken, "", out, false), "sh", "-c", "touch "+marker)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = out, launcherEnv()
	b, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != setupFailed || !strings.Contains(string(b), "the executor did not start") {
		t.Fatalf("the stage ended %v:\n%s", err, b)
	}
	if _, serr := os.Stat(marker); !errors.Is(serr, os.ErrNotExist) {
		t.Fatal("the executor ran")
	}
	if !isSetupFailure("exit", setupFailed, out) {
		t.Fatal("the attempt's end is not read as a failure before its executor")
	}
	if r := string(setupReportOf(out)); !strings.Contains(r, "The attempt's test database did not start") || !strings.Contains(r, "/bin/false") {
		t.Fatalf("the report:\n%s", r)
	}
}

// A confined attempt with a test database passes checkPins only with the
// stage where Run puts it, after the setup, its programs walled.
func TestCheckPinsHoldsTheTestDBStage(t *testing.T) {
	const self = "/opt/agentd"
	tools := &TestDBTools{Initdb: "/usr/lib/postgresql/16/bin/initdb", Postgres: "/usr/lib/postgresql/16/bin/postgres", Redis: "/usr/bin/redis-server"}
	for _, name := range []string{"exec:deterministic", "exec:cursor", "exec:claude"} {
		t.Run(name, func(t *testing.T) {
			root := shortDir(t)
			a := &Attempt{Claim: &Claimed{}, Spec: TaskSpec{Text: "# do it"}, Dir: filepath.Join(root, "a1"), Out: filepath.Join(root, "a1.out"), Walled: true}
			if name != "exec:cursor" {
				a.root = root // cursor's own data directory under this root is too long on macOS
			}
			for _, d := range []string{a.Dir, a.Out} {
				must(t, os.MkdirAll(d, 0o700))
			}
			a.inner = innerWrites(a, nil)
			w := &Wall{ABI: 4}
			sp, err := Executors(self, hostEnv, Secrets{CursorAPIKey: "fixture-key", ClaudeOAuthToken: "fixture-token"})[name](a)
			must(t, err)
			withDB, err := withTestDB(self, tools, a, sp, true)
			must(t, err)
			a.testDB = tools
			if sock := testDBSockDir(a.root, a.Out); !slices.Contains(executorWrites(name, a, nil), sock) && a.root != "" || !slices.Contains(withDB.Argv, sock) {
				t.Fatalf("the sockets' directory %s is not walled for the stage: %q", sock, withDB.Argv)
			}
			o := setupOpts{Home: setupHome(a.Out), Writes: a.inner}
			if name == "exec:cursor" {
				o.Refuse = cursorRefused
			}
			full, err := withSetup(self, a.Out, [][]string{{"npm", "ci"}}, withDB, o)
			must(t, err)
			wall := wallArgv(self, executorWrites(name, a, nil))
			full.Argv = append(slices.Clone(wall), full.Argv...)
			if err = checkPins(w, name, self, a, nil, full); err != nil {
				t.Fatalf("the spawn as Run builds it: %v", err)
			}
			if !slices.Contains(full.Argv, "--write") || !slices.Contains(full.Argv, testDBDir(a.Out)) {
				t.Fatalf("the stage's programs are not walled: %q", full.Argv)
			}
			bare, err := withSetup(self, a.Out, [][]string{{"npm", "ci"}}, sp, o)
			must(t, err)
			bare.Argv = append(slices.Clone(wall), bare.Argv...)
			if err = checkPins(w, name, self, a, nil, bare); err == nil || !strings.Contains(err.Error(), "the test database's stage") {
				t.Fatalf("a spawn without the stage: %v", err)
			}
			unwalled, err := withTestDB(self, tools, a, sp, false)
			must(t, err)
			unwalled, err = withSetup(self, a.Out, [][]string{{"npm", "ci"}}, unwalled, o)
			must(t, err)
			unwalled.Argv = append(slices.Clone(wall), unwalled.Argv...)
			if err = checkPins(w, name, self, a, nil, unwalled); err == nil {
				t.Fatal("a stage whose programs run unwalled passed")
			}
		})
	}
}

// A socket path over what the system takes fails the attempt before
// anything starts, and says so.
func TestTestDBSocketPathTooLong(t *testing.T) {
	a := &Attempt{Out: "/" + strings.Repeat("x", 120)}
	if _, err := withTestDB("/opt/agentd", &TestDBTools{}, a, Spawn{Argv: []string{"sh"}}, false); err == nil ||
		!strings.Contains(err.Error(), "over the") {
		t.Fatalf("a long socket path: %v", err)
	}
}

// Which attempts get the stage: one at a repository whose tests need a
// database, on a host with the programs; on a host without them the attempt
// runs as before and its log says why its database tests skip; one at
// another repository runs as before.
func TestTestDBFor(t *testing.T) {
	tools := &TestDBTools{Initdb: "/x/initdb", Postgres: "/x/postgres", Redis: "/x/redis-server"}
	root := shortDir(t)
	sp := Spawn{Argv: []string{"executor"}}
	for _, c := range []struct {
		repo        string
		tools       *TestDBTools
		stage, noDB bool
	}{{"bifrost-platform", tools, true, false}, {"bifrost-platform", nil, false, true}, {"bifrost-trade-infra", tools, false, false}} {
		a := &Attempt{Out: filepath.Join(root, "a1.out")}
		got, noDB, err := (&Daemon{Self: "/opt/agentd", TestDB: c.tools}).testDBFor(a, c.repo, sp)
		must(t, err)
		if staged := got.Argv[0] == "/opt/agentd" && got.Argv[1] == TestDBArg; staged != c.stage || noDB != c.noDB || (a.testDB != nil) != c.stage {
			t.Errorf("%s with tools %v: argv %q, no database %v", c.repo, c.tools != nil, got.Argv, noDB)
		}
	}
}

// What an end that killed the stage leaves of the database goes when the
// attempt settles, read-only files and all, and nothing outside it.
func TestRemoveTestDB(t *testing.T) {
	root := shortDir(t)
	out := filepath.Join(root, "a1.out")
	pg := filepath.Join(testDBDir(out), "pg")
	must(t, os.MkdirAll(pg, 0o700))
	must(t, os.WriteFile(filepath.Join(pg, "postmaster.pid"), []byte("1"), 0o400))
	must(t, os.Chmod(pg, 0o500))
	keep := filepath.Join(attemptTmp(out), "keep")
	must(t, os.WriteFile(keep, []byte("x"), 0o600))
	sock := testDBSockDir(root, out)
	must(t, os.MkdirAll(sock, 0o700))
	must(t, os.WriteFile(filepath.Join(sock, ".s.PGSQL.5432.lock"), []byte("1"), 0o600))
	other := filepath.Join(filepath.Dir(sock), "someone-elses")
	must(t, os.MkdirAll(other, 0o700))
	removeTestDB(root, out, t.Logf)
	for _, gone := range []string{testDBDir(out), sock} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s is still there: %v", gone, err)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("another attempt's sockets went: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("what is beside it went: %v", err)
	}
	removeTestDB(root, filepath.Join(root, "none.out"), t.Logf) // nothing there: nothing to do
}

// Review td404-3 A1: the executor's wall takes only directories that are
// there, and the stage inside it may not make one beside its own; so the
// sockets' directory under agentd's root is made before the launch, outside
// every wall, the attempt's own (0700), and the stage leaves it, emptied, for
// settle to remove.
func TestTestDBSocketDirIsMadeBeforeTheWall(t *testing.T) {
	root := shortDir(t)
	a := &Attempt{Out: filepath.Join(root, "tasks", "t_01M5E53AASSQBNFFHH29B3S79P", "a1.out"), root: root}
	_, err := withTestDB("/opt/agentd", &TestDBTools{Initdb: "/x/initdb", Postgres: "/x/postgres", Redis: "/x/redis-server"}, a,
		Spawn{Argv: []string{"executor"}}, true)
	must(t, err)
	sock := testDBSockDir(root, a.Out)
	fi, err := os.Lstat(sock)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("the sockets' directory before the launch: %v %v", fi, err)
	}
	must(t, os.WriteFile(filepath.Join(sock, "left"), []byte("x"), 0o600))
	must(t, emptyDir(sock))
	if left, _ := os.ReadDir(sock); len(left) != 0 {
		t.Fatalf("emptied, it holds %v", left)
	}
	if _, err = os.Lstat(sock); err != nil {
		t.Fatalf("the stage removed the directory itself: %v", err)
	}
	// A link where the directory goes is refused, not followed.
	other := shortDir(t)
	b := &Attempt{Out: filepath.Join(other, "a2.out"), root: other}
	linked := testDBSockDir(other, b.Out)
	must(t, os.MkdirAll(filepath.Dir(linked), 0o700))
	must(t, os.Symlink(t.TempDir(), linked))
	if _, err = withTestDB("/opt/agentd", &TestDBTools{}, b, Spawn{Argv: []string{"executor"}}, true); err == nil {
		t.Fatal("a link for the sockets' directory was taken")
	}
}

// Review td404-3 B2: Postgres and Redis are told to stop at once and share
// one deadline; two that do not stop are both killed by it, not one after
// the other.
func TestTestDBStopSharesOneDeadline(t *testing.T) {
	old := testDBStop
	testDBStop = time.Second
	t.Cleanup(func() { testDBStop = old })
	dir := t.TempDir()
	stubborn := []string{"sh", "-c", `trap '' INT TERM; while :; do sleep 1; done`}
	db := &testDB{}
	var err error
	if db.postgres, db.pgDone, err = startIn(dir, "pg.log", os.Environ(), nil, stubborn); err != nil {
		t.Fatal(err)
	}
	if db.redis, db.rdDone, err = startIn(dir, "redis.log", os.Environ(), nil, stubborn); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // both have set their traps
	start := time.Now()
	db.stop()
	if took := time.Since(start); took > 1800*time.Millisecond {
		t.Fatalf("stopping two that would not stop took %s, over one deadline of %s", took, testDBStop)
	}
}

// Review td404-3 B1: an attempt left uncleared, whose group a later
// takeover finds gone, leaves no test database behind either.
func TestRecoverRemovesAnUnclearedAttemptsTestDB(t *testing.T) {
	root := shortDir(t)
	reg, err := OpenRegistry(filepath.Join(root, "registry.json"))
	must(t, err)
	d := &Daemon{Root: root, Registry: reg, Logf: t.Logf}
	work := filepath.Join(root, "tasks", "t_01M5E53AASSQBNFFHH29B3S79P", "a1")
	db, sock := testDBDir(work+".out"), testDBSockDir(root, work+".out")
	for _, p := range []string{filepath.Join(db, "pg"), sock} {
		must(t, os.MkdirAll(p, 0o700))
	}
	must(t, reg.Put(Entry{AttemptID: "a1", TaskID: "t1", Workdir: work, Phase: "uncleared", Group: GroupRemains}))
	must(t, d.Recover(context.Background()))
	for _, p := range []string{db, sock} {
		if _, serr := os.Lstat(p); !errors.Is(serr, os.ErrNotExist) {
			t.Fatalf("%s outlived the attempt: %v", p, serr)
		}
	}
}
