package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// TD-383: the cleanup reads every attempt branch of a repository from the
// same advertisement the result check reads, and only those; an
// advertisement that is not whole is an error, as for one branch.
func TestGiteaAgentHeadsListsTheAttemptBranches(t *testing.T) {
	f := &fakeGitea{adverts: agentsRepo(advert(otherSHA+" HEAD", otherSHA+" refs/heads/main", headSHA+" refs/heads/agent/T-7/a1",
		otherSHA+" refs/heads/agent/T-7/a2", headSHA+" refs/tags/v1", otherSHA+" refs/tags/v1^{}", headSHA+" refs/heads/agentish"))}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	access := func(context.Context) (delivery.GiteaAccess, error) {
		return delivery.GiteaAccess{Base: srv.URL + "/", Org: "bifrost", User: "reader", Pass: "secret"}, nil
	}
	read := giteaAgentHeads(access, "bifrost-agents", srv.Client())
	got, err := read(context.Background(), "bifrost-platform")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Ref < got[j].Ref })
	want := []store.AgentHead{{Repo: "bifrost-platform", Ref: "refs/heads/agent/T-7/a1", Commit: headSHA},
		{Repo: "bifrost-platform", Ref: "refs/heads/agent/T-7/a2", Commit: otherSHA}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("heads = %+v; want %+v", got, want)
	}
	f.adverts = agentsRepo(advert(headSHA+" refs/heads/agent/T-7/a1") + "0000")
	if _, err = read(context.Background(), "bifrost-platform"); err == nil {
		t.Fatal("an advertisement with more after its last flush was read")
	}
	if _, err = read(context.Background(), "../etc"); !errors.Is(err, store.ErrState) {
		t.Fatalf("a name that is not a repository: %v", err)
	}
}

// A read of main is given again only while main's head, read every time, is
// where it was and the read is within its time to live; a head that moved
// (a revert pushed since) reads main anew; a head or a read that fails is no
// read, though one was kept (review 437 A1).
func TestCachedMainResultsFollowsMainsHead(t *testing.T) {
	reads, fail, head, headFails := 0, false, headSHA, false
	read := cachedMainResults(func(context.Context, string) (store.OnMain, error) {
		reads++
		if fail {
			return nil, errors.New("gitea unreachable")
		}
		return store.OnMain{"t_x": {headSHA: otherSHA}}, nil
	}, func(context.Context, string) (string, error) {
		if headFails {
			return "", errors.New("gitea unreachable")
		}
		return head, nil
	}, time.Hour)
	for i := 0; i < 3; i++ {
		if got, ok := read(context.Background(), "bifrost-platform"); !ok || got["t_x"][headSHA] != otherSHA || reads != 1 {
			t.Fatalf("read %d: %v %v, %d reads", i, got, ok, reads)
		}
	}
	head = otherSHA // a revert pushed
	if _, ok := read(context.Background(), "bifrost-platform"); !ok || reads != 2 {
		t.Fatalf("after main moved: ok %v, %d reads; want main read again", ok, reads)
	}
	headFails = true
	if _, ok := read(context.Background(), "bifrost-platform"); ok {
		t.Fatal("main's head out of reach gave the kept read")
	}
	headFails, fail = false, true
	if _, ok := read(context.Background(), "bifrost-ui"); ok || reads != 3 {
		t.Fatalf("a failed read: ok %v, %d reads; want one read per repository", ok, reads)
	}
	if _, ok := read(context.Background(), "bifrost-ui"); ok || reads != 4 {
		t.Fatalf("a failed read is not kept: %d reads", reads)
	}
}
