package cirunview

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
)

// The run view's answer is kept a short while. Origin: incident #63
// (2026-11-11), where a Console page's own reads starved the platform; this
// route reads a project's runs and all their tasks from the cluster.
func TestUnitRunsAreKeptAShortWhile(t *testing.T) {
	t.Parallel()
	reads := 0
	fail := false
	read := func(_ context.Context, repo string, limit int) ([]landingci.RunView, error) {
		reads++
		if fail {
			return nil, errors.New("cluster away")
		}
		return []landingci.RunView{{Name: repo + "-run", Tasks: []landingci.TaskView{}}}, nil
	}
	var c Cache
	t0 := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	a, err := c.Read(context.Background(), read, t0, "proj", 30)
	if err != nil || reads != 1 || a.Repo != "proj" || a.Runs[0].Name != "proj-run" || a.AsOf != "2026-11-14T12:00:00Z" {
		t.Fatalf("first: %+v %v reads %d", a, err, reads)
	}
	// Within the time it is kept: no second read, and the answer says when it was read.
	if b, err := c.Read(context.Background(), read, t0.Add(Keep-time.Second), "proj", 30); err != nil || reads != 1 || b.AsOf != a.AsOf {
		t.Fatalf("kept: %+v %v reads %d", b, err, reads)
	}
	// Another project, or another limit, is another answer.
	if _, err := c.Read(context.Background(), read, t0, "other", 30); err != nil || reads != 2 {
		t.Fatalf("another project: %v reads %d", err, reads)
	}
	if _, err := c.Read(context.Background(), read, t0, "proj", 5); err != nil || reads != 3 {
		t.Fatalf("another limit: %v reads %d", err, reads)
	}
	// After it: read again; a failed read is an error and is not kept.
	fail = true
	if _, err := c.Read(context.Background(), read, t0.Add(Keep), "proj", 30); err == nil || reads != 4 {
		t.Fatalf("a failed read: %v reads %d", err, reads)
	}
	fail = false
	if b, err := c.Read(context.Background(), read, t0.Add(Keep), "proj", 30); err != nil || reads != 5 || b.AsOf == a.AsOf {
		t.Fatalf("after a failed read: %+v %v reads %d", b, err, reads)
	}
}
