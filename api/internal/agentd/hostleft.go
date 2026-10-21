package agentd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// What agentd, a bench run or an exam sitting left running when the command
// that held the host lock died (review 487 A2). The lock is a file a live
// process holds, and goes with it; the process groups that command launched
// are each a group of their own (Launch) and outlive it. So each of the three
// records the groups it runs (agentd in its registry, an exam in its paper's
// sitting, a bench in its run's running), and whichever takes the lock next
// ends what those records name before it runs anything, or runs nothing
// while one is left: agentd at start and before each claim (Recover,
// Blocked), agentd bench and agentd exam once they hold the lock
// (LeftOnHost).

// RegistryPath is agentd's registry under root.
func RegistryPath(root string) string { return filepath.Join(root, "registry.json") }

// benchRunningName is the record, in a bench run's directory, of the process
// group its command runs now.
const benchRunningName = "running"

// benchRunning is that record: the group, its leader's start time and, when
// this system gives one, the leader's identity (procIdentity).
type benchRunning struct {
	PGID  int    `json:"pgid"`
	Start string `json:"start"`
	ID    string `json:"id,omitempty"`
}

// keepBenchRunning writes the record durably, as an exam's sitting is
// (keepSitting).
func keepBenchRunning(dir string, r benchRunning) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return writeDurably(filepath.Join(dir, benchRunningName), b)
}

// readBenchRunning reads a bench run's record; one that does not read is
// refused, for what that run left cannot be found by it.
func readBenchRunning(path string) (benchRunning, error) {
	var r benchRunning
	b, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if json.Unmarshal(b, &r) != nil || r.PGID <= 1 || r.Start == "" {
		return r, fmt.Errorf("agentd: %s is not a bench run's record: what that run left cannot be found; look and remove it by hand", path)
	}
	return r, nil
}

// endBenchRunning ends the group the record in dir names (endRecordedGroup)
// and removes the record once the group is gone; a group not known to be the
// run's keeps it, and the error says how to look.
func endBenchRunning(dir string, grace time.Duration) error {
	path := filepath.Join(dir, benchRunningName)
	r, err := readBenchRunning(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if state, pids := endRecordedGroup(r.PGID, r.Start, r.ID, grace); state != GroupCleared {
		return fmt.Errorf("agentd: the bench run recorded in %s had process group %d led by a process started %s; processes %v of "+
			"that group are left (%s), not known to be the run's. Do not end processes by that number alone: look (ps -o pid,lstart,args "+
			"-g %d); if they are the run's (its executor or checks, agentd launch), end them; if not, the record is stale: remove %s",
			path, r.PGID, r.Start, pids, state, r.PGID, path)
	}
	return os.Remove(path)
}

// endRecordedGroup ends the process group a record names and says whether it
// is gone. The group's leader is told by its identity (procIdentity), never
// by ps's start time alone, which two processes can share (review 419 B2):
//   - the process with the group's id is the recorded leader: the group is
//     ended (ClearGroup);
//   - it is another process: the system hands out no process id a group
//     still has, so the recorded group went before that process came;
//     nothing is signalled;
//   - no process has that id: the group is gone, or only processes of it are
//     left, which cannot be shown to be the recorded run's (ClearGroup says
//     which);
//   - the record has no identity, or the identity cannot be read now: the
//     group counts as gone only when no process is in it.
//
// A group left that is not known to be the run's is not touched.
func endRecordedGroup(pgid int, start, id string, grace time.Duration) (GroupState, []int) {
	now, err := procIdentity(pgid)
	_, serr := StartTime(pgid)
	leaderGone := serr != nil
	switch {
	case id != "" && err == nil && now == id:
		return ClearGroup(pgid, start, time.Time{}, grace)
	case id != "" && err == nil:
		return GroupCleared, nil
	case id != "" && leaderGone:
		return ClearGroup(pgid, start, time.Time{}, grace)
	case GroupAlive(pgid):
		pids, _ := groupMembers(pgid, 0)
		return GroupUnknown, pids
	}
	return GroupCleared, nil
}

// benchDirs are the run directories under root/bench; one that cannot be
// listed is an error, never "nothing left".
func benchDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "bench"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, en := range entries {
		if en.IsDir() {
			out = append(out, filepath.Join(root, "bench", en.Name()))
		}
	}
	return out, nil
}

// BenchRunsLeft ends what each bench run cut short left under root
// (endBenchRunning), and says why one could not be ended.
func BenchRunsLeft(root string, grace time.Duration) error {
	dirs, err := benchDirs(root)
	if err != nil {
		return err
	}
	var errs error
	for _, d := range dirs {
		errs = errors.Join(errs, endBenchRunning(d, grace))
	}
	return errs
}

// benchRecordsLeft are the bench runs' records under root that BenchRunsLeft
// could not end (or root/bench itself, when it cannot be listed).
func benchRecordsLeft(root string) []string {
	dirs, err := benchDirs(root)
	if err != nil {
		return []string{filepath.Join(root, "bench")}
	}
	var left []string
	for _, d := range dirs {
		if _, err := os.Lstat(filepath.Join(d, benchRunningName)); !errors.Is(err, fs.ErrNotExist) {
			left = append(left, filepath.Join(d, benchRunningName))
		}
	}
	return left
}

// LeftOnHost ends what a bench run or an exam sitting cut short left under
// root, and says what is still there that this command must not run beside:
// such a record that could not be ended, or an attempt of agentd's registry
// whose process group may still run (launching or running, or ended without
// its group known gone) and still does. Only agentd ends and hands over its
// attempts (Recover), so agentd bench and agentd exam, which run this once
// they hold the host lock, run nothing while one is left; the registry is
// only read.
func LeftOnHost(root string, grace time.Duration) error {
	errs := errors.Join(ExamRunsLeft(root, grace), BenchRunsLeft(root, grace))
	reg, err := OpenRegistry(RegistryPath(root))
	if err != nil {
		return errors.Join(errs, err)
	}
	entries, err := reg.Load()
	if err != nil {
		return errors.Join(errs, err)
	}
	for _, id := range sortedIDs(entries) {
		if e := entries[id]; attemptMayRun(e) {
			errs = errors.Join(errs, fmt.Errorf("agentd: attempt %s of agentd's registry %s is %s and its process group %d still runs: "+
				"start agentd, which ends and hands it over, and stop it again before this", id, RegistryPath(root), e.Phase, e.PGID))
		}
	}
	return errs
}

// attemptMayRun: an attempt whose group agentd did not show gone, a process
// of which still runs.
func attemptMayRun(e Entry) bool {
	pending := e.Phase == "launching" || e.Phase == "running" || e.Group == GroupRemains || e.Group == GroupUnknown
	return pending && GroupAlive(e.PGID)
}
