package agentd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

// attemptDirName is an attempt's directory under its task's (WorkspaceDir).
var attemptDirName = regexp.MustCompile(`^a[0-9]+$`)

// removeWorkspace empties an ended attempt's workspace and removes its
// output under root once the platform has its end and its group is gone
// (settle, Recover; TD-415: every attempt's checkout, modules and builds
// stayed on the host). The workspace's own directory stays, empty: an attempt
// never runs twice (rule R2), and its directory being there is how a second
// run of it is refused (ErrWorkspaceExists). Only a directory of the shape
// WorkspaceDir makes is touched, and nothing below a link on the way; what
// cannot be removed is logged and left, never a reason to keep the entry.
// Daemon.KeepWorkspaces keeps them as they were instead: for a look on the
// host, and for tests that read what an attempt left.
func removeWorkspace(root, workdir string, logf func(format string, args ...any)) {
	if root == "" || workdir == "" {
		return
	}
	rel, err := filepath.Rel(root, workdir)
	parts := strings.Split(rel, string(filepath.Separator))
	if err != nil || len(parts) != 3 || parts[0] != "tasks" || !taskName.MatchString(parts[1]) || !attemptDirName.MatchString(parts[2]) {
		logf("agentd: %s is not an attempt's workspace under %s; it is left", workdir, root)
		return
	}
	for _, d := range []string{filepath.Join(root, "tasks"), filepath.Join(root, "tasks", parts[1]), workdir} {
		info, err := os.Lstat(d)
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		if err != nil || !info.IsDir() {
			logf("agentd: %s is not a directory (%v); the workspace %s is left", d, err, workdir)
			return
		}
	}
	entries, err := os.ReadDir(workdir)
	if err != nil {
		logf("agentd: the workspace of an ended attempt, %s: %v", workdir, err)
		return
	}
	for _, e := range entries {
		if err = forceRemove(filepath.Join(workdir, e.Name())); err != nil {
			logf("agentd: the workspace of an ended attempt, %s: %v", workdir, err)
		}
	}
	if err = forceRemove(workdir + ".out"); err != nil {
		logf("agentd: the output of an ended attempt, %s.out: %v", workdir, err)
	}
}
