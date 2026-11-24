package unitplan

import (
	"strings"
	"testing"
)

// TD-588 (2026-11-23): the console's ui pin lagged ui main on 11-21 and 11-23,
// and each daily unit run until someone moved it reported a regression that
// was not there. A pin with advance stg is planned at the commit the follower
// gives, the STG release's; with no follower, or none it can give, at the
// line's commit, and the plan says which.
func TestAPinWithAdvanceSTGIsPlannedAtTheFollowersCommit(t *testing.T) {
	line, released := strings.Repeat("1", 40), strings.Repeat("2", 40)
	files, ui := planFiles()
	files[ManifestPath] = strings.Replace(manifestDoc, "advance: manual", "advance: stg", 1)
	var asked []string
	src := Source{Pinned: func(repo, at string) (*Tree, error) { asked = append(asked, at); return testTree(t, ui), nil }}
	build := func() *Plan {
		t.Helper()
		asked = nil
		plan, err := Build("r", strings.Repeat("c", 40), testTree(t, files), src)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	t.Cleanup(func() { SetFollower(nil) })

	SetFollower(func(repo string) (string, string) {
		if repo != "ui" {
			t.Errorf("asked for %q", repo)
		}
		return released, "stg release bifrost-deliver-platform-1"
	})
	p := build()
	if p.Pins[0].Commit != released || asked[0] != released || !strings.Contains(p.Pins[0].Note, "bifrost-deliver-platform-1") {
		t.Fatalf("followed pin: %+v, trees read at %v", p.Pins[0], asked)
	}

	SetFollower(func(string) (string, string) { return "", "" })
	if p := build(); p.Pins[0].Commit != line || !strings.Contains(p.Pins[0].Note, "could not be read") {
		t.Fatalf("a follower that cannot say: %+v", p.Pins[0])
	}
	SetFollower(nil)
	if p := build(); p.Pins[0].Commit != line || !strings.Contains(p.Pins[0].Note, "no follower") {
		t.Fatalf("no follower: %+v", p.Pins[0])
	}

	// Control: a manual pin never asks the follower.
	files[ManifestPath] = manifestDoc
	SetFollower(func(string) (string, string) { t.Error("a manual pin asked the follower"); return released, "" })
	if p := build(); p.Pins[0].Commit != line || p.Pins[0].Note != "" {
		t.Fatalf("manual pin: %+v", p.Pins[0])
	}
}

func TestAnAdvanceThatIsNeitherManualNorSTGIsRefused(t *testing.T) {
	if _, err := ParseManifest([]byte(strings.Replace(manifestDoc, "advance: manual", "advance: nightly", 1))); err == nil {
		t.Fatal("advance nightly was taken")
	}
}
