package agentd

import (
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

// Review 497 A2: an attempt a takeover ends is settled whatever the platform
// has of it; its workspace goes only when its last sample, taken at the
// takeover, is the platform's. When the log's upload fails, the output
// (the only copy of it) stays; when it is taken, the workspace is emptied.
func TestARestartsLastSampleDecidesTheWorkspace(t *testing.T) {
	for _, taken := range []bool{false, true} {
		f := newFake()
		d := fakeDaemon(t, f)
		d.KeepWorkspaces = false
		e, _ := entryFor(t, d, "restart", "restarted")
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(os.MkdirAll(filepath.Join(e.Workdir, "build"), 0o700))
		log := filepath.Join(e.Workdir+".out", "log.txt")
		must(os.WriteFile(log, []byte("only here"), 0o600))
		if !taken {
			f.set("put:log_tail", always(503, "unavailable"))
		}
		must(d.takeOver(bg, &e))
		_, lerr := os.Stat(log)
		entries, rerr := os.ReadDir(e.Workdir)
		switch {
		case !taken && (lerr != nil || len(entries) != 1):
			t.Fatalf("a sample not taken: the log %v, the workspace %d entries %v", lerr, len(entries), rerr)
		case taken && (!errors.Is(lerr, fs.ErrNotExist) || rerr != nil || len(entries) != 0):
			t.Fatalf("a sample taken: the log %v, the workspace %d entries %v", lerr, len(entries), rerr)
		}
		if _, ok := registered(t, d, "restart"); ok {
			t.Fatalf("taken %v: the entry is left", taken)
		}
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
