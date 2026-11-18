package nextgen

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// root is the repository, four directories up from this package.
func root(t *testing.T) string {
	t.Helper()
	r, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r, "api", "go.mod")); err != nil {
		t.Fatalf("not the repository root: %s", r)
	}
	return r
}

// goFiles is every Go file of the module but this package's own.
func goFiles(t *testing.T, r string) map[string]string {
	t.Helper()
	out := map[string]string{}
	self, _ := filepath.Abs(".")
	err := filepath.WalkDir(filepath.Join(r, "api"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == self {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[p] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// 2026-11-17 (the Owner: nothing in Ops may be kept by hand; what it shows
// is computed from the source). The disposition table is the design's 153
// rows, each disposition as the design counted them; a channel marked
// retired must be gone from the code (its paths absent, its text in no Go
// file), so the Bus page's "now" cannot run ahead of the code.
func TestRetiredChannelsAreGone(t *testing.T) {
	chs, err := Channels()
	if err != nil {
		t.Fatal(err)
	}
	if len(chs) != 153 {
		t.Fatalf("%d channels, the table has 153", len(chs))
	}
	want := map[string]int{"replaced": 58, "merged": 20, "kept": 31, "announced": 27, "deleted": 12, "not-this-way": 4, "reserved": 1}
	got := map[string]int{}
	seen := map[string]bool{}
	for _, c := range chs {
		got[c.Disposition]++
		if seen[c.ID] {
			t.Errorf("%s twice", c.ID)
		}
		seen[c.ID] = true
	}
	for d, n := range want {
		if got[d] != n {
			t.Errorf("%s: %d, the table has %d", d, got[d], n)
		}
	}
	r := root(t)
	files := goFiles(t, r)
	for _, c := range chs {
		x := c.Retired
		if x == nil {
			continue
		}
		if _, err := time.Parse("2006-01-02", x.At); err != nil || x.Change == "" {
			t.Errorf("%s: retired needs a date and a change", c.ID)
		}
		if x.Outside != "" {
			continue
		}
		if len(x.Absent) == 0 && len(x.AbsentText) == 0 {
			t.Errorf("%s: retired with nothing that proves it", c.ID)
		}
		for _, p := range x.Absent {
			if _, err := os.Stat(filepath.Join(r, p)); err == nil {
				t.Errorf("%s is marked retired but %s is still there", c.ID, p)
			}
		}
		for _, text := range x.AbsentText {
			for f, body := range files {
				if strings.Contains(body, text) {
					t.Errorf("%s is marked retired but %s still has %q", c.ID, f, text)
				}
			}
		}
	}
}

// 2026-11-17: the source switch's readers stand as readers.yaml says: a
// moved reader's proof is in its file, a retired one's text is in no Go
// file, an open one names whose it is and a kept one why.
func TestReadersStandAsTheySay(t *testing.T) {
	groups, err := Readers()
	if err != nil {
		t.Fatal(err)
	}
	r := root(t)
	files := goFiles(t, r)
	n := 0
	for _, g := range groups {
		for _, rd := range g.Readers {
			n++
			switch rd.Status {
			case StatusMoved:
				if rd.Proof == nil {
					t.Errorf("%s: moved without a proof", rd.ID)
					continue
				}
				b, err := os.ReadFile(filepath.Join(r, rd.Proof.File))
				if err != nil || !strings.Contains(string(b), rd.Proof.Contains) {
					t.Errorf("%s is marked moved but %s does not have %q", rd.ID, rd.Proof.File, rd.Proof.Contains)
				}
			case StatusRetired:
				if rd.AbsentText == "" {
					t.Errorf("%s: retired without the text that proves it", rd.ID)
				}
				for f, body := range files {
					if strings.Contains(body, rd.AbsentText) {
						t.Errorf("%s is marked retired but %s still has %q", rd.ID, f, rd.AbsentText)
					}
				}
			case StatusOpen:
				if rd.Owner == "" {
					t.Errorf("%s: open without an owner", rd.ID)
				}
			case StatusKept:
				if rd.Note == "" {
					t.Errorf("%s: kept without a reason", rd.ID)
				}
			default:
				t.Errorf("%s: status %q", rd.ID, rd.Status)
			}
		}
	}
	if n == 0 {
		t.Fatal("no readers")
	}
}

// The baselines name a time the query is taken at; Summarize counts what is
// retired and what is left.
func TestBaselinesAndSummary(t *testing.T) {
	bs, err := Baselines()
	if err != nil || len(bs) == 0 {
		t.Fatalf("baselines: %v %d", err, len(bs))
	}
	for _, b := range bs {
		if _, err := time.Parse(time.RFC3339, b.At); err != nil || !strings.Contains(b.Query, "{{at}}") {
			t.Errorf("%s: a baseline needs an RFC 3339 time and a query taken at it", b.Key)
		}
	}
	p := Summarize([]Channel{
		{ID: "A", Disposition: "deleted", Retired: &Retired{At: "2026-11-17", Change: "I1"}},
		{ID: "B", Disposition: "deleted"},
		{ID: "C", Disposition: "kept"},
	})
	if p.Start != 3 || p.Now != 2 || p.Retired != 1 || len(p.Recent) != 1 || p.Recent[0].ID != "A" {
		t.Fatalf("summary: %+v", p)
	}
}

// 2026-11-17 (the coordinator's audit #14: the earned routes were a
// hand-kept list): trust is earned from the readings, n clean comparisons
// and not one difference; a route with a difference, or too few, is not.
func TestEarnedRoutesFromReadings(t *testing.T) {
	got := EarnedRoutes([]string{"a", "b", "c", "d"},
		map[string]float64{"a": 60, "b": 60, "c": 10},
		map[string]float64{"b": 1}, 50)
	if !got["a"] || got["b"] || got["c"] || got["d"] || len(got) != 1 {
		t.Fatalf("earned %v", got)
	}
}

// Origin: 2026-11-18, the coordinator: a reader's "moved" was proven only by
// text in the code, so the Bus page could call a switch done that does not run
// on PROD. How long a reader may be silent comes from its own pace.
func TestEvidenceGoesByTheReadersOwnPace(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) float64 { return float64(now.Add(-d).Unix()) }
	loop := float64(EvidenceWindow / (30 * time.Second))
	for _, c := range []struct {
		name       string
		last, runs float64
		want       string
	}{
		{"a 30-second loop that just ran", ago(time.Minute), loop, EvidenceRunning},
		{"a 30-second loop silent for an hour", ago(time.Hour), loop, EvidenceStale},
		{"a page opened twice a week, silent for two days", ago(48 * time.Hour), 2, EvidenceRunning},
		{"never answered from the new source", 0, 0, EvidenceNone},
		{"last answered before the window", ago(8 * 24 * time.Hour), 0, EvidenceNone},
	} {
		if got := JudgeEvidence(now, c.last, c.runs).State; got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// Same origin: every moved reader marks its answers, or the page could only
// ever call it unproven; the mark names the reader as readers.yaml does.
func TestEveryMovedReaderMarksItsRuns(t *testing.T) {
	groups, err := Readers()
	if err != nil {
		t.Fatal(err)
	}
	files := goFiles(t, root(t))
	for _, g := range groups {
		for _, rd := range g.Readers {
			if rd.Status != StatusMoved {
				continue
			}
			found := false
			for f, body := range files {
				// Directly, or through mainOf, which marks the reader it is given.
				marks := strings.Contains(body, "bus/readerseen") || strings.Contains(body, `s.mainOf("`+rd.ID+`"`)
				if !strings.HasSuffix(f, "_test.go") && marks && strings.Contains(body, `"`+rd.ID+`"`) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("moved reader %s has no run mark (readerseen) in the code", rd.ID)
			}
		}
	}
}
