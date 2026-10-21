package agentd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// A task's throwaway test database (TD-403; the Owner 2026-10-20 20:4x:
// 「每次尝试临时起一个 Postgres：在尝试自己的临时目录里 initdb，写墙里、不联网，用完即删，
// 没有共享状态」). The agentrt and agentd tests take Postgres from
// AGENTRT_TEST_PG and Redis from AGENTRT_TEST_REDIS and skip without them,
// unless AGENTRT_TEST_REQUIRED=1 (agentrttest); nothing set them on the agent
// host, so a check of store work passed with every database test skipped.
// Now an attempt at a repository whose Repo says TestDB gets its own: a
// Postgres cluster initdb makes in the attempt's temporary directory and a
// Redis, each on a unix socket there and nothing else (no TCP), inside a
// wall over that directory on a host that has one, with the roles the tests
// expect; the executor, and a check's commands, see the three variables.
// The stage that starts them (agentd testdb) runs after the setup, in the
// attempt's process group, and starts the executor as its child: once the
// executor ends the stage stops both, removes the directory and exits as
// the executor did. Postgres runs without pg_ctl, which would put it in a
// session of its own: in the group, whatever ends the attempt (a stop, the
// hard limit, a lost lease, agentd's restart, whose takeover ends the group)
// ends it too, and settle removes what such an end leaves (removeTestDB).

// TestDBArg is the first argument that makes agentd start a task's test
// database and then its executor.
const TestDBArg = "testdb"

// TestDBTools are the programs a test database needs, by absolute path.
type TestDBTools struct {
	Initdb, Postgres, Redis string
}

// FindTestDBTools finds them: initdb and postgres in pgBin when it is named
// (AGENTD_PG_BIN), else in the newest of Debian's /usr/lib/postgresql/<n>/bin
// (its packages keep the server's programs off PATH), else beside the
// postgres on PATH; redis-server on PATH. A host without them gives no
// attempt a test database, and says why.
func FindTestDBTools(pgBin string) (*TestDBTools, error) {
	dirs := []string{pgBin}
	if pgBin == "" {
		found, _ := filepath.Glob("/usr/lib/postgresql/*/bin")
		sort.Slice(found, func(i, j int) bool { return pgMajor(found[i]) > pgMajor(found[j]) })
		dirs = found
		if p, err := exec.LookPath("postgres"); err == nil {
			dirs = append(dirs, filepath.Dir(p))
		}
	}
	var t TestDBTools
	for _, d := range dirs {
		initdb, postgres := filepath.Join(d, "initdb"), filepath.Join(d, "postgres")
		if filepath.IsAbs(d) && isProgram(initdb) && isProgram(postgres) {
			t.Initdb, t.Postgres = initdb, postgres
			break
		}
	}
	if t.Postgres == "" {
		return nil, fmt.Errorf("no directory holds both initdb and postgres (looked in %q)", dirs)
	}
	r, err := exec.LookPath("redis-server")
	if err == nil {
		r, err = filepath.Abs(r)
	}
	if err != nil {
		return nil, fmt.Errorf("no redis-server: %w", err)
	}
	t.Redis = r
	return &t, nil
}

func pgMajor(dir string) int {
	n, _ := strconv.Atoi(filepath.Base(filepath.Dir(dir)))
	return n
}

func isProgram(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

// testDBDir is an attempt's test database: the cluster in pg/ and the logs
// beside it, in the temporary directory, which the setup, the checked code
// and codex's commands may write too.
func testDBDir(out string) string { return filepath.Join(attemptTmp(out), "db") }

// testDBSockDir is where its sockets are: a short directory of the
// attempt's own under root, as cursor's data directory is (cursorDataDir),
// named by a digest of out. A socket path is short (sockMax), and one under
// the attempt's output on the agent host was within two bytes of it (06,
// 2026-10-20: 105 of 107 for STG's root). With no root (a test) it is in the
// database's directory.
func testDBSockDir(root, out string) string {
	if root == "" {
		return testDBDir(out)
	}
	sum := sha256.Sum256([]byte("testdb\x00" + filepath.Clean(out)))
	return filepath.Join(root, "s", hex.EncodeToString(sum[:])[:24])
}

// sockMax is the longest path of a unix socket: the size of sun_path less
// its NUL. Postgres refuses a longer one.
func sockMax() int {
	if runtime.GOOS == "linux" {
		return 107
	}
	return 103
}

// pgSocket is the socket Postgres makes in dir for its default port.
func pgSocket(dir string) string { return filepath.Join(dir, ".s.PGSQL.5432") }

// testDBArgv is the start of the command that runs the stage over the
// attempt whose output is out under root, its programs inside a wall over
// the database's directories when the host has one.
func testDBArgv(self string, t *TestDBTools, root, out string, walled bool) []string {
	dir, sock := testDBDir(out), testDBSockDir(root, out)
	argv := []string{self, TestDBArg, "--dir", dir, "--sock", sock, "--out", filepath.Join(out, "testdb.json"),
		"--initdb", t.Initdb, "--postgres", t.Postgres, "--redis", t.Redis}
	if walled {
		argv = append(argv, "--write", dir)
		if sock != dir {
			argv = append(argv, "--write", sock)
		}
	}
	return append(argv, "--")
}

// withTestDB makes sp start the attempt's test database first. A socket
// path over what the system takes fails the attempt before anything starts.
// The sockets' directory under root is made here, outside every wall, as
// cursor's data directory is: the executor's wall takes only directories
// that are there (agentd confine), and the stage inside it may write in the
// directory but not beside it, so it empties it and leaves it, and settle
// removes it (review td404-3 A1).
func withTestDB(self string, t *TestDBTools, a *Attempt, sp Spawn, walled bool) (Spawn, error) {
	sock := testDBSockDir(a.root, a.Out)
	if p := pgSocket(sock); len(p) > sockMax() {
		return sp, fmt.Errorf("agentd: the test database's socket %s is %d bytes, over the %d a socket path takes", p, len(p), sockMax())
	}
	if a.root != "" {
		for _, d := range []string{filepath.Dir(sock), sock} {
			if err := ownDir(d); err != nil {
				return sp, err
			}
		}
	}
	sp.Argv = append(testDBArgv(self, t, a.root, a.Out, walled), sp.Argv...)
	return sp, nil
}

// testDBFor makes sp start the attempt's test database first when its
// repository's tests need one (store.Repos: TestDB; for a check, the
// subject's repository) and this host has the programs; it says when the
// repository wants one and the host has none.
func (d *Daemon) testDBFor(a *Attempt, repo string, sp Spawn) (Spawn, bool, error) {
	if !store.Repos[repo].TestDB {
		return sp, false, nil
	}
	if d.TestDB == nil {
		return sp, true, nil
	}
	sp, err := withTestDB(d.Self, d.TestDB, a, sp, d.Wall != nil)
	if err == nil {
		a.testDB = d.TestDB
	}
	return sp, false, err
}

// noTestDB is the line an attempt's log gets when its repository wants a
// test database and the host has none: its database tests skip.
const noTestDB = "agentd: this host has no test database programs (initdb, postgres, redis-server; TD-403): the database tests skip"

// testRoles are the roles the agentrt tests connect as (agentrttest), as
// CI's Postgres has them.
var testRoles = []string{"agentrt_owner_dev", "platform_agentrt_dev", "platform_agentrt_stg", "agent_reader"}

// testDBLimit is how long the database may take to start.
const testDBLimit = 2 * time.Minute

// testDB is a running test database.
type testDB struct {
	dir, sock, user string
	postgres, redis *exec.Cmd
	pgDone, rdDone  chan struct{}
}

// env is what the executor gets to reach it.
func (db *testDB) env() [][2]string {
	return [][2]string{
		{"AGENTRT_TEST_PG", fmt.Sprintf("host=%s port=5432 user=%s sslmode=disable", db.sock, db.user)},
		{"AGENTRT_TEST_REDIS", "unix://" + filepath.Join(db.sock, "redis.sock")},
		{"AGENTRT_TEST_REQUIRED", "1"},
	}
}

// RunTestDB is agentd testdb --dir <dir> --sock <dir> --out <file> --initdb
// <path> --postgres <path> --redis <path> [--write <dir>...] -- <argv>: it
// starts the test database in dir, its sockets in sock (each program inside
// agentd confine over the --write directories when there are any), records
// how that went in --out and, when it is up, runs argv with the database's
// variables, then stops the database, removes both directories and exits as
// argv did. A database that does not
// start ends it with setupFailed, and argv never runs.
func RunTestDB(args []string) int {
	i := slices.Index(args, "--")
	fset := flag.NewFlagSet(TestDBArg, flag.ContinueOnError)
	dir := fset.String("dir", "", "the database's directory")
	sock := fset.String("sock", "", "the directory of its sockets")
	out := fset.String("out", "", "where to write testdb.json")
	var t TestDBTools
	fset.StringVar(&t.Initdb, "initdb", "", "initdb")
	fset.StringVar(&t.Postgres, "postgres", "", "postgres")
	fset.StringVar(&t.Redis, "redis", "", "redis-server")
	var writes stringsFlag
	fset.Var(&writes, "write", "a directory the database's programs may write (repeated)")
	if i < 0 || i == len(args)-1 || fset.Parse(args[:i]) != nil || fset.NArg() > 0 || !filepath.IsAbs(*dir) || !filepath.IsAbs(*sock) || *out == "" ||
		t.Initdb == "" || t.Postgres == "" || t.Redis == "" {
		fmt.Fprintln(os.Stderr, "usage: agentd testdb --dir <dir> --sock <dir> --out <file> --initdb <path> --postgres <path> --redis <path> [--write <dir>...] -- <command> [args...]")
		return 2
	}
	argv := args[i+1:]
	path, err := exec.LookPath(argv[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentd testdb:", err)
		return 127
	}
	// The group's TERM reaches the executor and the database as well as
	// this stage, which goes on to stop the database once the executor has
	// ended. A caught signal is the default again in what it starts.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	ctx, cancel := context.WithTimeout(context.Background(), testDBLimit)
	db, rec := startTestDB(ctx, *dir, *sock, t, writes)
	cancel()
	if body, merr := json.MarshalIndent(rec, "", "  "); merr == nil {
		err = os.WriteFile(*out, body, 0o600)
	} else {
		err = merr
	}
	gone := func() error { return errors.Join(os.RemoveAll(*dir), emptyDir(*sock)) }
	if err != nil || !rec.Pass {
		db.stop()
		_ = gone()
		if err != nil {
			fmt.Fprintln(os.Stderr, "agentd testdb:", err)
			return 2
		}
		fmt.Fprintf(os.Stderr, "agentd testdb: %s; the executor did not start\n", rec.Reason)
		if n := len(rec.Commands); n > 0 {
			fmt.Fprintln(os.Stderr, rec.Commands[n-1].Tail)
		}
		return setupFailed
	}
	cmd := &exec.Cmd{Path: path, Args: argv, Env: os.Environ(), Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	for _, kv := range db.env() {
		cmd.Env = withEnv(cmd.Env, kv[0], kv[1])
	}
	if err = cmd.Start(); err != nil {
		db.stop()
		_ = gone()
		fmt.Fprintln(os.Stderr, "agentd testdb:", err)
		return 127
	}
	_ = cmd.Wait()
	db.stop()
	if err = gone(); err != nil {
		fmt.Fprintln(os.Stderr, "agentd testdb:", err)
	}
	signal.Stop(sigs)
	// Exit as the executor did: by its signal, for agentd to read the end
	// as it would have read the executor's own.
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		signal.Reset(ws.Signal())
		_ = syscall.Kill(os.Getpid(), ws.Signal())
		time.Sleep(time.Second)
		return 128 + int(ws.Signal())
	}
	return cmd.ProcessState.ExitCode()
}

// startTestDB makes and starts the database in dir: initdb, then Postgres
// and Redis, each up before the deadline, then the tests' roles. Its record
// has a trace per step, the first that failed its last.
func startTestDB(ctx context.Context, dir, sockDir string, t TestDBTools, writes []string) (*testDB, Verification) {
	rec := Verification{Commands: []CommandTrace{}}
	db := &testDB{dir: dir, sock: sockDir}
	fail := func(argv []string, why, tail string) (*testDB, Verification) {
		rec.Commands = append(rec.Commands, CommandTrace{Argv: argv, ExitCode: 1, Tail: tail})
		rec.Reason = why
		return db, rec
	}
	u, err := user.Current()
	if err != nil {
		return fail([]string{"user"}, "the user's name: "+err.Error(), "")
	}
	db.user = u.Username
	if err = os.RemoveAll(dir); err == nil {
		err = os.MkdirAll(dir, 0o700)
	}
	if err == nil {
		err = emptyDir(sockDir)
	}
	if err == nil {
		err = os.MkdirAll(sockDir, 0o700)
	}
	if err != nil {
		return fail([]string{"mkdir", dir, sockDir}, err.Error(), "")
	}
	// Postgres's postmaster refuses to start "multithreaded" without a
	// valid locale on macOS; C is valid everywhere.
	env := withEnv(AllowedEnv(os.Environ()), "LC_ALL", "C")
	pg := filepath.Join(dir, "pg")
	init, err := walledVerify(ctx, env, [][]string{{t.Initdb, "-D", pg, "-U", db.user, "-A", "trust", "-E", "UTF8", "--locale=C",
		"--no-sync", "--no-instructions"}}, writes, testDBLimit)
	if err != nil {
		return fail([]string{t.Initdb}, "initdb: "+err.Error(), "")
	}
	rec.Commands = append(rec.Commands, init.Commands...)
	if !init.Pass {
		rec.Reason = "initdb: " + init.Reason
		return db, rec
	}
	pgArgv := []string{t.Postgres, "-D", pg, "-k", sockDir, "-c", "listen_addresses=", "-c", "dynamic_shared_memory_type=mmap",
		"-c", "fsync=off", "-c", "synchronous_commit=off", "-c", "full_page_writes=off", "-c", "jit=off"}
	if db.postgres, db.pgDone, err = startIn(dir, "postgres.log", env, writes, pgArgv); err != nil {
		return fail(pgArgv, "postgres: "+err.Error(), "")
	}
	sock := filepath.Join(sockDir, "redis.sock")
	rdArgv := []string{t.Redis, "--port", "0", "--unixsocket", sock, "--unixsocketperm", "700", "--save", "", "--appendonly", "no",
		"--dir", dir, "--daemonize", "no"}
	if db.redis, db.rdDone, err = startIn(dir, "redis.log", env, writes, rdArgv); err != nil {
		return fail(rdArgv, "redis-server: "+err.Error(), "")
	}
	conninfo := db.env()[0][1] + " dbname=postgres connect_timeout=2"
	var conn *pgx.Conn
	for conn == nil {
		select {
		case <-db.pgDone:
			return fail(pgArgv, "postgres ended before it was up", logTail(dir, "postgres.log"))
		case <-db.rdDone:
			return fail(rdArgv, "redis-server ended before it was up", logTail(dir, "redis.log"))
		case <-ctx.Done():
			return fail(pgArgv, "postgres was not up within "+testDBLimit.String(), logTail(dir, "postgres.log"))
		case <-time.After(100 * time.Millisecond):
		}
		if c, cerr := pgx.Connect(ctx, conninfo); cerr == nil {
			conn = c
		}
	}
	defer func() { _ = conn.Close(context.Background()) }()
	for _, r := range testRoles {
		if _, err = conn.Exec(ctx, "CREATE ROLE "+pgx.Identifier{r}.Sanitize()+" LOGIN"); err != nil {
			return fail([]string{"CREATE ROLE", r}, err.Error(), "")
		}
	}
	for {
		if redisUp(sock) {
			break
		}
		select {
		case <-db.rdDone:
			return fail(rdArgv, "redis-server ended before it was up", logTail(dir, "redis.log"))
		case <-ctx.Done():
			return fail(rdArgv, "redis-server was not up within "+testDBLimit.String(), logTail(dir, "redis.log"))
		case <-time.After(100 * time.Millisecond):
		}
	}
	rec.Commands = append(rec.Commands, CommandTrace{Argv: pgArgv}, CommandTrace{Argv: rdArgv})
	rec.Pass = true
	return db, rec
}

// startIn starts argv, inside a wall over writes when there are any, in the
// stage's process group, its output to name in dir; done closes when it
// ends.
func startIn(dir, name string, env, writes, argv []string) (*exec.Cmd, chan struct{}, error) {
	run := argv
	if len(writes) > 0 {
		self, err := os.Executable()
		if err != nil {
			return nil, nil, err
		}
		run = append(wallArgv(self, writes), argv...)
	}
	log, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = log.Close() }()
	cmd := exec.Command(run[0], run[1:]...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, env, log, log
	if err = cmd.Start(); err != nil {
		return nil, nil, err
	}
	done := make(chan struct{})
	safego.Go("agentd-testdb-wait", func() { _ = cmd.Wait(); close(done) })
	return cmd, done, nil
}

// redisUp says whether Redis answers a PING on sock.
func redisUp(sock string) bool {
	c, err := net.DialTimeout("unix", sock, time.Second)
	if err != nil {
		return false
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err = c.Write([]byte("PING\r\n")); err != nil {
		return false
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	return err == nil && line == "+PONG\r\n"
}

// logTail is the end of a program's log in dir.
func logTail(dir, name string) string {
	b, err := readTail(filepath.Join(dir, name), tailBytes)
	if err != nil {
		return ""
	}
	return string(b)
}

// testDBStop is how long the database's programs have, together, to stop
// before they are killed: within agentd's grace for a group it ends.
var testDBStop = 8 * time.Second

// stop ends what started: Postgres by its fast shutdown and Redis by TERM,
// both told at once, and whatever has not stopped by one deadline killed
// (review td404-3 B2: one after the other, the two waits made 16 s).
func (db *testDB) stop() {
	if db == nil {
		return
	}
	type prog struct {
		cmd  *exec.Cmd
		done chan struct{}
	}
	var started []prog
	for _, p := range []struct {
		prog
		sig syscall.Signal
	}{{prog{db.postgres, db.pgDone}, syscall.SIGINT}, {prog{db.redis, db.rdDone}, syscall.SIGTERM}} {
		if p.cmd != nil {
			_ = p.cmd.Process.Signal(p.sig)
			started = append(started, p.prog)
		}
	}
	deadline := time.After(testDBStop)
	for _, p := range started {
		select {
		case <-p.done:
		case <-deadline:
			deadline = closedClock
			_ = p.cmd.Process.Kill()
			<-p.done
		}
	}
}

// closedClock is a deadline already past.
var closedClock = func() <-chan time.Time {
	c := make(chan time.Time)
	close(c)
	return c
}()

// emptyDir makes dir hold nothing, leaving dir itself (a sockets' directory
// under agentd's root, which the stage may write in but not beside).
func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		errs = append(errs, os.RemoveAll(filepath.Join(dir, e.Name())))
	}
	return errors.Join(errs...)
}

// removeTestDB removes what an attempt's test database left (an end that
// killed the stage leaves its directories): the database's directory in the
// attempt's output, and its sockets' under root, inside each only.
func removeTestDB(root, out string, logf func(format string, args ...any)) {
	if out == "" {
		return
	}
	if r, err := ownDirAt(root, out); err == nil {
		if err = removeIn(r, filepath.Join("tmp", "db")); err != nil {
			logf("agentd: the test database of %s: %v", out, err)
		}
		_ = r.Close()
	} else if !errors.Is(err, fs.ErrNotExist) {
		logf("agentd: %s: %v", out, err)
	}
	if root == "" {
		return
	}
	sock := testDBSockDir(root, out)
	s, err := openWithin(root, filepath.Dir(sock))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		logf("agentd: %s: %v", filepath.Dir(sock), err)
		return
	}
	defer func() { _ = s.Close() }()
	if err = removeIn(s, filepath.Base(sock)); err != nil {
		logf("agentd: the test database's sockets of %s: %v", out, err)
	}
}
