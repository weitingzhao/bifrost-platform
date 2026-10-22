package agentd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Review 497 A1: a platform that refused the claim's free disk (from before
// TD-415) has it left out of the claims for DiskRetryAfter only; the claim
// after tries it again, so once the platform is released the host reports
// its disk again without a restart.
func TestARefusedDiskIsTriedAgainLater(t *testing.T) {
	was := DiskRetryAfter
	DiskRetryAfter = 50 * time.Millisecond
	t.Cleanup(func() { DiskRetryAfter = was })
	f := newFake()
	d := fakeDaemon(t, f)
	f.set("claim", func(n int) (int, string) {
		if n == 0 {
			return 400, `{"error":"bad_request","message":"body: json: unknown field \"disk_free_bytes\""}`
		}
		return 204, ""
	})
	disk := &Disk{Host: 1, Root: 1}
	carries := func(i int) bool {
		t.Helper()
		var b map[string]any
		if err := json.Unmarshal(f.sent("claim")[i], &b); err != nil {
			t.Fatal(err)
		}
		return b["disk_free_bytes"] != nil
	}
	for i := 0; i < 2; i++ {
		if _, err := d.Client.ClaimWith(bg, 1, nil, disk); err != nil {
			t.Fatal(err)
		}
	}
	if !carries(0) || carries(1) || carries(2) {
		t.Fatal("the old platform's refusal: the claim is sent again without the disk, and the next one too")
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := d.Client.ClaimWith(bg, 1, nil, disk); err != nil {
		t.Fatal(err)
	}
	if !carries(3) {
		t.Fatal("after DiskRetryAfter the claim does not carry the disk again")
	}
}

// Reviews 497 A2, 499 A-1: an attempt a takeover ends is settled whatever
// the platform has of it; its workspace goes only when its last sample,
// taken at the takeover, is whole and the platform's. A log tail or a patch
// the platform did not take (for now or for good), a patch too large to
// take, and an attempt without a repo (no patch of it is taken) leave the
// workspace and the log, the only copies, as they are.
func TestARestartsLastSampleDecidesTheWorkspace(t *testing.T) {
	for _, c := range []struct {
		name, refuse string
		status       int
		big, noRepo  bool
		taken        bool
	}{
		{name: "taken", taken: true},
		{name: "log unavailable", refuse: "put:log_tail", status: 503},
		{name: "log refused", refuse: "put:log_tail", status: 400},
		{name: "patch refused", refuse: "put:checkpoint_patch", status: 400},
		{name: "patch too large", big: true},
		{name: "no repo", noRepo: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFake()
			d := fakeDaemon(t, f)
			d.KeepWorkspaces = false
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			var e Entry
			if c.noRepo {
				e, _ = entryFor(t, d, "a1", "restarted")
				must(os.MkdirAll(e.Workdir, 0o700))
			} else {
				e, _, _, _, _ = unchangedEntry(t, d, "restarted")
			}
			change := []byte("package x\n")
			if c.big {
				change = bytes.Repeat([]byte("// a line the executor wrote\n"), 2*patchLimit/29)
			}
			must(os.WriteFile(filepath.Join(e.Workdir, "x.go"), change, 0o600))
			log := filepath.Join(e.Workdir+".out", "log.txt")
			must(os.WriteFile(log, []byte("only here"), 0o600))
			if c.refuse != "" {
				f.set(c.refuse, always(c.status, "refused"))
			}
			must(d.takeOver(bg, &e))
			_, lerr := os.Stat(log)
			entries, rerr := os.ReadDir(e.Workdir)
			_, xerr := os.Stat(filepath.Join(e.Workdir, "x.go"))
			switch {
			case !c.taken && (lerr != nil || xerr != nil):
				t.Fatalf("not whole: the log %v, the change %v", lerr, xerr)
			case c.taken && (!errors.Is(lerr, fs.ErrNotExist) || rerr != nil || len(entries) != 0):
				t.Fatalf("whole and taken: the log %v, the workspace %d entries %v", lerr, len(entries), rerr)
			}
			if _, ok := registered(t, d, e.AttemptID); ok {
				t.Fatal("the entry is left")
			}
		})
	}
}

// Review 499 A-1: an attempt that failed before its executor started gives
// up its workspace only when why it failed, and its log's tail, were taken
// whole; a report or a log tail the platform refused leaves them as they are.
func TestAFailureNotHandedInKeepsItsWorkspace(t *testing.T) {
	for _, c := range []struct {
		name, refuse string
		taken        bool
	}{
		{name: "taken", taken: true},
		{name: "report refused", refuse: "put:report"},
		{name: "log refused", refuse: "put:log_tail"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFake()
			d := fakeDaemon(t, f)
			d.KeepWorkspaces = false
			e, a := entryFor(t, d, "prep", "launching")
			for _, p := range []string{filepath.Join(e.Workdir, "kept.txt"), filepath.Join(a.Out, "log.txt")} {
				if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("only here"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if c.refuse != "" {
				f.set(c.refuse, always(400, "refused"))
			}
			l := &lease{lost: make(chan struct{}), stop: make(chan struct{}), cancel: func() {}, done: make(chan struct{})}
			if _, err := d.failBefore(bg, a, nil, &e, l, errors.New("the setup failed")); err == nil {
				t.Fatal("the failure is not returned")
			}
			_, kerr := os.Stat(filepath.Join(e.Workdir, "kept.txt"))
			if c.taken != errors.Is(kerr, fs.ErrNotExist) {
				t.Fatalf("taken %v: the workspace's file %v", c.taken, kerr)
			}
		})
	}
}

// Review 497 A3: only the workspace an entry's task and attempt make is ever
// removed, through a root nothing can leave: a Workdir that reaches it
// through a link and "..", and a task directory that is a link, leave what
// lies outside as it is.
func TestAnOddWorkdirRemovesNothingOutside(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	for _, p := range []string{filepath.Join(root, "tasks", "t_one"), filepath.Join(outside, "child"), filepath.Join(outside, "a2"), filepath.Join(outside, "a2.out"),
		filepath.Join(outside, "a1", "keep"), filepath.Join(outside, "a1.out")} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "child"), filepath.Join(root, "tasks", "t_one", "link")); err != nil {
		t.Fatal(err)
	}
	odd := &Entry{TaskID: "t_one", AttemptNo: 2, Workdir: filepath.Join(root, "tasks", "t_one") + "/link/../a2"}
	if err := removeWorkspace(root, odd); !errors.Is(err, errNotWorkspace) {
		t.Fatalf("an odd Workdir: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "tasks", "t_two")); err != nil {
		t.Fatal(err)
	}
	linked := &Entry{TaskID: "t_two", AttemptNo: 1, Workdir: WorkspaceDir(root, "t_two", 1)}
	if err := removeWorkspace(root, linked); err == nil {
		t.Fatal("a task directory that is a link was gone through")
	}
	for _, p := range []string{"a2", "a2.out", filepath.Join("a1", "keep"), "a1.out"} {
		if _, err := os.Stat(filepath.Join(outside, p)); err != nil {
			t.Fatalf("%s outside the root: %v", p, err)
		}
	}
}

// Review 497 B2: a settled attempt whose workspace cannot be emptied stays
// in the registry as unswept, never run again, and Recover empties it once
// it can, then forgets the entry.
func TestAWorkspaceNotEmptiedIsTriedAgain(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	d.KeepWorkspaces = false
	e, _ := entryFor(t, d, "stuck", "ending")
	e.HandedOver = true
	// The workspace's place holds a file, not a directory: it cannot be
	// emptied as one.
	if err := os.WriteFile(e.Workdir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.settle(&e); err != nil {
		t.Fatal(err)
	}
	if got, ok := registered(t, d, "stuck"); !ok || got.Phase != "unswept" {
		t.Fatalf("the entry: %+v %v", got, ok)
	}
	if err := os.Remove(e.Workdir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.Workdir, "node_modules", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(e.Workdir); err != nil || len(entries) != 0 {
		t.Fatalf("the workspace after Recover: %d entries %v", len(entries), err)
	}
	if _, ok := registered(t, d, "stuck"); ok {
		t.Fatal("the entry is left after its workspace was emptied")
	}
	if n := len(f.sent("ended")); n != 0 {
		t.Fatalf("the unswept attempt was ended again (%d)", n)
	}
}
