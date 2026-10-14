// Package agentd is the agent host daemon of the agent runtime (design v5
// §5.2 「agentd」): it claims work from the platform, runs each attempt's
// executor in its own process group, keeps the lease, uploads checkpoints and
// results, and takes over what a previous run of itself left. It opens no
// port. This file is what it keeps on the host: the machine-wide lock and the
// registry of the process groups it started.
package agentd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
)

// ErrHeld: another agentd holds this host's lock.
var ErrHeld = errors.New("agentd: another agentd runs on this host")

// HostLock is the machine-wide lock: one agentd per host (v5 §5.2 本地登记).
// The kernel drops it when the process dies, however it dies.
type HostLock struct{ f *os.File }

// HostLockPath is the host lock's one place, whatever root a run of agentd
// uses (review 163 A9): ~/.config/bifrost/agentd.lock.
func HostLockPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "bifrost", "agentd.lock"), nil
}

// LockHost takes the lock at path, or returns ErrHeld at once.
func LockHost(path string) (*HostLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrHeld
		}
		return nil, err
	}
	return &HostLock{f: f}, nil
}

// Release gives the lock up.
func (l *HostLock) Release() error { return l.f.Close() }

// Entry is one attempt agentd runs or ran on this host: its process group,
// identified by the group id and the leader's start time (a group id can be
// reused), and how far its result has gone (v5 §5.2 本地登记、检查点、收尾).
type Entry struct {
	AttemptID string `json:"attempt_id"`
	TaskID    string `json:"task_id"`
	AttemptNo int    `json:"attempt_no"`
	Epoch     int64  `json:"lease_epoch"`
	Role      string `json:"role"`
	Repo      string `json:"repo,omitempty"`
	Effect    string `json:"effect_class"`
	PushedRef string `json:"pushed_ref,omitempty"`
	WorkID    string `json:"work_id,omitempty"`
	Workdir   string `json:"workdir"`
	Base      string `json:"base,omitempty"` // the commit the workspace was checked out at
	PGID      int    `json:"pgid"`
	Start     string `json:"start"`
	// Phase: launching, running (the executor runs), exited (it exited; the
	// result is here), computed, persisted, submitting (the submit is being
	// sent; it is resent as it is), submitted; lost (salvage only);
	// restarted (a previous agentd's group, ended at takeover); ending (the
	// ended request is written); uncleared (ended was reported, but the
	// group is not gone: kept until it is, and the host takes no new work).
	Phase string `json:"phase"`
	// CheckpointSeq is the last checkpoint number issued; Confirmed the last
	// the platform confirmed (review 142 R2). Pending is the sample issued
	// and not yet confirmed: its request is resent as it is until it is, and
	// only then is the next sample taken (review 164 A6).
	CheckpointSeq int                `json:"checkpoint_seq"`
	Confirmed     int                `json:"confirmed"`
	Pending       *PendingCheckpoint `json:"pending_checkpoint,omitempty"`
	ExitCode      *int               `json:"exit_code,omitempty"`
	EndHow        string             `json:"end_how,omitempty"`
	// Group is what is known of the process group once it was ended (review
	// 164 A2).
	Group GroupState `json:"group,omitempty"`
	// The hand-over's journal (review 163 A3–A5): the conclusion sent with
	// computed, and the result sent with persist or salvage, written before
	// they are sent. LogTailDone and ReportDone: that upload was taken, or
	// refused for good (review 164 A4).
	Conclusion   string   `json:"conclusion,omitempty"`
	LogTailDone  bool     `json:"log_tail_done,omitempty"`
	ReportDone   bool     `json:"report_done,omitempty"`
	Prepared     bool     `json:"prepared,omitempty"`
	Report       string   `json:"report,omitempty"`
	Artifacts    []string `json:"artifacts,omitempty"`
	PushedCommit string   `json:"pushed_commit,omitempty"`
	ChangeID     string   `json:"change_id,omitempty"`
	// EndRequest is the ended request, written before it is first sent and
	// resent as it is (review 164 A2).
	EndRequest *EndRequest `json:"end_request,omitempty"`
}

// PendingCheckpoint is a checkpoint request: its number, the artifacts it
// names and whether the patch was too large.
type PendingCheckpoint struct {
	Seq       int      `json:"seq"`
	Artifacts []string `json:"artifacts,omitempty"`
	TooLarge  bool     `json:"too_large,omitempty"`
}

// EndRequest is the ended request.
type EndRequest struct {
	EndHow  string `json:"end_how"`
	Cleared bool   `json:"group_cleared"`
}

// Registry is the file of entries, by attempt. Every change is written to a
// new file, synced, and renamed over the old one, and the directory synced,
// so a crash leaves the old file or the new one, never part of either.
type Registry struct {
	path string
	mu   sync.Mutex
}

// OpenRegistry uses the file at path; it need not exist yet.
func OpenRegistry(path string) (*Registry, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return &Registry{path: path}, nil
}

// Load reads every entry.
func (r *Registry) Load() (map[string]Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.load()
}

func (r *Registry) load() (map[string]Entry, error) {
	out := map[string]Entry{}
	b, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Entry
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("agentd: registry %s: %w", r.path, err)
	}
	for _, e := range list {
		out[e.AttemptID] = e
	}
	return out, nil
}

// Put writes e (by its attempt), durably, before it returns.
func (r *Registry) Put(e Entry) error {
	return r.change(func(m map[string]Entry) { m[e.AttemptID] = e })
}

// Delete removes the entry of an attempt.
func (r *Registry) Delete(attemptID string) error {
	return r.change(func(m map[string]Entry) { delete(m, attemptID) })
}

func (r *Registry) change(fn func(map[string]Entry)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, err := r.load()
	if err != nil {
		return err
	}
	fn(m)
	list := make([]Entry, 0, len(m))
	for _, e := range m {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].AttemptID < list[j].AttemptID })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeDurably(r.path, b)
}

func writeDurably(path string, b []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
