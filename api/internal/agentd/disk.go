package agentd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Disk is the free disk a claim reports (TD-415): the bytes free to an
// unprivileged process on the volume the host's root is on and on the one
// agentd's root (AGENTD_ROOT) is on. The platform gives the host no new work
// while either is below its floor.
type Disk struct {
	Host, Root int64
}

// MeasureDisk reads the free disk of / and of root.
func MeasureDisk(root string) (Disk, error) {
	host, err := freeBytes("/")
	if err != nil {
		return Disk{}, err
	}
	r, err := freeBytes(root)
	if err != nil {
		return Disk{}, err
	}
	return Disk{Host: host, Root: r}, nil
}

func freeBytes(path string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("agentd: free disk of %s: %w", path, err)
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// removeWorkspace empties a settled attempt's workspace and removes its
// output under root (settle, Recover; TD-415: every attempt's checkout,
// modules and builds stayed on the host). The workspace's own directory
// stays, empty: an attempt never runs twice (rule R2), and its directory
// being there is how a second run of it is refused (ErrWorkspaceExists).
// Only the workspace the entry's task and attempt make (WorkspaceDir) is
// touched, the entry's Workdir exactly that, no directory on the way a link,
// and everything is removed through an os.Root at root, which no name can
// leave (review 497 A3). Daemon.KeepWorkspaces keeps them instead: for a look
// on the host, and for the tests that read what an attempt left.
func removeWorkspace(root string, e *Entry) error {
	if root == "" || !taskName.MatchString(e.TaskID) || e.AttemptNo < 1 || e.Workdir != WorkspaceDir(root, e.TaskID, e.AttemptNo) {
		return fmt.Errorf("%w: %q is not attempt %d of %s's workspace under %s; it is left", errNotWorkspace, e.Workdir, e.AttemptNo, e.TaskID, root)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	task := filepath.Join("tasks", e.TaskID)
	dir := filepath.Join(task, filepath.Base(e.Workdir))
	for _, d := range []string{"tasks", task, dir} {
		info, err := r.Lstat(d)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil || !info.IsDir() {
			return fmt.Errorf("agentd: %s is not a directory (%v); the workspace %s is left", d, err, e.Workdir)
		}
	}
	if err = r.Chmod(dir, 0o700); err != nil {
		return err
	}
	f, err := r.Open(dir)
	if err != nil {
		return err
	}
	entries, err := f.ReadDir(-1)
	_ = f.Close()
	if err != nil {
		return err
	}
	var errs []error
	for _, en := range entries {
		errs = append(errs, rootRemove(r, filepath.Join(dir, en.Name())))
	}
	errs = append(errs, rootRemove(r, dir+".out"))
	return errors.Join(errs...)
}

// rootRemove removes name under r and all below it, as forceRemove does a
// path: a directory is made writable first (Go's module cache is read only),
// a link is removed, never followed.
func rootRemove(r *os.Root, name string) error {
	info, err := r.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err = r.Chmod(name, 0o700); err != nil {
			return err
		}
		f, err := r.Open(name)
		if err != nil {
			return err
		}
		entries, err := f.ReadDir(-1)
		_ = f.Close()
		if err != nil {
			return err
		}
		for _, en := range entries {
			if err = rootRemove(r, filepath.Join(name, en.Name())); err != nil {
				return err
			}
		}
	}
	if err = r.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// errNotWorkspace: an entry's Workdir is not its attempt's workspace; nothing
// is removed, and trying again would not change that.
var errNotWorkspace = errors.New("agentd")

// sweep empties a settled attempt's workspace once what it left that matters
// is the platform's (HandedOver) and says whether the entry is to stay as
// unswept: emptying it failed, and Recover tries again later, never running
// the attempt again (review 497 B2). An attempt not handed over, a Workdir
// that is not its workspace, and KeepWorkspaces leave everything as it is.
func (d *Daemon) sweep(e *Entry) (unswept bool) {
	if !e.HandedOver || d.KeepWorkspaces {
		return false
	}
	err := removeWorkspace(d.Root, e)
	if err != nil {
		d.logf("agentd: the workspace of %s: %v", e.AttemptID, err)
	}
	return err != nil && !errors.Is(err, errNotWorkspace)
}
