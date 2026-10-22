package controller_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-415, review 497 A4: a host's disk going below the floor, and back
// above, are paged to the Owner by the controller: a page the relay failed
// is tried again on the next pass, one it took (or had no target for) is
// marked and never sent again.
func TestDiskPagesPageEachRecordOnce(t *testing.T) {
	pool := setup(t)
	const gib = int64(1) << 30
	record := func(d store.HostDisk) {
		t.Helper()
		if _, err := store.RecordHostDisk(ctx, pool, "h1", d, 0); err != nil {
			t.Fatal(err)
		}
	}
	var titles, bodies []string
	result := ""
	c := &controller.Controller{DB: pool, PageOwner: func(_ context.Context, title, body, _ string) (string, error) {
		if result == "" {
			return "", errors.New("the relay is away")
		}
		titles, bodies = append(titles, title), append(bodies, body)
		return result, nil
	}}
	pass := func(want int, wantErr bool) {
		t.Helper()
		n, err := c.DiskPages(ctx)
		if n != want || (err != nil) != wantErr {
			t.Fatalf("pass: %d marked, %v; want %d, an error %v", n, err, want, wantErr)
		}
	}
	record(store.HostDisk{Host: 100 * gib, Root: gib})
	pass(0, true)
	result = "accepted"
	pass(1, false)
	pass(0, false)
	if len(titles) != 1 || titles[0] != "Host h1 is low on disk" || !strings.Contains(bodies[0], "given no new work") {
		t.Fatalf("pages %q %q", titles, bodies)
	}
	result = "skipped"
	record(store.HostDisk{Host: 100 * gib, Root: 100 * gib})
	pass(1, false)
	pass(0, false)
	if len(titles) != 2 || titles[1] != "Host h1 has room on disk again" || !strings.Contains(bodies[1], "other conditions") {
		t.Fatalf("pages %q %q", titles, bodies)
	}
}
