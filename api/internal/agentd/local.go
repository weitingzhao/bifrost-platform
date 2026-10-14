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
	Epoch     int64  `json:"lease_epoch"`
	Workdir   string `json:"workdir"`
	PGID      int    `json:"pgid"`
	Start     string `json:"start"`
	// Phase: running (the executor runs), computed (it exited; the result is
	// here), persisted, submitted, ended.
	Phase string `json:"phase"`
	// CheckpointSeq is the last checkpoint number issued; Confirmed the last
	// the platform confirmed (review 142 R2): a retry of one sample reuses
	// its number.
	CheckpointSeq int    `json:"checkpoint_seq"`
	Confirmed     int    `json:"confirmed"`
	ExitCode      *int   `json:"exit_code,omitempty"`
	EndHow        string `json:"end_how,omitempty"`
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
